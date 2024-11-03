package database

import (
	"errors"

	"github.com/go-pg/pg/v10"
	"go.uber.org/zap"
)

// SettingRepository handles database operations for user and guild settings.
type SettingRepository struct {
	db     *pg.DB
	logger *zap.Logger
}

// NewSettingRepository creates a SettingRepository with database access for
// storing and retrieving settings.
func NewSettingRepository(db *pg.DB, logger *zap.Logger) *SettingRepository {
	return &SettingRepository{
		db:     db,
		logger: logger,
	}
}

// GetUserSettings retrieves settings for a specific user.
// If no settings exist, it creates default settings.
func (r *SettingRepository) GetUserSettings(userID uint64) (*UserSetting, error) {
	settings := &UserSetting{
		UserID:      userID,
		DefaultSort: SortByRandom,
	}

	err := r.db.Model(settings).
		WherePK().
		Select()
	if err != nil {
		if errors.Is(err, pg.ErrNoRows) {
			// Create default settings if none exist
			_, err = r.db.Model(settings).Insert()
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

// SaveUserSettings updates or creates user settings in the database.
func (r *SettingRepository) SaveUserSettings(settings *UserSetting) error {
	_, err := r.db.Model(settings).
		OnConflict("(user_id) DO UPDATE").
		Set("streamer_mode = EXCLUDED.streamer_mode").
		Set("default_sort = EXCLUDED.default_sort").
		Insert()
	if err != nil {
		r.logger.Error("Failed to save user settings",
			zap.Error(err),
			zap.Uint64("userID", settings.UserID))
		return err
	}

	return nil
}

// SaveGuildSettings saves guild settings to the database.
func (r *SettingRepository) SaveGuildSettings(settings *GuildSetting) error {
	_, err := r.db.Model(settings).
		OnConflict("(guild_id) DO UPDATE").
		Set("whitelisted_roles = EXCLUDED.whitelisted_roles").
		Insert()
	if err != nil {
		r.logger.Error("Failed to save guild settings", zap.Error(err), zap.Uint64("guildID", settings.GuildID))
		return err
	}
	return nil
}

// GetGuildSettings retrieves settings for a specific guild.
// If no settings exist, it creates default settings.
func (r *SettingRepository) GetGuildSettings(guildID uint64) (*GuildSetting, error) {
	settings := &GuildSetting{
		GuildID:          guildID,
		WhitelistedRoles: []uint64{},
	}

	err := r.db.Model(settings).
		WherePK().
		Select()
	if err != nil {
		if errors.Is(err, pg.ErrNoRows) {
			// Create default settings if none exist
			_, err = r.db.Model(settings).Insert()
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
// The role is removed if it exists, or added if it doesn't.
func (r *SettingRepository) ToggleWhitelistedRole(guildID, roleID uint64) error {
	settings, err := r.GetGuildSettings(guildID)
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
	_, err = r.db.Model(settings).
		OnConflict("(guild_id) DO UPDATE").
		Set("whitelisted_roles = EXCLUDED.whitelisted_roles").
		Insert()
	if err != nil {
		r.logger.Error("Failed to toggle whitelisted role",
			zap.Error(err),
			zap.Uint64("guildID", guildID),
			zap.Uint64("roleID", roleID))
		return err
	}

	return nil
}
