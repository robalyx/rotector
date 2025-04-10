package models

import (
	"context"
	"fmt"

	"github.com/robalyx/rotector/internal/database/types"
	"github.com/uptrace/bun"
	"go.uber.org/zap"
)

// IvanModel handles database operations for ivan messages.
type IvanModel struct {
	db     *bun.DB
	logger *zap.Logger
}

// NewIvan creates an IvanModel.
func NewIvan(db *bun.DB, logger *zap.Logger) *IvanModel {
	return &IvanModel{
		db:     db,
		logger: logger.Named("db_ivan"),
	}
}

// GetUserMessages retrieves messages for a user, ordered by date.
func (r *IvanModel) GetUserMessages(ctx context.Context, userID uint64) ([]*types.IvanMessage, error) {
	var messages []*types.IvanMessage
	err := r.db.NewSelect().
		Model(&messages).
		Where("user_id = ?", userID).
		Order("date_time ASC").
		Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get user messages: %w", err)
	}
	return messages, nil
}

// GetUsersMessages retrieves messages for multiple users in a single query.
// Returns a map of user ID to their messages.
func (r *IvanModel) GetUsersMessages(ctx context.Context, userIDs []uint64) (map[uint64][]*types.IvanMessage, error) {
	var messages []*types.IvanMessage
	err := r.db.NewSelect().
		Model(&messages).
		Where("user_id IN (?)", bun.In(userIDs)).
		Order("user_id ASC", "date_time ASC").
		Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get users messages: %w", err)
	}

	// Group messages by user ID
	result := make(map[uint64][]*types.IvanMessage)
	for _, msg := range messages {
		result[msg.UserID] = append(result[msg.UserID], msg)
	}

	return result, nil
}

// GetAndMarkUsersMessages retrieves messages for multiple users and marks them as checked.
// Returns a map of user ID to their messages.
func (r *IvanModel) GetAndMarkUsersMessages(ctx context.Context, userIDs []uint64) (map[uint64][]*types.IvanMessage, error) {
	var messages []*types.IvanMessage
	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Get messages
		err := tx.NewSelect().
			Model(&messages).
			Where("user_id IN (?)", bun.In(userIDs)).
			Order("user_id ASC", "date_time ASC").
			For("UPDATE SKIP LOCKED").
			Scan(ctx)
		if err != nil {
			return fmt.Errorf("failed to get users messages: %w", err)
		}

		// Mark messages as checked
		_, err = tx.NewUpdate().
			Model(&messages).
			Set("was_checked = true").
			Where("user_id IN (?)", bun.In(userIDs)).
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to mark messages as checked: %w", err)
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("transaction failed: %w", err)
	}

	// Group messages by user ID
	result := make(map[uint64][]*types.IvanMessage)
	for _, msg := range messages {
		result[msg.UserID] = append(result[msg.UserID], msg)
	}

	return result, nil
}
