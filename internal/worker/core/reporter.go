package core

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/redis/rueidis"
	"go.uber.org/zap"
)

// StatusReporter handles automatic status reporting for workers.
type StatusReporter struct {
	monitor  *Monitor
	status   Status
	stopChan chan struct{}
	logger   *zap.Logger
}

// NewStatusReporter creates a new status reporter for a worker.
func NewStatusReporter(client rueidis.Client, workerType string, logger *zap.Logger) *StatusReporter {
	// Generate a UUID4 for the worker ID
	workerID := uuid.New().String()

	return &StatusReporter{
		monitor: NewMonitor(client, logger),
		status: Status{
			WorkerID:   workerID,
			WorkerType: workerType,
			IsHealthy:  true,
		},
		stopChan: make(chan struct{}),
		logger:   logger.Named("status_reporter"),
	}
}

// Start begins periodic status reporting.
func (r *StatusReporter) Start() {
	go func() {
		ticker := time.NewTicker(HeartbeatInterval)
		defer ticker.Stop()

		// Report initial status
		if err := r.monitor.ReportStatus(context.Background(), r.status); err != nil {
			r.logger.Error("Failed to report initial status", zap.Error(err))
		}

		for {
			select {
			case <-ticker.C:
				if err := r.monitor.ReportStatus(context.Background(), r.status); err != nil {
					r.logger.Error("Failed to report status", zap.Error(err))
				}
			case <-r.stopChan:
				return
			}
		}
	}()
}

// Stop ends status reporting.
func (r *StatusReporter) Stop() {
	close(r.stopChan)
}

// UpdateStatus updates the current status.
func (r *StatusReporter) UpdateStatus(task string, progress int) {
	r.status.CurrentTask = task
	r.status.Progress = progress
}

// SetHealthy updates the health status.
func (r *StatusReporter) SetHealthy(healthy bool) {
	r.status.IsHealthy = healthy
}

// GetWorkerID returns the unique worker ID.
func (r *StatusReporter) GetWorkerID() string {
	return r.status.WorkerID
}
