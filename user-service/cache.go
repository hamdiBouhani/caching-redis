package main

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
	"golang.org/x/sync/singleflight"
)

var ErrNotFound = errors.New("not found")

type Cache struct {
	rdb *redis.Client
	sf  singleflight.Group
	ttl time.Duration
}

func NewCache(rdb *redis.Client) *Cache {
	return &Cache{rdb: rdb, ttl: 5 * time.Minute}
}

// GetOrLoad: cache-aside with singleflight to prevent stampede.
func (c *Cache) GetOrLoad(ctx context.Context, key string, loader func(ctx context.Context) (*User, error)) (*User, error) {
	// 1. Try cache
	if u, err := c.get(ctx, key); err == nil {
		return u, nil
	}

	// 2. Singleflight: only one goroutine loads on miss
	v, err, _ := c.sf.Do(key, func() (interface{}, error) {
		// Double-check cache (another goroutine may have filled it)
		if u, err := c.get(ctx, key); err == nil {
			return u, nil
		}
		u, err := loader(ctx)
		if err != nil {
			return nil, err
		}
		if err := c.set(ctx, key, u); err != nil {
			// Log but don't fail the request
			_ = err
		}
		return u, nil
	})
	if err != nil {
		return nil, err
	}
	return v.(*User), nil
}

func (c *Cache) get(ctx context.Context, key string) (*User, error) {
	raw, err := c.rdb.Get(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	var u User
	if err := json.Unmarshal(raw, &u); err != nil {
		return nil, err
	}
	return &u, nil
}

func (c *Cache) set(ctx context.Context, key string, u *User) error {
	raw, err := json.Marshal(u)
	if err != nil {
		return err
	}
	return c.rdb.Set(ctx, key, raw, c.ttl).Err()
}

func (c *Cache) Invalidate(ctx context.Context, key string) error {
	return c.rdb.Del(ctx, key).Err()
}

// Allow returns true if the request is within the limit.
// Uses a simple fixed-window counter — good enough for most cases.
func (c *Cache) Allow(ctx context.Context, userID string, limit int, window time.Duration) (bool, error) {
	key := "ratelimit:" + userID
	pipe := c.rdb.TxPipeline()
	incr := pipe.Incr(ctx, key)
	pipe.Expire(ctx, key, window)
	if _, err := pipe.Exec(ctx); err != nil {
		return false, err
	}
	return incr.Val() <= int64(limit), nil
}
