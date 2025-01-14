package queue

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/bytedance/sonic"
	"github.com/redis/rueidis"
	"github.com/robalyx/rotector/internal/common/storage/database"
	"github.com/robalyx/rotector/internal/common/storage/database/types"
	"github.com/robalyx/rotector/internal/common/storage/database/types/enum"
	"go.uber.org/zap"
)

const (
	// QueueInfoExpiry controls how long queue metadata (status, position, priority)
	// remains valid in Redis before automatic cleanup.
	QueueInfoExpiry = 1 * time.Hour

	// HighPriority items are processed first through a dedicated Redis sorted set.
	// Used for urgent operations that need immediate attention.
	HighPriority = "high"
	// NormalPriority items are processed after high priority items.
	// Represents the default processing tier for standard operations.
	NormalPriority = "normal"
	// LowPriority items are processed last, allowing higher priority items to skip ahead.
	// Suitable for background tasks that can tolerate delays.
	LowPriority = "low"

	// StatusPending indicates an item is waiting in the queue.
	// Initial state for all newly added items before processing begins.
	StatusPending = "Pending"
	// StatusProcessing indicates an item is currently being handled.
	// Prevents multiple workers from processing the same item simultaneously.
	StatusProcessing = "Processing"
	// StatusComplete indicates successful processing of an item.
	// Terminal state for items that finished their queue lifecycle normally.
	StatusComplete = "Complete"
	// StatusSkipped indicates an item was intentionally not processed.
	// Terminal state for items that were removed from the queue without processing.
	StatusSkipped = "Skipped"

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

// Item encapsulates all metadata needed to process a queued task.
type Item struct {
	UserID      uint64    `json:"userId"`      // Target user for the queued operation
	Priority    string    `json:"priority"`    // Processing priority level
	Reason      string    `json:"reason"`      // Why the item was queued
	AddedBy     uint64    `json:"addedBy"`     // User ID who initiated the queue operation
	AddedAt     time.Time `json:"addedAt"`     // Timestamp for FIFO ordering within priority
	Status      string    `json:"status"`      // Current processing status
	CheckExists bool      `json:"checkExists"` // Whether to verify user exists before processing
}

// Manager orchestrates queue operations using Redis sorted sets for priority queues
// and separate keys for metadata storage. Thread-safe through Redis transactions.
type Manager struct {
	db     *database.Client // For persistent storage and activity logging
	client rueidis.Client   // Redis client for queue operations
	logger *zap.Logger      // Structured logging
}

// NewManager initializes a queue manager with its required dependencies.
// The manager uses Redis sorted sets for queue storage and regular keys for metadata.
func NewManager(db *database.Client, client rueidis.Client, logger *zap.Logger) *Manager {
	return &Manager{
		db:     db,
		client: client,
		logger: logger,
	}
}

// GetQueueLength returns the length of a queue.
func (m *Manager) GetQueueLength(ctx context.Context, priority string) int {
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

	// Log the activity
	go m.db.Activity().Log(ctx, &types.ActivityLog{
		ActivityTarget: types.ActivityTarget{
			UserID: item.UserID,
		},
		ReviewerID:        item.AddedBy,
		ActivityType:      enum.ActivityTypeUserRechecked,
		ActivityTimestamp: time.Now(),
		Details:           map[string]interface{}{"reason": item.Reason},
	})

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
func (m *Manager) GetQueueInfo(ctx context.Context, userID uint64) (status, priority string, position int, err error) {
	// Get status
	statusCmd := m.client.Do(ctx, m.client.B().Get().Key(fmt.Sprintf("%s%d", QueueStatusPrefix, userID)).Build())
	if statusCmd.Error() == nil {
		status, _ = statusCmd.ToString()
	}

	// Get priority
	priorityCmd := m.client.Do(ctx, m.client.B().Get().Key(fmt.Sprintf("%s%d", QueuePriorityPrefix, userID)).Build())
	if priorityCmd.Error() == nil {
		priority, _ = priorityCmd.ToString()
	}

	// Get position
	positionCmd := m.client.Do(ctx, m.client.B().Get().Key(fmt.Sprintf("%s%d", QueuePositionPrefix, userID)).Build())
	if positionCmd.Error() == nil {
		pos, _ := positionCmd.ToString()
		position, _ = strconv.Atoi(pos)
	}

	return
}

// SetQueueInfo sets the queue status, position, and priority for a user with expiry.
func (m *Manager) SetQueueInfo(ctx context.Context, userID uint64, status, priority string, position int) error {
	// Set status with expiry
	if err := m.client.Do(ctx, m.client.B().Set().Key(
		fmt.Sprintf("%s%d", QueueStatusPrefix, userID)).
		Value(status).
		Ex(QueueInfoExpiry).
		Build()).Error(); err != nil {
		return fmt.Errorf("failed to set status: %w", err)
	}

	// Set priority with expiry
	if err := m.client.Do(ctx, m.client.B().Set().Key(
		fmt.Sprintf("%s%d", QueuePriorityPrefix, userID)).
		Value(priority).
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
