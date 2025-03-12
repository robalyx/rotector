package models

import (
	"context"
	"fmt"
	"time"

	"github.com/robalyx/rotector/internal/common/storage/database/types"
	"github.com/uptrace/bun"
	"go.uber.org/zap"
)

// MessageModel handles database operations for inappropriate Discord messages.
type MessageModel struct {
	db     *bun.DB
	logger *zap.Logger
}

// NewMessage creates a new message model instance.
func NewMessage(db *bun.DB, logger *zap.Logger) *MessageModel {
	return &MessageModel{
		db:     db,
		logger: logger.Named("db_message"),
	}
}

// BatchStoreInappropriateMessages stores multiple inappropriate messages.
func (m *MessageModel) BatchStoreInappropriateMessages(
	ctx context.Context, messages []*types.InappropriateMessage,
) error {
	if len(messages) == 0 {
		return nil
	}

	_, err := m.db.NewInsert().
		Model(&messages).
		On("CONFLICT (server_id, channel_id, user_id, message_id) DO UPDATE").
		Set("content = EXCLUDED.content").
		Set("reason = EXCLUDED.reason").
		Set("confidence = EXCLUDED.confidence").
		Set("updated_at = EXCLUDED.updated_at").
		Exec(ctx)

	return err
}

// BatchUpdateUserSummaries updates multiple user summaries.
func (m *MessageModel) BatchUpdateUserSummaries(
	ctx context.Context, summaries []*types.InappropriateUserSummary,
) error {
	if len(summaries) == 0 {
		return nil
	}

	_, err := m.db.NewInsert().
		Model(&summaries).
		On("CONFLICT (user_id) DO UPDATE").
		Set("reason = EXCLUDED.reason").
		Set("message_count = ?TableAlias.message_count + EXCLUDED.message_count").
		Set("last_detected = EXCLUDED.last_detected").
		Set("updated_at = EXCLUDED.updated_at").
		Exec(ctx)

	return err
}

// GetUserInappropriateMessages retrieves inappropriate messages for a specific user in a server.
func (m *MessageModel) GetUserInappropriateMessages(
	ctx context.Context, serverID uint64, userID uint64, limit int,
) ([]*types.InappropriateMessage, error) {
	var messages []*types.InappropriateMessage

	query := m.db.NewSelect().
		Model(&messages).
		Where("server_id = ?", serverID).
		Where("user_id = ?", userID).
		Order("detected_at DESC")

	if limit > 0 {
		query = query.Limit(limit)
	}

	err := query.Scan(ctx)
	if err != nil {
		return nil, err
	}

	return messages, nil
}

// GetUserInappropriateMessageSummaries retrieves summaries of inappropriate messages for multiple users.
func (m *MessageModel) GetUserInappropriateMessageSummaries(
	ctx context.Context, userIDs []uint64,
) (map[uint64]*types.InappropriateUserSummary, error) {
	var summaries []*types.InappropriateUserSummary

	err := m.db.NewSelect().
		Model(&summaries).
		Where("user_id IN (?)", bun.In(userIDs)).
		Scan(ctx)
	if err != nil {
		return nil, err
	}

	// Convert slice to map for easier lookup
	summaryMap := make(map[uint64]*types.InappropriateUserSummary)
	for _, summary := range summaries {
		summaryMap[summary.UserID] = summary
	}

	return summaryMap, nil
}

// GetUserMessagesByCursor retrieves paginated inappropriate messages for a user in a server .
func (m *MessageModel) GetUserMessagesByCursor(
	ctx context.Context, serverID uint64, userID uint64, cursor *types.MessageCursor, limit int,
) ([]*types.InappropriateMessage, *types.MessageCursor, error) {
	var messages []*types.InappropriateMessage

	// Build base query
	query := m.db.NewSelect().
		Model(&messages).
		Where("server_id = ?", serverID).
		Where("user_id = ?", userID).
		Limit(limit + 1) // Get one extra to determine if there's a next page

	// Apply cursor conditions if provided
	if cursor != nil {
		query = query.Where("(detected_at, message_id) < (?, ?)",
			cursor.DetectedAt,
			cursor.MessageID,
		)
	}

	// Order by detection time and message ID for stable pagination
	query = query.Order("detected_at DESC", "message_id DESC")

	// Execute query
	err := query.Scan(ctx)
	if err != nil {
		return nil, nil, err
	}

	// Check if we have a next page
	var nextCursor *types.MessageCursor
	if len(messages) > limit {
		lastMsg := messages[limit] // Get the extra message we fetched
		nextCursor = &types.MessageCursor{
			DetectedAt: lastMsg.DetectedAt,
			MessageID:  lastMsg.MessageID,
		}
		messages = messages[:limit] // Remove the extra message from results
	}

	return messages, nextCursor, nil
}

