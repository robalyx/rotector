package database

import (
	"errors"

	"github.com/go-pg/pg/v10"
	"go.uber.org/zap"
)

// SettingRepository handles setting-related database operations.
type SettingRepository struct {
	db     *pg.DB
	logger *zap.Logger
}

// NewSettingRepository creates a new SettingRepository instance.
func NewSettingRepository(db *pg.DB, logger *zap.Logger) *SettingRepository {
	return &SettingRepository{
		db:     db,
		logger: logger,
	}
}

// GetUserPreferences retrieves user preferences from the database.
func (r *SettingRepository) GetUserPreferences(userID uint64) (*UserPreference, error) {
	preferences := &UserPreference{UserID: userID}
	err := r.db.Model(preferences).WherePK().Select()
	if err != nil {
		if errors.Is(err, pg.ErrNoRows) {
			// If no preferences found, return default preferences
			return &UserPreference{
				UserID:       userID,
				StreamerMode: false,
				DefaultSort:  SortByRandom,
			}, nil
		}
		r.logger.Error("Failed to get user preferences", zap.Error(err), zap.Uint64("userID", userID))
		return nil, err
	}
	return preferences, nil
}

// GetGuildSettings retrieves guild settings from the database.
func (r *SettingRepository) GetGuildSettings(guildID uint64) (*GuildSetting, error) {
	settings := &GuildSetting{GuildID: guildID}
	err := r.db.Model(settings).WherePK().Select()
	if err != nil {
		if errors.Is(err, pg.ErrNoRows) {
			// If no settings found, return default settings
			return &GuildSetting{GuildID: guildID}, nil
		}
		r.logger.Error("Failed to get guild settings", zap.Error(err), zap.Uint64("guildID", guildID))
		return nil, err
	}
	return settings, nil
}

// SaveUserPreferences saves user preferences to the database.
func (r *SettingRepository) SaveUserPreferences(preferences *UserPreference) error {
	_, err := r.db.Model(preferences).
		OnConflict("(user_id) DO UPDATE").
		Set("streamer_mode = EXCLUDED.streamer_mode").
		Set("default_sort = EXCLUDED.default_sort").
		Insert()
	if err != nil {
		r.logger.Error("Failed to save user preferences", zap.Error(err), zap.Uint64("userID", preferences.UserID))
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

// Add a new function to toggle a role in the whitelist.
func (r *SettingRepository) ToggleWhitelistedRole(guildID uint64, roleID uint64) error {
	settings, err := r.GetGuildSettings(guildID)
	if err != nil {
		return err
	}

	index := -1
	for i, id := range settings.WhitelistedRoles {
		if id == roleID {
			index = i
			break
		}
	}

	if index == -1 {
		settings.WhitelistedRoles = append(settings.WhitelistedRoles, roleID)
	} else {
		settings.WhitelistedRoles = append(settings.WhitelistedRoles[:index], settings.WhitelistedRoles[index+1:]...)
	}

	return r.SaveGuildSettings(settings)
}
