package progress

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// Bar represents a progress bar.
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

// NewBar creates a new progress bar.
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

// SetStepMessage sets the step message for the progress bar.
func (b *Bar) SetStepMessage(message string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.stepMessage = message
	b.stepStart = time.Now()
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

	stepDuration := time.Since(b.stepStart).Round(time.Second)
	overallDuration := time.Since(b.overallStart).Round(time.Second)
	return fmt.Sprintf("\r%s [%s] %.1f%% | %s (%s) | Overall: %s (ETA: %s)", b.message, bar, percent*100, b.stepMessage, stepDuration, overallDuration, b.calculateETA())
}

// calculateETA calculates the estimated time of completion based on overall durations.
func (b *Bar) calculateETA() string {
	if len(b.overallDurations) == 0 {
		return "0s"
	}

	var totalDuration time.Duration
	for _, duration := range b.overallDurations {
		totalDuration += duration
	}

	eta := totalDuration / time.Duration(len(b.overallDurations))
	return eta.Round(time.Second).String()
}

// Reset resets the progress bar to its initial state.
func (b *Bar) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Remove the oldest duration if the array is full
	if len(b.overallDurations) >= 10 {
		b.overallDurations = b.overallDurations[1:]
	}

	// Add the current overall duration
	b.overallDurations = append(b.overallDurations, time.Since(b.overallStart))

	// Reset the bar
	b.current = 0
	b.lastUpdate = time.Now()
	b.stepMessage = ""
	b.stepStart = time.Now()
	b.overallStart = time.Now()
}
