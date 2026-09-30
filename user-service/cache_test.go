package main

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestCache spins up a miniredis instance + Cache wired to it.
func newTestCache(t *testing.T) (*Cache, *miniredis.Miniredis) {
	t.Helper()

	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mr.Close)

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	return NewCache(rdb), mr
}

func sampleUser() *User {
	return &User{ID: "123", Name: "Alice", Email: "alice@example.com"}
}

// ---------- get / set ----------

func TestCache_SetAndGet(t *testing.T) {
	cache, _ := newTestCache(t)
	ctx := context.Background()

	want := sampleUser()
	require.NoError(t, cache.set(ctx, "user:123", want))

	got, err := cache.get(ctx, "user:123")
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestCache_Get_Miss(t *testing.T) {
	cache, _ := newTestCache(t)

	_, err := cache.get(context.Background(), "does-not-exist")
	assert.ErrorIs(t, err, ErrNotFound)
}

// ---------- Invalidate ----------

func TestCache_Invalidate(t *testing.T) {
	cache, _ := newTestCache(t)
	ctx := context.Background()

	require.NoError(t, cache.set(ctx, "user:123", sampleUser()))
	require.NoError(t, cache.Invalidate(ctx, "user:123"))

	_, err := cache.get(ctx, "user:123")
	assert.ErrorIs(t, err, ErrNotFound)
}

// ---------- GetOrLoad ----------

func TestCache_GetOrLoad_CacheMiss_LoadsAndStores(t *testing.T) {
	cache, _ := newTestCache(t)
	ctx := context.Background()

	var calls int32
	loader := func(ctx context.Context) (*User, error) {
		atomic.AddInt32(&calls, 1)
		return sampleUser(), nil
	}

	got, err := cache.GetOrLoad(ctx, "user:123", loader)
	require.NoError(t, err)
	assert.Equal(t, "Alice", got.Name)
	assert.Equal(t, int32(1), atomic.LoadInt32(&calls), "loader should be called once")

	// Second call should hit cache, not loader
	got2, err := cache.GetOrLoad(ctx, "user:123", loader)
	require.NoError(t, err)
	assert.Equal(t, got, got2)
	assert.Equal(t, int32(1), atomic.LoadInt32(&calls), "loader should still be called once")
}

func TestCache_GetOrLoad_LoaderError(t *testing.T) {
	cache, _ := newTestCache(t)
	ctx := context.Background()

	wantErr := errors.New("db down")
	_, err := cache.GetOrLoad(ctx, "user:123", func(ctx context.Context) (*User, error) {
		return nil, wantErr
	})
	assert.ErrorIs(t, err, wantErr)
}

// The singleflight guarantee: N concurrent misses = 1 loader call.
func TestCache_GetOrLoad_SingleflightPreventsStampede(t *testing.T) {
	cache, _ := newTestCache(t)
	ctx := context.Background()

	var calls int32
	loader := func(ctx context.Context) (*User, error) {
		atomic.AddInt32(&calls, 1)
		time.Sleep(50 * time.Millisecond) // simulate slow DB
		return sampleUser(), nil
	}

	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			u, err := cache.GetOrLoad(ctx, "user:123", loader)
			assert.NoError(t, err)
			assert.NotNil(t, u)
		}()
	}
	wg.Wait()

	assert.Equal(t, int32(1), atomic.LoadInt32(&calls),
		"loader should only be called once despite %d concurrent requests", goroutines)
}

// ---------- Allow (rate limiting) ----------

func TestCache_Allow_UnderLimit(t *testing.T) {
	cache, _ := newTestCache(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		ok, err := cache.Allow(ctx, "alice", 10, time.Minute)
		require.NoError(t, err)
		assert.True(t, ok, "request %d should be allowed", i+1)
	}
}

func TestCache_Allow_ExceedsLimit(t *testing.T) {
	cache, _ := newTestCache(t)
	ctx := context.Background()

	// Exhaust the limit
	for i := 0; i < 10; i++ {
		ok, err := cache.Allow(ctx, "alice", 10, time.Minute)
		require.NoError(t, err)
		require.True(t, ok)
	}

	// The 11th should be denied
	ok, err := cache.Allow(ctx, "alice", 10, time.Minute)
	require.NoError(t, err)
	assert.False(t, ok, "11th request should be rate limited")
}

func TestCache_Allow_IsolatesUsers(t *testing.T) {
	cache, _ := newTestCache(t)
	ctx := context.Background()

	// Exhaust alice's limit
	for i := 0; i < 10; i++ {
		_, _ = cache.Allow(ctx, "alice", 10, time.Minute)
	}

	// Bob should still be allowed
	ok, err := cache.Allow(ctx, "bob", 10, time.Minute)
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestCache_Allow_ResetsAfterWindow(t *testing.T) {
	cache, mr := newTestCache(t)
	ctx := context.Background()

	// Hit the limit
	for i := 0; i < 10; i++ {
		_, _ = cache.Allow(ctx, "alice", 10, time.Minute)
	}
	ok, _ := cache.Allow(ctx, "alice", 10, time.Minute)
	require.False(t, ok)

	// Fast-forward miniredis's clock past the window
	mr.FastForward(61 * time.Second)

	ok, err := cache.Allow(ctx, "alice", 10, time.Minute)
	require.NoError(t, err)
	assert.True(t, ok, "limit should reset after the window expires")
}
