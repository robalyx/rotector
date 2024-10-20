package progress

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

// Renderer handles rendering multiple progress bars.
type Renderer struct {
	bars   []*Bar
	output io.Writer
	mu     sync.Mutex
}

// NewRenderer creates a new Renderer.
func NewRenderer(bars []*Bar) *Renderer {
	return &Renderer{
		bars:   bars,
		output: os.Stdout,
	}
}

// Render starts rendering the progress bars.
func (r *Renderer) Render() {
	for {
		r.mu.Lock()

		// Clear the lines
		for range r.bars {
			_, _ = fmt.Fprint(r.output, "\033[1A\033[K")
		}

		// Render progress bars
		for _, bar := range r.bars {
			_, _ = fmt.Fprintln(r.output, bar.String())
		}

		r.mu.Unlock()

		time.Sleep(100 * time.Millisecond)
	}
}

// Stop stops the renderer.
func (r *Renderer) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Clear the lines one last time
	for range r.bars {
		_, _ = fmt.Fprint(r.output, "\033[1A\033[K")
	}
}
