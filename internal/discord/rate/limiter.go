package rate

import (
	"context"
	"math/rand"
	"sync"
	"time"
)

// Limiter enforces delays between Discord API requests with random jitter.
type Limiter struct {
	mu          sync.Mutex
	lastRequest time.Time
	minInterval time.Duration
	maxJitter   time.Duration
	rng         *rand.Rand
}

// New creates a rate limiter with base interval and jitter.
// For example, baseInterval=1s and jitter=200ms will result in delays between 800ms-1200ms.
func New(baseInterval, jitter time.Duration) *Limiter {
	return &Limiter{
		lastRequest: time.Now().Add(-baseInterval),
		minInterval: baseInterval,
		maxJitter:   jitter,
		rng:         rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// WaitForNextSlot blocks until enough time has passed since the last request.
func (r *Limiter) WaitForNextSlot(ctx context.Context) error {
	r.mu.Lock()
	elapsed := time.Since(r.lastRequest)
	jitterOffset := time.Duration(r.rng.Int63n(int64(r.maxJitter*2))) - r.maxJitter
	targetDelay := r.minInterval + jitterOffset
	waitDuration := targetDelay - elapsed

	r.mu.Unlock()

	if waitDuration > 0 {
		timer := time.NewTimer(waitDuration)
		defer timer.Stop()

		select {
		case <-timer.C:
			// Wait completed successfully
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	r.mu.Lock()
	r.lastRequest = time.Now()
	r.mu.Unlock()

	return nil
}
