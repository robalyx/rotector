package sync

import (
	"math/rand"
	"time"
)

// requestRateLimiter enforces delays between Discord API requests with random jitter
// to avoid detection patterns.
type requestRateLimiter struct {
	lastRequest time.Time
	minInterval time.Duration
	maxJitter   time.Duration
	rng         *rand.Rand
}

// newRequestRateLimiter creates a rate limiter with base interval and jitter.
// For example, baseInterval=1s and jitter=200ms will result in delays between 800ms-1200ms.
func newRequestRateLimiter(baseInterval, jitter time.Duration) *requestRateLimiter {
	return &requestRateLimiter{
		lastRequest: time.Now().Add(-baseInterval),
		minInterval: baseInterval,
		maxJitter:   jitter,
		rng:         rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// waitForNextSlot blocks until enough time has passed since the last request.
func (r *requestRateLimiter) waitForNextSlot() {
	elapsed := time.Since(r.lastRequest)

	jitterOffset := time.Duration(r.rng.Int63n(int64(r.maxJitter*2))) - r.maxJitter
	targetDelay := r.minInterval + jitterOffset

	if elapsed < targetDelay {
		time.Sleep(targetDelay - elapsed)
	}

	r.lastRequest = time.Now()
}
