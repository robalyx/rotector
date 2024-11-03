package progress

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// Bar creates a visual progress indicator with percentage, step messages,
// and estimated completion time. It uses mutex locking to handle concurrent updates.
type Bar struct {
	total            int64
	current          int64
	width            int
	mu               sync.Mutex
	lastUpdate       time.Time
	message          string
	stepMessage      string
	stepStart        time.Time
	overallStart     time.Time
	overallDurations []time.Duration
}

// NewBar creates a progress bar with a total value to track progress against,
// a width in characters for the visual bar, and a message describing the overall operation.
func NewBar(total int64, width int, message string) *Bar {
	return &Bar{
		total:            total,
		current:          0,
		width:            width,
		mu:               sync.Mutex{},
		lastUpdate:       time.Now(),
		message:          message,
		stepStart:        time.Now(),
		overallStart:     time.Now(),
		overallDurations: []time.Duration{},
	}
}

// Increment adds to the current progress value, capping at the total.
func (b *Bar) Increment(n int64) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.current += n
	if b.current > b.total {
		b.current = b.total
	}
}

// SetTotal updates the total value that represents 100% progress.
func (b *Bar) SetTotal(total int64) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.total = total
}

// SetCurrent directly sets the current progress value, capping at total.
func (b *Bar) SetCurrent(current int64) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.current = current
	if b.current > b.total {
		b.current = b.total
	}
}

// SetMessage updates the overall operation description.
func (b *Bar) SetMessage(message string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.message = message
}

// SetStepMessage updates the current step description and resets the step timer.
func (b *Bar) SetStepMessage(message string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.stepMessage = message
	b.stepStart = time.Now()
}

// String generates the visual progress bar with percentage complete,
// current step message and duration, overall duration and ETA.
// Updates are rate-limited to 100ms to prevent screen flicker.
func (b *Bar) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Rate limit updates to 100ms
	if time.Since(b.lastUpdate) < 100*time.Millisecond {
		return ""
	}
	b.lastUpdate = time.Now()

	// Calculate progress percentage and bar fill
	percent := float64(b.current) / float64(b.total)
	filled := int(percent * float64(b.width))
	bar := strings.Repeat("=", filled) + strings.Repeat("-", b.width-filled)

	// Format durations
	stepDuration := time.Since(b.stepStart).Round(time.Second)
	overallDuration := time.Since(b.overallStart).Round(time.Second)

	return fmt.Sprintf("\r%s [%s] %.1f%% | %s (%s) | Overall: %s (ETA: %s)",
		b.message, bar, percent*100, b.stepMessage, stepDuration,
		overallDuration, b.calculateETA())
}

// calculateETA estimates completion time based on previous operation durations.
// Returns "0s" if no duration history is available.
func (b *Bar) calculateETA() string {
	if len(b.overallDurations) == 0 {
		return "0s"
	}

	// Average the stored durations
	var totalDuration time.Duration
	for _, duration := range b.overallDurations {
		totalDuration += duration
	}

	eta := totalDuration / time.Duration(len(b.overallDurations))
	return eta.Round(time.Second).String()
}

// Reset prepares the bar for a new operation by storing the previous operation's duration
// and resetting progress counters and timers. It maintains a rolling window of past durations
// for ETA calculation.
func (b *Bar) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Remove oldest duration if at capacity (10 entries)
	if len(b.overallDurations) >= 10 {
		b.overallDurations = b.overallDurations[1:]
	}

	// Store current operation's duration
	b.overallDurations = append(b.overallDurations, time.Since(b.overallStart))

	// Reset counters and timers
	b.current = 0
	b.lastUpdate = time.Now()
	b.stepMessage = ""
	b.stepStart = time.Now()
	b.overallStart = time.Now()
}
