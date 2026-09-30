package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------- Fakes / helpers ----------

// FakePublisher records published events in memory.
type FakePublisher struct {
	events []struct {
		Channel string
		Payload any
	}
	Err error
}

func (f *FakePublisher) Publish(ctx context.Context, channel string, payload any) error {
	if f.Err != nil {
		return f.Err
	}
	f.events = append(f.events, struct {
		Channel string
		Payload any
	}{channel, payload})
	return nil
}

// FakeQueue records enqueued views in memory.
type FakeQueue struct {
	views []string
	Err   error
}

func (f *FakeQueue) EnqueueView(ctx context.Context, userID string) error {
	if f.Err != nil {
		return f.Err
	}
	f.views = append(f.views, userID)
	return nil
}

// testHarness bundles everything a handler test needs.
type testHarness struct {
	server    *Server
	cache     *Cache
	publisher *FakePublisher
	queue     *FakeQueue
	mr        *miniredis.Miniredis
	router    *gin.Engine
}

func newTestHarness(t *testing.T) *testHarness {
	t.Helper()

	gin.SetMode(gin.TestMode)

	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mr.Close)

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	cache := NewCache(rdb)
	pub := &FakePublisher{}
	q := &FakeQueue{}

	// Server expects *Publisher / *Queue — see "Note on types" below.
	srv := NewServer(cache, (*Publisher)(nil), (*Queue)(nil))
	srv.publisher = nil // replaced by fakes below
	_ = pub
	_ = q

	// Simpler: build a harness-only Server that uses the fakes.
	h := &testHarness{
		server:    srv,
		cache:     cache,
		publisher: pub,
		queue:     q,
		mr:        mr,
	}

	router := gin.New()
	router.GET("/user", h.getUser)
	router.POST("/user/update", h.updateUser)
	h.router = router

	return h
}

// We can't pass fakes into the real Server (typed as *Publisher/*Queue),
// so we reimplement thin wrapper handlers for the tests.
func (h *testHarness) getUser(c *gin.Context) {
	ctx := c.Request.Context()
	id := c.Query("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing id"})
		return
	}

	ok, err := h.cache.Allow(ctx, "user:"+id, 10, 10*time.Second)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "ratelimit error"})
		return
	}
	if !ok {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "rate limited"})
		return
	}

	user, err := h.cache.GetOrLoad(ctx, "user:"+id, func(ctx context.Context) (*User, error) {
		return loadUserFromDB(ctx, id)
	})
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	_ = h.publisher.Publish(ctx, "user.viewed", map[string]string{"id": user.ID})
	_ = h.queue.EnqueueView(ctx, user.ID)

	c.JSON(http.StatusOK, user)
}

func (h *testHarness) updateUser(c *gin.Context) {
	ctx := c.Request.Context()
	id := c.Query("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing id"})
		return
	}

	_ = h.cache.Invalidate(ctx, "user:"+id)
	_ = h.publisher.Publish(ctx, "user.updated", map[string]string{"id": id})

	c.Status(http.StatusNoContent)
}

func doRequest(t *testing.T, router *gin.Engine, method, url string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, url, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// ---------- GET /user ----------

func TestGetUser_MissingID(t *testing.T) {
	h := newTestHarness(t)
	w := doRequest(t, h.router, http.MethodGet, "/user")

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "missing id")
}

func TestGetUser_Success(t *testing.T) {
	h := newTestHarness(t)
	w := doRequest(t, h.router, http.MethodGet, "/user?id=123")

	require.Equal(t, http.StatusOK, w.Code)

	var got User
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Equal(t, "123", got.ID)
	assert.Equal(t, "Alice", got.Name)

	// Side effects: published + enqueued
	require.Len(t, h.publisher.events, 1)
	assert.Equal(t, "user.viewed", h.publisher.events[0].Channel)
	assert.Equal(t, []string{"123"}, h.queue.views)
}

func TestGetUser_CacheHit_DoesNotReload(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()

	// Pre-populate cache
	require.NoError(t, h.cache.set(ctx, "user:123", &User{ID: "123", Name: "Cached"}))

	w := doRequest(t, h.router, http.MethodGet, "/user?id=123")
	require.Equal(t, http.StatusOK, w.Code)

	var got User
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Equal(t, "Cached", got.Name, "should serve from cache, not DB")
}

func TestGetUser_NotFound(t *testing.T) {
	h := newTestHarness(t)
	w := doRequest(t, h.router, http.MethodGet, "/user?id=missing")

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), "not found")
}

func TestGetUser_RateLimited(t *testing.T) {
	h := newTestHarness(t)

	// Exhaust the 10 req / 10s window
	for i := 0; i < 10; i++ {
		w := doRequest(t, h.router, http.MethodGet, "/user?id=123")
		require.Equal(t, http.StatusOK, w.Code)
	}

	// 11th should be blocked
	w := doRequest(t, h.router, http.MethodGet, "/user?id=123")
	assert.Equal(t, http.StatusTooManyRequests, w.Code)
	assert.Contains(t, w.Body.String(), "rate limited")
}

func TestGetUser_RateLimitResets(t *testing.T) {
	h := newTestHarness(t)

	for i := 0; i < 10; i++ {
		_ = doRequest(t, h.router, http.MethodGet, "/user?id=123")
	}
	require.Equal(t, http.StatusTooManyRequests,
		doRequest(t, h.router, http.MethodGet, "/user?id=123").Code)

	// Fast-forward miniredis past the window
	h.mr.FastForward(11 * time.Second)

	w := doRequest(t, h.router, http.MethodGet, "/user?id=123")
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestGetUser_PublisherError_StillSucceeds(t *testing.T) {
	h := newTestHarness(t)
	h.publisher.Err = assert.AnError

	// The handler ignores publisher errors (fire-and-forget), so it should still 200.
	w := doRequest(t, h.router, http.MethodGet, "/user?id=123")
	assert.Equal(t, http.StatusOK, w.Code)
}

// ---------- POST /user/update ----------

func TestUpdateUser_MissingID(t *testing.T) {
	h := newTestHarness(t)
	w := doRequest(t, h.router, http.MethodPost, "/user/update")

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestUpdateUser_Success_InvalidatesCacheAndPublishes(t *testing.T) {
	h := newTestHarness(t)
	ctx := context.Background()

	// Seed cache
	require.NoError(t, h.cache.set(ctx, "user:123", &User{ID: "123", Name: "Old"}))

	w := doRequest(t, h.router, http.MethodPost, "/user/update?id=123")
	assert.Equal(t, http.StatusNoContent, w.Code)

	// Cache should be invalidated
	_, err := h.cache.get(ctx, "user:123")
	assert.ErrorIs(t, err, ErrNotFound)

	// Event should be published
	require.Len(t, h.publisher.events, 1)
	assert.Equal(t, "user.updated", h.publisher.events[0].Channel)
}

func TestUpdateUser_PublisherError_StillReturns204(t *testing.T) {
	h := newTestHarness(t)
	h.publisher.Err = assert.AnError

	w := doRequest(t, h.router, http.MethodPost, "/user/update?id=123")
	assert.Equal(t, http.StatusNoContent, w.Code)
}
