package views

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/robalyx/rotector/internal/tui/components"
)

// Worker represents the individual worker view.
type Worker struct {
	name       string
	workerType string
	status     string
	healthy    bool
	logViewer  *components.LogViewer
	progress   *components.ProgressBar
	ctx        context.Context
	width      int
	height     int
}

// NewWorker creates a new worker view.
func NewWorker() *Worker {
	return &Worker{}
}

// SetWorker sets the current worker to display.
func (w *Worker) SetWorker(name, workerType, status, logPath string, healthy bool, bar *components.ProgressBar) {
	w.name = name
	w.workerType = workerType
	w.status = status
	w.healthy = healthy

	w.progress = bar
	if w.logViewer != nil && logPath != "" {
		w.logViewer.SetLogPath(logPath)
	}
}

// SetContext sets the context for the worker view.
func (w *Worker) SetContext(ctx context.Context) {
	w.ctx = ctx
	w.logViewer = components.NewLogViewer()
}

// SetSize sets the worker view size.
func (w *Worker) SetSize(width, height int) {
	w.width = width
	w.height = height

	if w.logViewer != nil {
		logViewerHeight := max(height-5, 5)
		w.logViewer.SetSize(width, logViewerHeight)
	}
}

// Init initializes the worker view.
func (w *Worker) Init() tea.Cmd {
	if w.logViewer != nil {
		return w.logViewer.Init()
	}

	return nil
}

// Update handles messages for the worker view.
func (w *Worker) Update(msg tea.Msg) (*Worker, tea.Cmd) {
	if w.logViewer != nil {
		_, cmd := w.logViewer.Update(msg)
		return w, cmd
	}

	return w, nil
}

// Refresh updates the worker view data.
func (w *Worker) Refresh() {
	if w.logViewer != nil {
		w.logViewer.Refresh()
	}
}

// View renders the worker view.
func (w *Worker) View() string {
	if w.name == "" {
		return "No worker selected"
	}

	// Progress section
	progress := ""
	if w.progress != nil {
		progress = w.progress.View()
	}

	// Log section
	logs := ""
	if w.logViewer != nil {
		logs = w.logViewer.View()
	}

	return lipgloss.JoinVertical(lipgloss.Left, progress, "", logs)
}
