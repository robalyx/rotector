package queue

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/bytedance/sonic"
	"github.com/redis/rueidis"
	"github.com/rotector/rotector/internal/bot/constants"
	"go.uber.org/zap"
)

const (
	// Queue priorities.
	HighPriority   = "high"
	NormalPriority = "normal"
	LowPriority    = "low"

	// Queue status.
	StatusPending  = "pending"
	StatusRunning  = "running"
	StatusComplete = "complete"
	StatusFailed   = "failed"
)

// Item represents a queue item.
type Item struct {
	UserID   uint64    `json:"userId"`
	Priority string    `json:"priority"`
	Reason   string    `json:"reason"`
	AddedBy  uint64    `json:"addedBy"`
	AddedAt  time.Time `json:"addedAt"`
	Status   string    `json:"status"`
}

// Manager handles queue operations.
type Manager struct {
	client rueidis.Client
	logger *zap.Logger
}

// NewManager creates a new queue manager.
func NewManager(client rueidis.Client, logger *zap.Logger) *Manager {
	return &Manager{
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
	key := fmt.Sprintf("queue:%s_priority", item.Priority)

	// Serialize item to JSON
	itemJSON, err := sonic.Marshal(item)
	if err != nil {
		return fmt.Errorf("failed to marshal queue item: %w", err)
	}

	// Add to sorted set with score as timestamp
	err = m.client.Do(context.Background(),
		m.client.B().Zadd().Key(key).ScoreMember().ScoreMember(float64(item.AddedAt.Unix()), string(itemJSON)).Build(),
	).Error()
	if err != nil {
		return fmt.Errorf("failed to add item to queue: %w", err)
	}

	return nil
}

// GetPriorityFromCustomID converts a custom ID to a priority string.
func GetPriorityFromCustomID(customID string) string {
	switch customID {
	case constants.QueueHighPriorityCustomID:
		return HighPriority
	case constants.QueueNormalPriorityCustomID:
		return NormalPriority
	case constants.QueueLowPriorityCustomID:
		return LowPriority
	default:
		return NormalPriority
	}
}

// GetQueueItems gets items from a queue with the given key and batch size.
func (m *Manager) GetQueueItems(key string, batchSize int) ([]string, error) {
	result, err := m.client.Do(context.Background(),
		m.client.B().Zrange().Key(key).Min("0").Max(strconv.Itoa(batchSize-1)).Build(),
	).AsStrSlice()
	if err != nil {
		return nil, fmt.Errorf("failed to get items from queue: %w", err)
	}

	return result, nil
}

// RemoveQueueItem removes an item from a queue.
func (m *Manager) RemoveQueueItem(key string, item *Item) error {
	itemJSON, err := sonic.Marshal(item)
	if err != nil {
		return fmt.Errorf("failed to marshal queue item: %w", err)
	}

	err = m.client.Do(context.Background(),
		m.client.B().Zrem().Key(key).Member(string(itemJSON)).Build(),
	).Error()
	if err != nil {
		return fmt.Errorf("failed to remove item from queue: %w", err)
	}

	return nil
}

// UpdateQueueItem updates an item in a queue with a new score.
func (m *Manager) UpdateQueueItem(key string, score float64, item *Item) error {
	itemJSON, err := sonic.Marshal(item)
	if err != nil {
		return fmt.Errorf("failed to marshal queue item: %w", err)
	}

	err = m.client.Do(context.Background(),
		m.client.B().Zadd().Key(key).ScoreMember().
			ScoreMember(score, string(itemJSON)).Build(),
	).Error()
	if err != nil {
		return fmt.Errorf("failed to update item in queue: %w", err)
	}

	return nil
}
