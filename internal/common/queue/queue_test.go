package queue

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/rueidis"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func setupTest(t *testing.T) (*Manager, *miniredis.Miniredis, func()) {
	// Start miniredis server
	mr, err := miniredis.Run()
	require.NoError(t, err)

	// Create Redis client
	client, err := rueidis.NewClient(rueidis.ClientOption{
		InitAddress:  []string{mr.Addr()},
		DisableCache: true,
	})
	require.NoError(t, err)

	// Create test logger
	logger, err := zap.NewDevelopment()
	require.NoError(t, err)

	// Create queue manager
	manager := NewManager(client, logger)

	cleanup := func() {
		mr.Close()
		client.Close()
		logger.Sync()
	}

	return manager, mr, cleanup
}

func TestAddToQueue(t *testing.T) {
	manager, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()
	testItem := &Item{
		UserID:      123,
		Priority:    PriorityNormal,
		Reason:      "test",
		AddedBy:     456,
		AddedAt:     time.Now(),
		Status:      StatusPending,
		CheckExists: true,
	}

	err := manager.AddToQueue(ctx, testItem)
	require.NoError(t, err)

	// Verify queue length
	length := manager.GetQueueLength(ctx, PriorityNormal)
	assert.Equal(t, 1, length)
}

func TestGetQueueItems(t *testing.T) {
	manager, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()
	testItem := &Item{
		UserID:      123,
		Priority:    PriorityNormal,
		Reason:      "test",
		AddedBy:     456,
		AddedAt:     time.Now(),
		Status:      StatusPending,
		CheckExists: true,
	}

	// Add item to queue
	err := manager.AddToQueue(ctx, testItem)
	require.NoError(t, err)

	// Get items from queue
	key := "queue:normal_priority"
	items, err := manager.GetQueueItems(ctx, key, 10)
	require.NoError(t, err)
	assert.Len(t, items, 1)
}

func TestRemoveQueueItem(t *testing.T) {
	manager, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()
	testItem := &Item{
		UserID:      123,
		Priority:    PriorityNormal,
		Reason:      "test",
		AddedBy:     456,
		AddedAt:     time.Now(),
		Status:      StatusPending,
		CheckExists: true,
	}

	// Add and then remove item
	key := "queue:normal_priority"
	err := manager.AddToQueue(ctx, testItem)
	require.NoError(t, err)

	err = manager.RemoveQueueItem(ctx, key, testItem)
	require.NoError(t, err)

	// Verify queue is empty
	length := manager.GetQueueLength(ctx, PriorityNormal)
	assert.Equal(t, 0, length)
}

func TestQueueInfo(t *testing.T) {
	manager, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()
	userID := uint64(123)
	status := StatusPending
	priority := PriorityNormal
	position := 1

	// Set queue info
	err := manager.SetQueueInfo(ctx, userID, status, priority, position)
	require.NoError(t, err)

	// Get and verify queue info
	gotStatus, gotPriority, gotPosition, err := manager.GetQueueInfo(ctx, userID)
	require.NoError(t, err)

	assert.Equal(t, status, gotStatus)
	assert.Equal(t, priority, gotPriority)
	assert.Equal(t, position, gotPosition)
}

func TestUpdateQueueItem(t *testing.T) {
	manager, _, cleanup := setupTest(t)
	defer cleanup()

	ctx := context.Background()
	testItem := &Item{
		UserID:      123,
		Priority:    PriorityNormal,
		Reason:      "test",
		AddedBy:     456,
		AddedAt:     time.Now(),
		Status:      StatusPending,
		CheckExists: true,
	}

	// Add item
	key := "queue:normal_priority"
	err := manager.AddToQueue(ctx, testItem)
	require.NoError(t, err)

	// Update item with new score
	newScore := float64(time.Now().Unix())
	err = manager.UpdateQueueItem(ctx, key, newScore, testItem)
	require.NoError(t, err)

	// Verify item was updated
	items, err := manager.GetQueueItems(ctx, key, 1)
	require.NoError(t, err)
	assert.Len(t, items, 1)
}