// GetUserMessageGuilds returns a list of guild IDs where a user has inappropriate messages.
func (m *MessageModel) GetUserMessageGuilds(ctx context.Context, userID uint64) ([]uint64, error) {
	var guildIDs []uint64
	err := m.db.NewSelect().
		Model((*types.InappropriateMessage)(nil)).
		ColumnExpr("DISTINCT server_id").
		Where("user_id = ?", userID).
		Scan(ctx, &guildIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to get user message guilds: %w", err)
	}
	return guildIDs, nil
}

// GetUserInappropriateMessageSummary retrieves the inappropriate message summary for a specific user.
func (m *MessageModel) GetUserInappropriateMessageSummary(
	ctx context.Context, userID uint64,
) (*types.InappropriateUserSummary, error) {
	var summary types.InappropriateUserSummary

	err := m.db.NewSelect().
		Model(&summary).
		Where("user_id = ?", userID).
		Scan(ctx)
	if err != nil {
		return nil, err
	}

	return &summary, nil
}

// GetUserMessageSummaries retrieves message summaries for specific users.
func (m *MessageModel) GetUserMessageSummaries(
	ctx context.Context, userIDs []uint64,
) (map[uint64]*types.InappropriateUserSummary, error) {
	var summaries []*types.InappropriateUserSummary

	err := m.db.NewSelect().
		Model(&summaries).
		Where("user_id IN (?)", bun.In(userIDs)).
		Scan(ctx)
	if err != nil {
		return nil, err
	}

	// Convert slice to map for easier lookup
	summaryMap := make(map[uint64]*types.InappropriateUserSummary)
	for _, summary := range summaries {
		summaryMap[summary.UserID] = summary
	}

	return summaryMap, nil
}

// GetUniqueInappropriateUserCount returns the number of unique users with inappropriate messages.
func (m *MessageModel) GetUniqueInappropriateUserCount(ctx context.Context) (int, error) {
	count, err := m.db.NewSelect().
		Model((*types.InappropriateUserSummary)(nil)).
		ColumnExpr("DISTINCT user_id").
		Count(ctx)
	if err != nil {
		return 0, err
	}
	return count, nil
}

// DeleteUserMessages deletes all inappropriate messages for a specific user.
func (m *MessageModel) DeleteUserMessages(ctx context.Context, userID uint64) error {
	// Delete from inappropriate_messages table
	_, err := m.db.NewDelete().
		Model((*types.InappropriateMessage)(nil)).
		Where("user_id = ?", userID).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to delete user messages: %w", err)
	}

	// Delete from inappropriate_user_summaries table
	_, err = m.db.NewDelete().
		Model((*types.InappropriateUserSummary)(nil)).
		Where("user_id = ?", userID).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to delete user message summary: %w", err)
	}

	return nil
}

// RedactUserMessages redacts the content of all inappropriate messages for a specific user.
func (m *MessageModel) RedactUserMessages(ctx context.Context, userID uint64) error {
	// Update message content and detected_at in inappropriate_messages table
	_, err := m.db.NewUpdate().
		Model((*types.InappropriateMessage)(nil)).
		Set("content = '[redacted]'").
		Set("detected_at = ?", time.Time{}).
		Set("updated_at = ?", time.Now()).
		Where("user_id = ?", userID).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to redact user messages: %w", err)
	}

	// Update user summary
	_, err = m.db.NewUpdate().
		Model((*types.InappropriateUserSummary)(nil)).
		Set("message_count = 0").
		Set("last_detected = ?", time.Time{}).
		Set("updated_at = ?", time.Now()).
		Where("user_id = ?", userID).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to update user message summary: %w", err)
	}

	return nil
}
