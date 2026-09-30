package main

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

// RateLimitMiddleware applies a per-key sliding-window limit.
// keyFn decides which key to use (user ID, IP, API key, etc.).
func RateLimitMiddleware(rl *RateLimiter, keyFn func(*gin.Context) string, limit int, window time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		key := "ratelimit:" + keyFn(c)

		res, err := rl.Allow(c.Request.Context(), key, limit, window)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "ratelimit error"})
			return
		}

		c.Header("X-RateLimit-Limit", strconv.Itoa(limit))
		c.Header("X-RateLimit-Remaining", strconv.Itoa(res.Remaining))
		c.Header("X-RateLimit-Reset", strconv.FormatInt(res.ResetAt.Unix(), 10))

		if !res.Allowed {
			c.Header("Retry-After", strconv.FormatInt(
				int64(time.Until(res.ResetAt).Seconds())+1, 10))
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error":    "rate limited",
				"reset_at": res.ResetAt.Format(time.RFC3339),
			})
			return
		}

		c.Next()
	}
}

type Server struct {
	cache     *Cache
	publisher *Publisher
	queue     *Queue
}

func NewServer(cache *Cache, pub *Publisher, q *Queue) *Server {
	return &Server{cache: cache, publisher: pub, queue: q}
}

// GET /user?id=123
func (s *Server) GetUser(c *gin.Context) {
	ctx := c.Request.Context()
	id := c.Query("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing id"})
		return
	}

	// Allow returns true if the request is within the limit.
	// Uses a simple fixed-window counter — good enough for most cases.
	// Rate limit: 10 req / 10s per user
	ok, err := s.cache.Allow(ctx, "user:"+id, 10, 10*time.Second)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "ratelimit error"})
		return
	}
	if !ok {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "rate limited"})
		return
	}

	// Get user from cache or load from DB
	user, err := s.cache.GetOrLoad(ctx, "user:"+id, func(ctx context.Context) (*User, error) {
		return loadUserFromDB(ctx, id)
	})
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	// Publish an event (fire-and-forget)
	_ = s.publisher.Publish(ctx, "user.viewed", map[string]string{"id": user.ID})

	// Enqueue background work
	_ = s.queue.EnqueueView(ctx, user.ID)

	c.JSON(http.StatusOK, user)
}

// POST /user/update?id=123
func (s *Server) UpdateUser(c *gin.Context) {
	ctx := c.Request.Context()
	id := c.Query("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing id"})
		return
	}

	// ... update DB ...

	// Invalidate cache
	if err := s.cache.Invalidate(ctx, "user:"+id); err != nil {
		// log but don't fail
		_ = err
	}

	// Publish change event
	_ = s.publisher.Publish(ctx, "user.updated", map[string]string{"id": id})

	c.Status(http.StatusNoContent)
}
