package models

import (
	"context"
	"database/sql"
	"errors"

	"github.com/uptrace/bun"
	"go.uber.org/zap"
)

// Review Modes.
const (
	TrainingReviewMode = "training"
	StandardReviewMode = "standard"
)

// Review Target Modes.
const (
	FlaggedReviewTarget   = "flagged"
	ConfirmedReviewTarget = "confirmed"
)

// FormatReviewMode converts the review mode constant to a user-friendly display string.
func FormatReviewMode(mode string) string {
	switch mode {
	case TrainingReviewMode:
		return "Training Mode"
	case StandardReviewMode:
		return "Standard Mode"
	default:
		return "Unknown Mode"
	}
}

// FormatReviewTargetMode converts the target mode to display string.
func FormatReviewTargetMode(mode string) string {
	switch mode {
	case FlaggedReviewTarget:
		return "Flagged Items"
	case ConfirmedReviewTarget:
		return "Confirmed Items"
	default:
		return "Unknown Target"
	}
}

// UserSetting stores user-specific preferences.
type UserSetting struct {
	UserID           uint64 `bun:",pk"`
	StreamerMode     bool   `bun:",notnull"`
	UserDefaultSort  string `bun:",notnull"`
	GroupDefaultSort string `bun:",notnull"`
	ReviewMode       string `bun:",notnull"`
	ReviewTargetMode string `bun:",notnull"`
}

// BotSetting stores bot-wide configuration options.
type BotSetting struct {
	ID           uint64   `bun:",pk,autoincrement"`
	ReviewerIDs  []uint64 `bun:"reviewer_ids,type:bigint[]"`
	AdminIDs     []uint64 `bun:"admin_ids,type:bigint[]"`
	SessionLimit uint64   `bun:",notnull"`
}

// IsAdmin checks if the given user ID is in the admin list.
func (s *BotSetting) IsAdmin(userID uint64) bool {
	for _, adminID := range s.AdminIDs {
		if adminID == userID {
			return true
		}
	}
	return false
}

// IsReviewer checks if the given user ID is in the reviewer list.
func (s *BotSetting) IsReviewer(userID uint64) bool {
	for _, reviewerID := range s.ReviewerIDs {
		if reviewerID == userID {
			return true
		}
	}
	return false
}

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
func (r *SettingModel) GetUserSettings(ctx context.Context, userID uint64) (*UserSetting, error) {
	settings := &UserSetting{
		UserID:           userID,
		StreamerMode:     false,
		UserDefaultSort:  SortByRandom,
		GroupDefaultSort: SortByRandom,
		ReviewMode:       StandardReviewMode,
		ReviewTargetMode: FlaggedReviewTarget,
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
func (r *SettingModel) SaveUserSettings(ctx context.Context, settings *UserSetting) error {
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
func (r *SettingModel) GetBotSettings(ctx context.Context) (*BotSetting, error) {
	settings := &BotSetting{
		ID:           1,
		ReviewerIDs:  []uint64{},
		AdminIDs:     []uint64{},
		SessionLimit: 0,
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
func (r *SettingModel) SaveBotSettings(ctx context.Context, settings *BotSetting) error {
	_, err := r.db.NewInsert().Model(settings).
		On("CONFLICT (id) DO UPDATE").
		Set("reviewer_ids = EXCLUDED.reviewer_ids").
		Set("admin_ids = EXCLUDED.admin_ids").
		Set("session_limit = EXCLUDED.session_limit").
		Exec(ctx)
	if err != nil {
		r.logger.Error("Failed to save bot settings", zap.Error(err))
		return err
	}
	return nil
}
