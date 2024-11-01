package queue

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/bytedance/sonic"
	"github.com/redis/rueidis"
	"github.com/rotector/rotector/internal/common/database"
	"go.uber.org/zap"
)

const (
	// QueueInfoExpiry is how long queue info should be kept.
	QueueInfoExpiry = 24 * time.Hour
	AbortedExpiry   = 24 * time.Hour

	// Queue priorities.
	HighPriority   = "high"
	NormalPriority = "normal"
	LowPriority    = "low"

	// Queue status.
	StatusPending    = "Pending"
	StatusProcessing = "Processing"
	StatusComplete   = "Complete"
	StatusSkipped    = "Skipped"

	// Redis key prefixes.
	QueueStatusPrefix   = "queue_status:"
	QueuePositionPrefix = "queue_position:"
	QueuePriorityPrefix = "queue_priority:"

	// Redis key prefixes for aborted items.
	abortedPrefix = "aborted:"
)

// Item represents a queue item.
type Item struct {
	UserID      uint64    `json:"userId"`
	Priority    string    `json:"priority"`
	Reason      string    `json:"reason"`
	AddedBy     uint64    `json:"addedBy"`
	AddedAt     time.Time `json:"addedAt"`
	Status      string    `json:"status"`
	CheckExists bool      `json:"checkExists"`
}

// Manager handles queue operations.
type Manager struct {
	db     *database.Database
	client rueidis.Client
	logger *zap.Logger
}

// NewManager creates a new queue manager.
func NewManager(db *database.Database, client rueidis.Client, logger *zap.Logger) *Manager {
	return &Manager{
		db:     db,
		client: client,
		logger: logger,
	}
}

// GetQueueLength returns the length of a queue.
func (m *Manager) GetQueueLength(priority string) int {
	key := fmt.Sprintf("queue:%s_priority", priority)
	count, err := m.client.Do(context.Background(), m.client.B().Zcard().Key(key).Build()).ToInt64()
	if err != nil {
		m.logger.Error("Failed to get queue length", zap.Error(err))
		return 0
	}
	return int(count)
}

// AddToQueue adds an item to the queue.
func (m *Manager) AddToQueue(item *Item) error {
	// If user was previously aborted, remove the abort status
	// This allows the user to be requeued by another reviewer
	if m.IsAborted(item.UserID) {
		if err := m.ClearAborted(item.UserID); err != nil {
			return err
		}
	}

	// Serialize item to JSON
	itemJSON, err := sonic.Marshal(item)
	if err != nil {
		m.logger.Error("Failed to marshal queue item", zap.Error(err))
		return err
	}

	// Add to sorted set with score as timestamp
	key := fmt.Sprintf("queue:%s_priority", item.Priority)
	err = m.client.Do(context.Background(),
		m.client.B().Zadd().Key(key).ScoreMember().ScoreMember(float64(item.AddedAt.Unix()), string(itemJSON)).Build(),
	).Error()
	if err != nil {
		m.logger.Error("Failed to add item to queue", zap.Error(err))
		return err
	}

	// Log the activity
	go m.db.UserActivity().LogActivity(&database.UserActivityLog{
		UserID:            item.UserID,
		ReviewerID:        item.AddedBy,
		ActivityType:      database.ActivityTypeRechecked,
		ActivityTimestamp: time.Now(),
		Details:           map[string]interface{}{"reason": item.Reason},
	})

	return nil
}

// GetQueueItems gets items from a queue with the given key and batch size.
func (m *Manager) GetQueueItems(key string, batchSize int) ([]string, error) {
	result, err := m.client.Do(context.Background(),
		m.client.B().Zrange().Key(key).Min("0").Max(strconv.Itoa(batchSize-1)).Build(),
	).AsStrSlice()
	if err != nil {
		m.logger.Error("Failed to get items from queue", zap.Error(err))
		return nil, err
	}

	return result, nil
}

// RemoveQueueItem removes an item from a queue.
func (m *Manager) RemoveQueueItem(key string, item *Item) error {
	itemJSON, err := sonic.Marshal(item)
	if err != nil {
		m.logger.Error("Failed to marshal queue item", zap.Error(err))
		return err
	}

	err = m.client.Do(context.Background(),
		m.client.B().Zrem().Key(key).Member(string(itemJSON)).Build(),
	).Error()
	if err != nil {
		m.logger.Error("Failed to remove item from queue", zap.Error(err))
		return err
	}

	return nil
}

// UpdateQueueItem updates an item in a queue with a new score.
func (m *Manager) UpdateQueueItem(key string, score float64, item *Item) error {
	itemJSON, err := sonic.Marshal(item)
	if err != nil {
		m.logger.Error("Failed to marshal queue item", zap.Error(err))
		return err
	}

	err = m.client.Do(context.Background(),
		m.client.B().Zadd().Key(key).ScoreMember().
			ScoreMember(score, string(itemJSON)).Build(),
	).Error()
	if err != nil {
		m.logger.Error("Failed to update item in queue", zap.Error(err))
		return err
	}

	return nil
}

// GetQueueInfo returns the queue status, position, and priority for a user.
func (m *Manager) GetQueueInfo(userID uint64) (status, priority string, position int, err error) {
	ctx := context.Background()

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
func (m *Manager) SetQueueInfo(userID uint64, status, priority string, position int) error {
	ctx := context.Background()

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

// ClearQueueInfo removes all queue info for a user.
func (m *Manager) ClearQueueInfo(userID uint64) error {
	ctx := context.Background()

	// Delete status
	if err := m.client.Do(ctx, m.client.B().Del().Key(
		fmt.Sprintf("%s%d", QueueStatusPrefix, userID)).Build()).Error(); err != nil {
		return fmt.Errorf("failed to delete status: %w", err)
	}

	// Delete priority
	if err := m.client.Do(ctx, m.client.B().Del().Key(
		fmt.Sprintf("%s%d", QueuePriorityPrefix, userID)).Build()).Error(); err != nil {
		return fmt.Errorf("failed to delete priority: %w", err)
	}

	// Delete position
	if err := m.client.Do(ctx, m.client.B().Del().Key(
		fmt.Sprintf("%s%d", QueuePositionPrefix, userID)).Build()).Error(); err != nil {
		return fmt.Errorf("failed to delete position: %w", err)
	}

	return nil
}

// MarkAsAborted marks a user ID as aborted.
func (m *Manager) MarkAsAborted(userID uint64) error {
	ctx := context.Background()

	// Set aborted flag with 24 hour expiry (cleanup)
	err := m.client.Do(ctx, m.client.B().Set().
		Key(fmt.Sprintf("%s%d", abortedPrefix, userID)).
		Value("1").
		Ex(AbortedExpiry).
		Build()).Error()
	if err != nil {
		return fmt.Errorf("failed to mark user as aborted: %w", err)
	}

	return nil
}

// IsAborted checks if a user ID has been marked as aborted.
func (m *Manager) IsAborted(userID uint64) bool {
	ctx := context.Background()

	result := m.client.Do(ctx, m.client.B().Get().
		Key(fmt.Sprintf("%s%d", abortedPrefix, userID)).
		Build())

	return result.Error() == nil
}

// ClearAborted removes the aborted status for a user ID.
func (m *Manager) ClearAborted(userID uint64) error {
	ctx := context.Background()

	err := m.client.Do(ctx, m.client.B().Del().
		Key(fmt.Sprintf("%s%d", abortedPrefix, userID)).
		Build()).Error()
	if err != nil {
		return fmt.Errorf("failed to clear aborted status: %w", err)
	}

	return nil
}
