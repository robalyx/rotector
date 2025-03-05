package queue

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/bytedance/sonic"
	"github.com/redis/rueidis"
	"go.uber.org/zap"
)

const (
	// QueueInfoExpiry controls how long queue metadata (status, position, priority)
	// remains valid in Redis before automatic cleanup.
	QueueInfoExpiry = 1 * time.Hour

	// QueueStatusPrefix namespaces Redis keys storing item processing states.
	// Keys are formatted as "queue_status:{userID}".
	QueueStatusPrefix = "queue_status:"
	// QueuePositionPrefix namespaces Redis keys tracking item positions.
	// Keys are formatted as "queue_position:{userID}" .
	QueuePositionPrefix = "queue_position:"
	// QueuePriorityPrefix namespaces Redis keys mapping items to priority levels.
	// Keys are formatted as "queue_priority:{userID}".
	QueuePriorityPrefix = "queue_priority:"
)

// Priority represents the processing priority level of a queue item.
type Priority string

const (
	PriorityHigh   Priority = "high"
	PriorityNormal Priority = "normal"
	PriorityLow    Priority = "low"
)

// Status represents the current processing state of a queue item.
type Status string

const (
	StatusPending    Status = "pending"
	StatusProcessing Status = "processing"
	StatusComplete   Status = "complete"
)

// Item encapsulates all metadata needed to process a queued task.
type Item struct {
	UserID      uint64    `json:"userId"`      // Target user for the queued operation
	Priority    Priority  `json:"priority"`    // Processing priority level
	Reason      string    `json:"reason"`      // Why the item was queued
	AddedBy     uint64    `json:"addedBy"`     // User ID who initiated the queue operation
	AddedAt     time.Time `json:"addedAt"`     // Timestamp for FIFO ordering within priority
	Status      Status    `json:"status"`      // Current processing status
	CheckExists bool      `json:"checkExists"` // Whether to verify user exists before processing
}

// Manager orchestrates queue operations using Redis sorted sets for priority queues
// and separate keys for metadata storage. Thread-safe through Redis transactions.
type Manager struct {
	client rueidis.Client // Redis client for queue operations
	logger *zap.Logger    // Structured logging
}

// NewManager initializes a queue manager with its required dependencies.
// The manager uses Redis sorted sets for queue storage and regular keys for metadata.
func NewManager(client rueidis.Client, logger *zap.Logger) *Manager {
	return &Manager{
		client: client,
		logger: logger.Named("queue_manager"),
	}
}

// GetQueueLength returns the length of a queue.
func (m *Manager) GetQueueLength(ctx context.Context, priority Priority) int {
	key := fmt.Sprintf("queue:%s_priority", priority)
	count, err := m.client.Do(ctx, m.client.B().Zcard().Key(key).Build()).ToInt64()
	if err != nil {
		m.logger.Error("Failed to get queue length", zap.Error(err))
		return 0
	}
	return int(count)
}

// AddToQueue adds an item to the queue.
func (m *Manager) AddToQueue(ctx context.Context, item *Item) error {
	// Serialize item to JSON
	itemJSON, err := sonic.Marshal(item)
	if err != nil {
		m.logger.Error("Failed to marshal queue item", zap.Error(err))
		return err
	}

	// Add to sorted set with score as timestamp
	key := fmt.Sprintf("queue:%s_priority", item.Priority)
	err = m.client.Do(ctx,
		m.client.B().Zadd().Key(key).ScoreMember().ScoreMember(float64(item.AddedAt.Unix()), string(itemJSON)).Build(),
	).Error()
	if err != nil {
		m.logger.Error("Failed to add item to queue", zap.Error(err))
		return err
	}

	return nil
}

// GetQueueItems gets items from a queue with the given key and batch size.
func (m *Manager) GetQueueItems(ctx context.Context, key string, batchSize int) ([]string, error) {
	result, err := m.client.Do(ctx,
		m.client.B().Zrange().Key(key).Min("0").Max(strconv.Itoa(batchSize-1)).Build(),
	).AsStrSlice()
	if err != nil {
		m.logger.Error("Failed to get items from queue", zap.Error(err))
		return nil, err
	}

	return result, nil
}

