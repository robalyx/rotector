package worker

import (
	"context"
	"fmt"
	"time"

	"github.com/bytedance/sonic"
	"github.com/redis/rueidis"
	"go.uber.org/zap"
)

const (
	// HeartbeatInterval is how often workers should report their status.
	HeartbeatInterval = 10 * time.Second

	// HeartbeatTTL is how long a worker's status remains valid.
	HeartbeatTTL = 10 * time.Minute

	// StaleThreshold is how long before a worker is considered offline.
	StaleThreshold = 1 * time.Minute
)

// Status represents a worker's current state.
type Status struct {
	WorkerID    string    `json:"workerId"`
	WorkerType  string    `json:"workerType"`
	SubType     string    `json:"subType"`
	LastSeen    time.Time `json:"lastSeen"`
	CurrentTask string    `json:"currentTask,omitempty"`
	Progress    int       `json:"progress"`
	IsHealthy   bool      `json:"isHealthy"`
}

// Monitor handles worker status reporting and querying.
type Monitor struct {
	client rueidis.Client
	logger *zap.Logger
}

// NewMonitor creates a new worker status monitor.
func NewMonitor(client rueidis.Client, logger *zap.Logger) *Monitor {
	return &Monitor{
		client: client,
		logger: logger,
	}
}

// ReportStatus updates a worker's status in Redis.
func (m *Monitor) ReportStatus(ctx context.Context, status Status) error {
	// Update last seen timestamp
	status.LastSeen = time.Now()

	// Convert status to JSON
	data, err := sonic.Marshal(status)
	if err != nil {
		return fmt.Errorf("failed to marshal status: %w", err)
	}

	// Store in Redis with TTL
	key := fmt.Sprintf("worker:%s:%s:%s", status.WorkerType, status.SubType, status.WorkerID)
	err = m.client.Do(ctx, m.client.B().Set().Key(key).Value(string(data)).Ex(HeartbeatTTL).Build()).Error()
	if err != nil {
		return fmt.Errorf("failed to store status: %w", err)
	}

	return nil
}

// GetAllStatuses retrieves all worker statuses.
func (m *Monitor) GetAllStatuses(ctx context.Context) ([]Status, error) {
	// Get all worker keys
	keys, err := m.client.Do(ctx, m.client.B().Keys().Pattern("worker:*").Build()).AsStrSlice()
	if err != nil {
		return nil, fmt.Errorf("failed to get worker keys: %w", err)
	}

	statuses := make([]Status, 0, len(keys))

	// Get each worker's status
	for _, key := range keys {
		data, err := m.client.Do(ctx, m.client.B().Get().Key(key).Build()).AsBytes()
		if err != nil {
			m.logger.Error("Failed to get worker status", zap.String("key", key), zap.Error(err))
			continue
		}

		var status Status
		if err := sonic.Unmarshal(data, &status); err != nil {
			m.logger.Error("Failed to unmarshal worker status", zap.String("key", key), zap.Error(err))
			continue
		}

		statuses = append(statuses, status)
	}

	return statuses, nil
}
