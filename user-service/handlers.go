package main

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

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
