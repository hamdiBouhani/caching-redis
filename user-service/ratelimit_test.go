package main

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{
		now: time.Unix(0, 0),
	}
}

func (f *fakeClock) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

func (f *fakeClock) Advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = f.now.Add(d)
}

func TestRateLimiter_SlidingWindow_NoBoundaryBurst(t *testing.T) {
	cache, _ := newTestCache(t)
	clock := newFakeClock()
	rl := NewRateLimiter(cache.rdb).WithClock(clock)
	ctx := context.Background()

	// t=0: consume 5
	for i := 0; i < 5; i++ {
		res, err := rl.Allow(ctx, "test", 10, time.Second)
		require.NoError(t, err)
		require.True(t, res.Allowed)
	}

	// t=900ms: consume 5 more (should still fit — 10 total in window)
	clock.Advance(900 * time.Millisecond)
	for i := 0; i < 5; i++ {
		res, err := rl.Allow(ctx, "test", 10, time.Second)
		require.NoError(t, err)
		require.True(t, res.Allowed)
	}

	// 11th request at t=900ms → blocked (sliding window)
	res, err := rl.Allow(ctx, "test", 10, time.Second)
	require.NoError(t, err)
	assert.False(t, res.Allowed, "11th should be blocked")

	// Advance to t=1100ms — the first 5 requests (t=0) are now outside the window
	clock.Advance(200 * time.Millisecond)
	res, err = rl.Allow(ctx, "test", 10, time.Second)
	require.NoError(t, err)
	assert.True(t, res.Allowed, "window should have slid forward")
}