// RemoveQueueItem removes an item from a queue.
func (m *Manager) RemoveQueueItem(ctx context.Context, key string, item *Item) error {
	itemJSON, err := sonic.Marshal(item)
	if err != nil {
		m.logger.Error("Failed to marshal queue item", zap.Error(err))
		return err
	}

	err = m.client.Do(ctx, m.client.B().Zrem().Key(key).Member(string(itemJSON)).Build()).Error()
	if err != nil {
		m.logger.Error("Failed to remove item from queue", zap.Error(err))
		return err
	}

	return nil
}

// UpdateQueueItem updates an item in a queue with a new score.
func (m *Manager) UpdateQueueItem(ctx context.Context, key string, score float64, item *Item) error {
	itemJSON, err := sonic.Marshal(item)
	if err != nil {
		m.logger.Error("Failed to marshal queue item", zap.Error(err))
		return err
	}

	err = m.client.Do(ctx, m.client.B().Zadd().Key(key).ScoreMember().
		ScoreMember(score, string(itemJSON)).Build(),
	).Error()
	if err != nil {
		m.logger.Error("Failed to update item in queue", zap.Error(err))
		return err
	}

	return nil
}

// GetQueueInfo returns the queue status, position, and priority for a user.
func (m *Manager) GetQueueInfo(ctx context.Context, userID uint64) (Status, Priority, int, error) {
	var status Status
	var priority Priority
	var position int

	// Get status
	statusCmd := m.client.Do(ctx, m.client.B().Get().Key(fmt.Sprintf("%s%d", QueueStatusPrefix, userID)).Build())
	if statusCmd.Error() == nil {
		statusStr, _ := statusCmd.ToString()
		status = Status(statusStr)
	}

	// Get priority
	priorityCmd := m.client.Do(ctx, m.client.B().Get().Key(fmt.Sprintf("%s%d", QueuePriorityPrefix, userID)).Build())
	if priorityCmd.Error() == nil {
		priorityStr, _ := priorityCmd.ToString()
		priority = Priority(priorityStr)
	}

	// Get position
	positionCmd := m.client.Do(ctx, m.client.B().Get().Key(fmt.Sprintf("%s%d", QueuePositionPrefix, userID)).Build())
	if positionCmd.Error() == nil {
		posStr, _ := positionCmd.ToString()
		position, _ = strconv.Atoi(posStr)
	}

	return status, priority, position, nil
}

// SetQueueInfo sets the queue status, position, and priority for a user with expiry.
func (m *Manager) SetQueueInfo(
	ctx context.Context, userID uint64, status Status, priority Priority, position int,
) error {
	// Set status with expiry
	if err := m.client.Do(ctx, m.client.B().Set().Key(
		fmt.Sprintf("%s%d", QueueStatusPrefix, userID)).
		Value(string(status)).
		Ex(QueueInfoExpiry).
		Build()).Error(); err != nil {
		return fmt.Errorf("failed to set status: %w", err)
	}

	// Set priority with expiry
	if err := m.client.Do(ctx, m.client.B().Set().Key(
		fmt.Sprintf("%s%d", QueuePriorityPrefix, userID)).
		Value(string(priority)).
		Ex(QueueInfoExpiry).
		Build()).Error(); err != nil {
		return fmt.Errorf("failed to set priority: %w", err)
	}

	// Set position with expiry
	if err := m.client.Do(ctx, m.client.B().Set().Key(
		fmt.Sprintf("%s%d", QueuePositionPrefix, userID)).
		Value(strconv.Itoa(position)).
		Ex(QueueInfoExpiry).
		Build()).Error(); err != nil {
		return fmt.Errorf("failed to set position: %w", err)
	}

	return nil
}
