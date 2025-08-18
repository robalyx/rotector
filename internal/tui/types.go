package tui

import (
	"time"

	"github.com/robalyx/rotector/internal/tui/components"
)

// WorkerInfo contains information about a worker.
type WorkerInfo struct {
	ID          int
	Name        string
	Type        string
	LogPath     string
	Bar         *components.ProgressBar
	Status      string
	Healthy     bool
	LastUpdated time.Time
}
