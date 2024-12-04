package models

import (
	"context"
	"database/sql"
	"errors"

	"github.com/rotector/rotector/internal/common/storage/database/types"
	"github.com/uptrace/bun"
	"go.uber.org/zap"
)

// SettingModel handles database operations for user and bot settings.
type SettingModel struct {
	db     *bun.DB
	logger *zap.Logger
}

// NewSetting creates a SettingModel with database access.
func NewSetting(db *bun.DB, logger *zap.Logger) *SettingModel {
	return &SettingModel{
		db:     db,
		logger: logger,
	}
}

// GetUserSettings retrieves settings for a specific user.
func (r *SettingModel) GetUserSettings(ctx context.Context, userID uint64) (*types.UserSetting, error) {
	settings := &types.UserSetting{
		UserID:           userID,
		StreamerMode:     false,
		UserDefaultSort:  types.SortByRandom,
		GroupDefaultSort: types.SortByRandom,
		ReviewMode:       types.StandardReviewMode,
		ReviewTargetMode: types.FlaggedReviewTarget,
	}

	err := r.db.NewSelect().Model(settings).
		WherePK().
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// Create default settings if none exist
			_, err = r.db.NewInsert().Model(settings).Exec(ctx)
			if err != nil {
				r.logger.Error("Failed to create user settings", zap.Error(err), zap.Uint64("userID", userID))
				return nil, err
			}
		} else {
			r.logger.Error("Failed to get user settings", zap.Error(err), zap.Uint64("userID", userID))
			return nil, err
		}
	}

	return settings, nil
}

// SaveUserSettings updates or creates user settings.
func (r *SettingModel) SaveUserSettings(ctx context.Context, settings *types.UserSetting) error {
	_, err := r.db.NewInsert().Model(settings).
		On("CONFLICT (user_id) DO UPDATE").
		Set("streamer_mode = EXCLUDED.streamer_mode").
		Set("user_default_sort = EXCLUDED.user_default_sort").
		Set("group_default_sort = EXCLUDED.group_default_sort").
		Set("review_mode = EXCLUDED.review_mode").
		Set("review_target_mode = EXCLUDED.review_target_mode").
		Exec(ctx)
	if err != nil {
		r.logger.Error("Failed to save user settings",
			zap.Error(err),
			zap.Uint64("userID", settings.UserID))
		return err
	}

	return nil
}

// GetBotSettings retrieves the bot settings.
func (r *SettingModel) GetBotSettings(ctx context.Context) (*types.BotSetting, error) {
	settings := &types.BotSetting{
		ID:             1,
		ReviewerIDs:    []uint64{},
		AdminIDs:       []uint64{},
		SessionLimit:   0,
		WelcomeMessage: "",
	}

	err := r.db.NewSelect().Model(settings).
		WherePK().
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// Create default settings if none exist
			_, err = r.db.NewInsert().Model(settings).Exec(ctx)
			if err != nil {
				r.logger.Error("Failed to create bot settings", zap.Error(err))
				return nil, err
			}
		} else {
			r.logger.Error("Failed to get bot settings", zap.Error(err))
			return nil, err
		}
	}

	return settings, nil
}

// SaveBotSettings saves bot settings to the database.
func (r *SettingModel) SaveBotSettings(ctx context.Context, settings *types.BotSetting) error {
	_, err := r.db.NewInsert().Model(settings).
		On("CONFLICT (id) DO UPDATE").
		Set("reviewer_ids = EXCLUDED.reviewer_ids").
		Set("admin_ids = EXCLUDED.admin_ids").
		Set("session_limit = EXCLUDED.session_limit").
		Set("welcome_message = EXCLUDED.welcome_message").
		Exec(ctx)
	if err != nil {
		r.logger.Error("Failed to save bot settings", zap.Error(err))
		return err
	}
	return nil
}
