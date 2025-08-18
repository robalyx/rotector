package tui

import (
	"context"
	"errors"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/robalyx/rotector/internal/tui/components"
	"go.uber.org/zap"
)

var ErrTUIManagerAlreadyRunning = errors.New("TUI manager is already running")

// Manager manages the TUI interface for multiple workers.
type Manager struct {
	model   *Model
	program *tea.Program
	ctx     context.Context
	cancel  context.CancelFunc
	logger  *zap.Logger
	logDir  string
	workers map[int]*WorkerInfo
	mu      sync.RWMutex
	running bool
}

// NewManager creates a new TUI manager.
func NewManager(ctx context.Context, logDir string, logger *zap.Logger) *Manager {
	childCtx, cancel := context.WithCancel(ctx)

	model := NewModel(childCtx, logDir)

	return &Manager{
		model:   model,
		ctx:     childCtx,
		cancel:  cancel,
		logger:  logger,
		logDir:  logDir,
		workers: make(map[int]*WorkerInfo),
	}
}

// Start starts the TUI interface.
func (m *Manager) Start() error {
	if m.running {
		return ErrTUIManagerAlreadyRunning
	}

	m.running = true
	m.program = tea.NewProgram(m.model, tea.WithAltScreen())

	// Run the program in a goroutine
	go func() {
		defer func() {
			m.running = false
		}()

		if _, err := m.program.Run(); err != nil {
			m.logger.Error("TUI program error", zap.Error(err))
		}
	}()

	return nil
}

// Stop stops the TUI interface.
func (m *Manager) Stop() {
	if !m.running {
		return
	}

	if m.program != nil {
		m.program.Quit()
	}

	if m.cancel != nil {
		m.cancel()
	}
}

// AddWorker adds a new worker to track.
func (m *Manager) AddWorker(id int, workerType, name, logPath string) *components.ProgressBar {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Create a TUI progress bar for this worker
	progressBar := components.NewProgressBar(100, name)

	worker := &WorkerInfo{
		ID:          id,
		Name:        name,
		Type:        workerType,
		LogPath:     logPath,
		Bar:         progressBar,
		Status:      "Starting",
		Healthy:     true,
		LastUpdated: time.Now(),
	}

	m.workers[id] = worker

	// Add worker to model if TUI is running
	if m.running && m.model != nil {
		m.model.AddWorker(id, workerType, name, logPath, progressBar)
	}

	return progressBar
}

// UpdateWorkerStatus updates a worker's status.
func (m *Manager) UpdateWorkerStatus(id int, status string, healthy bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if worker, exists := m.workers[id]; exists {
		worker.Status = status
		worker.Healthy = healthy
		worker.LastUpdated = time.Now()

		// Update model if TUI is running
		if m.running && m.model != nil {
			m.model.UpdateWorkerStatus(id, status, healthy)
		}
	}
}

// RemoveWorker removes a worker from tracking.
func (m *Manager) RemoveWorker(id int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.workers, id)
}

// GetWorker gets a worker by ID.
func (m *Manager) GetWorker(id int) *WorkerInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.workers[id]
}

// IsRunning returns whether the TUI is currently running.
func (m *Manager) IsRunning() bool {
	return m.running
}
