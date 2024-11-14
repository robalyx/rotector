package database

import (
	"context"
	"database/sql"
	"errors"
	"slices"

	"github.com/disgoorg/snowflake/v2"
	"github.com/uptrace/bun"
	"go.uber.org/zap"
)

// UserSetting stores user-specific preferences.
type UserSetting struct {
	UserID       uint64 `bun:",pk"`
	StreamerMode bool   `bun:",notnull"`
	DefaultSort  string `bun:",notnull"`
}

// GuildSetting stores server-wide configuration options.
type GuildSetting struct {
	GuildID          uint64   `bun:",pk"`
	WhitelistedRoles []uint64 `bun:"type:bigint[]"`
}

// HasAnyRole checks if any of the provided role IDs match the whitelisted roles.
// Returns true if there is at least one match, false otherwise.
func (s *GuildSetting) HasAnyRole(roleIDs []snowflake.ID) bool {
	for _, roleID := range roleIDs {
		if slices.Contains(s.WhitelistedRoles, uint64(roleID)) {
			return true
		}
	}
	return false
}

// SettingRepository handles database operations for user and guild settings.
type SettingRepository struct {
	db     *bun.DB
	logger *zap.Logger
}

// NewSettingRepository creates a SettingRepository with database access.
func NewSettingRepository(db *bun.DB, logger *zap.Logger) *SettingRepository {
	return &SettingRepository{
		db:     db,
		logger: logger,
	}
}

// GetUserSettings retrieves settings for a specific user.
func (r *SettingRepository) GetUserSettings(ctx context.Context, userID uint64) (*UserSetting, error) {
	settings := &UserSetting{
		UserID:      userID,
		DefaultSort: SortByRandom,
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
func (r *SettingRepository) SaveUserSettings(ctx context.Context, settings *UserSetting) error {
	_, err := r.db.NewInsert().Model(settings).
		On("CONFLICT (user_id) DO UPDATE").
		Set("streamer_mode = EXCLUDED.streamer_mode").
		Set("default_sort = EXCLUDED.default_sort").
		Exec(ctx)
	if err != nil {
		r.logger.Error("Failed to save user settings",
			zap.Error(err),
			zap.Uint64("userID", settings.UserID))
		return err
	}

	return nil
}

// SaveGuildSettings saves guild settings to the database.
func (r *SettingRepository) SaveGuildSettings(ctx context.Context, settings *GuildSetting) error {
	_, err := r.db.NewInsert().Model(settings).
		On("CONFLICT (guild_id) DO UPDATE").
		Set("whitelisted_roles = EXCLUDED.whitelisted_roles").
		Exec(ctx)
	if err != nil {
		r.logger.Error("Failed to save guild settings", zap.Error(err), zap.Uint64("guildID", settings.GuildID))
		return err
	}
	return nil
}

// GetGuildSettings retrieves settings for a specific guild.
func (r *SettingRepository) GetGuildSettings(ctx context.Context, guildID uint64) (*GuildSetting, error) {
	settings := &GuildSetting{
		GuildID:          guildID,
		WhitelistedRoles: []uint64{},
	}

	err := r.db.NewSelect().Model(settings).
		WherePK().
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// Create default settings if none exist
			_, err = r.db.NewInsert().Model(settings).Exec(ctx)
			if err != nil {
				r.logger.Error("Failed to create guild settings", zap.Error(err), zap.Uint64("guildID", guildID))
				return nil, err
			}
		} else {
			r.logger.Error("Failed to get guild settings", zap.Error(err), zap.Uint64("guildID", guildID))
			return nil, err
		}
	}

	return settings, nil
}

// ToggleWhitelistedRole adds or removes a role from a guild's whitelist.
func (r *SettingRepository) ToggleWhitelistedRole(ctx context.Context, guildID, roleID uint64) error {
	settings, err := r.GetGuildSettings(ctx, guildID)
	if err != nil {
		return err
	}

	// Remove role if it exists, add if it doesn't
	roleExists := false
	for i, existingRoleID := range settings.WhitelistedRoles {
		if existingRoleID == roleID {
			settings.WhitelistedRoles = append(
				settings.WhitelistedRoles[:i],
				settings.WhitelistedRoles[i+1:]...,
			)
			roleExists = true
			break
		}
	}

	if !roleExists {
		settings.WhitelistedRoles = append(settings.WhitelistedRoles, roleID)
	}

	// Save updated settings
	_, err = r.db.NewInsert().Model(settings).
		On("CONFLICT (guild_id) DO UPDATE").
		Set("whitelisted_roles = EXCLUDED.whitelisted_roles").
		Exec(ctx)
	if err != nil {
		r.logger.Error("Failed to toggle whitelisted role",
			zap.Error(err),
			zap.Uint64("guildID", guildID),
			zap.Uint64("roleID", roleID))
		return err
	}

	return nil
}
