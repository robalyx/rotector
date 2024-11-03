package progress

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

// Renderer manages multiple progress bars by updating them concurrently
// and handling terminal output synchronization.
type Renderer struct {
	bars   []*Bar
	output io.Writer
	mu     sync.Mutex
}

// NewRenderer creates a Renderer that will manage the provided progress bars.
// It uses stdout as the default output destination.
func NewRenderer(bars []*Bar) *Renderer {
	return &Renderer{
		bars:   bars,
		output: os.Stdout,
	}
}

// Render starts the rendering loop that updates all progress bars.
// It clears previous lines and redraws bars every 100ms to show progress.
// The loop continues until Stop is called.
func (r *Renderer) Render() {
	for {
		r.mu.Lock()

		// Clear previous lines using ANSI escape codes
		for range r.bars {
			_, _ = fmt.Fprint(r.output, "\033[1A\033[K")
		}

		// Draw updated progress bars
		for _, bar := range r.bars {
			_, _ = fmt.Fprintln(r.output, bar.String())
		}

		r.mu.Unlock()

		time.Sleep(100 * time.Millisecond)
	}
}

// Stop cleans up the display by clearing the progress bars from the screen.
// This prevents leftover progress bars from cluttering the terminal.
func (r *Renderer) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Clear all progress bar lines one last time
	for range r.bars {
		_, _ = fmt.Fprint(r.output, "\033[1A\033[K")
	}
}
