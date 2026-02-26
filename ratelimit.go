package patchbin

import (
	"fmt"
	"sync"
	"time"
)

// RateLimiter enforces a single global cap on submissions per interval,
// shared across all users (not keyed by pubkey or IP).
type RateLimiter struct {
	mu       sync.Mutex
	max      int
	interval time.Duration
	count    int
	resetAt  time.Time
}

func NewRateLimiter(max int, interval time.Duration) *RateLimiter {
	return &RateLimiter{
		max:      max,
		interval: interval,
	}
}

// Allow reports whether a new submission is permitted under the current
// window, incrementing the window's counter if so.
func (r *RateLimiter) Allow() bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	if now.After(r.resetAt) {
		r.count = 0
		r.resetAt = now.Add(r.interval)
	}

	if r.count >= r.max {
		return false
	}

	r.count++
	return true
}

func (r *RateLimiter) Error() error {
	return fmt.Errorf(
		"rate limit exceeded: max %d submissions per %s, try again later",
		r.max,
		r.interval,
	)
}
