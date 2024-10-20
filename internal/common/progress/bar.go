package progress

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// Bar represents a progress bar.
type Bar struct {
	total      int64
	current    int64
	width      int
	mu         sync.Mutex
	lastUpdate time.Time
	message    string
}

// NewBar creates a new progress bar.
func NewBar(total int64, width int, message string) *Bar {
	return &Bar{
		total:      total,
		current:    0,
		width:      width,
		mu:         sync.Mutex{},
		lastUpdate: time.Now(),
		message:    message,
	}
}

// Increment increases the current progress.
func (b *Bar) Increment(n int64) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.current += n
	if b.current > b.total {
		b.current = b.total
	}
}

// SetTotal sets the total value for the progress bar.
func (b *Bar) SetTotal(total int64) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.total = total
}

// SetCurrent sets the current value for the progress bar.
func (b *Bar) SetCurrent(current int64) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.current = current
	if b.current > b.total {
		b.current = b.total
	}
}

// SetMessage sets the message for the progress bar.
func (b *Bar) SetMessage(message string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.message = message
}

// String returns the string representation of the progress bar.
func (b *Bar) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	if time.Since(b.lastUpdate) < 100*time.Millisecond {
		return ""
	}
	b.lastUpdate = time.Now()

	percent := float64(b.current) / float64(b.total)
	filled := int(percent * float64(b.width))
	bar := strings.Repeat("=", filled) + strings.Repeat("-", b.width-filled)
	return fmt.Sprintf("\r%s [%s] %.1f%%", b.message, bar, percent*100)
}

// Reset resets the progress bar to its initial state.
func (b *Bar) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.current = 0
	b.lastUpdate = time.Now()
}
