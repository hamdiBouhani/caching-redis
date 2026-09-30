package main

import (
	"context"
	_ "embed"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

//go:embed ratelimit.lua
var ratelimitLua string

type RateLimitResult struct {
	Allowed   bool
	Remaining int
	ResetAt   time.Time
}

var ratelimitScript = redis.NewScript(ratelimitLua)

// ratelimit.go
type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time {
	return time.Now()
}

type RateLimiter struct {
	rdb   *redis.Client
	clock Clock
}

func NewRateLimiter(rdb *redis.Client) *RateLimiter {
	return &RateLimiter{rdb: rdb, clock: realClock{}}
}

// WithClock lets tests swap in a fake clock.
func (r *RateLimiter) WithClock(c Clock) *RateLimiter {
	r.clock = c
	return r
}

// Allow checks and consumes one token for key. window is the rolling window,
// limit is the max requests allowed within it.
func (r *RateLimiter) Allow(ctx context.Context, key string, limit int, window time.Duration) (RateLimitResult, error) {
	nowMs := r.clock.Now().UnixMilli()
	windowMs := window.Milliseconds()

	res, err := ratelimitScript.Run(ctx, r.rdb, []string{key},
		nowMs, windowMs, limit,
	).Slice()
	if err != nil {
		return RateLimitResult{}, err
	}

	if len(res) != 3 {
		return RateLimitResult{}, errors.New("unexpected script result")
	}

	allowed, _ := res[0].(int64)
	remaining, _ := res[1].(int64)
	resetMs, _ := res[2].(int64)

	return RateLimitResult{
		Allowed:   allowed == 1,
		Remaining: int(remaining),
		ResetAt:   time.UnixMilli(resetMs),
	}, nil
}
