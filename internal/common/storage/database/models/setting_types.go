package models

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/rotector/rotector/internal/bot/constants"
	"github.com/rotector/rotector/internal/common/utils"
)

// Common validation errors.
var (
	ErrInvalidIDFormat  = errors.New("invalid Discord ID format")
	ErrSelfAssignment   = errors.New("you cannot add/remove yourself")
	ErrInvalidOption    = errors.New("invalid option selected")
	ErrInvalidBoolValue = errors.New("value must be true or false")
)

// SettingType represents the data type of a setting.
type SettingType string

// Available setting types.
const (
	SettingTypeBool   SettingType = "bool"
	SettingTypeEnum   SettingType = "enum"
	SettingTypeID     SettingType = "id"
	SettingTypeNumber SettingType = "number"
)

// SettingValidator is a function that validates setting input.
// It takes the value to validate and the ID of the user making the change.
type SettingValidator func(value string, userID uint64) error

// SettingValueGetter is a function that retrieves the value of a setting.
type SettingValueGetter func(userSettings *UserSetting, botSettings *BotSetting) string

// SettingValueUpdater is a function that updates the value of a setting.
type SettingValueUpdater func(value string, userSettings *UserSetting, botSettings *BotSetting) error

// Setting defines the structure and behavior of a single setting.
type Setting struct {
	Key          string              `json:"key"`          // Unique identifier for the setting
	Name         string              `json:"name"`         // Display name
	Description  string              `json:"description"`  // Help text explaining the setting
	Type         SettingType         `json:"type"`         // Data type of the setting
	DefaultValue interface{}         `json:"defaultValue"` // Default value
	Options      []SettingOption     `json:"options"`      // Available options for enum types
	Validators   []SettingValidator  `json:"-"`            // Functions to validate input
	ValueGetter  SettingValueGetter  `json:"-"`            // Function to retrieve the value
	ValueUpdater SettingValueUpdater `json:"-"`            // Function to update the value
}

// SettingOption represents a single option for enum-type settings.
type SettingOption struct {
	Value       string `json:"value"`       // Internal value used by the system
	Label       string `json:"label"`       // Display name shown to users
	Description string `json:"description"` // Help text explaining the option
	Emoji       string `json:"emoji"`       // Optional emoji to display with the option
}

// SettingRegistry manages the available settings.
type SettingRegistry struct {
	UserSettings map[string]Setting // User-specific settings
	BotSettings  map[string]Setting // Bot-wide settings
}

// validateDiscordID checks if a string is a valid Discord user ID.
// It prevents self-assignment and ensures proper ID format.
func validateDiscordID(value string, userID uint64) error {
	// Parse the value ID
	id, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidIDFormat, err)
	}

	// Prevent self-assignment
	if id == userID {
		return ErrSelfAssignment
	}

	return nil
}

// validateEnum returns a validator function that checks if a value is in a list of valid options.
func validateEnum(validOptions []string) SettingValidator {
	return func(value string, _ uint64) error {
		for _, opt := range validOptions {
			if value == opt {
				return nil
			}
		}
		return ErrInvalidOption
	}
}

// validateBool checks if a string is a valid boolean value ("true" or "false").
func validateBool(value string, _ uint64) error {
	if value != "true" && value != "false" {
		return ErrInvalidBoolValue
	}
	return nil
}

// validateNumber checks if a string is a valid number.
func validateNumber(value string, _ uint64) error {
	_, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return fmt.Errorf("value must be a valid number: %w", err)
	}
	return nil
}

// NewSettingRegistry creates and initializes the setting registry.
func NewSettingRegistry() *SettingRegistry {
	r := &SettingRegistry{
		UserSettings: make(map[string]Setting),
		BotSettings:  make(map[string]Setting),
	}

	r.registerUserSettings()
	r.registerBotSettings()

	return r
}

// registerUserSettings adds all user-specific settings to the registry.
func (r *SettingRegistry) registerUserSettings() {
	r.UserSettings[constants.StreamerModeOption] = r.createStreamerModeSetting()
	r.UserSettings[constants.UserDefaultSortOption] = r.createUserDefaultSortSetting()
	r.UserSettings[constants.GroupDefaultSortOption] = r.createGroupDefaultSortSetting()
	r.UserSettings[constants.ReviewModeOption] = r.createReviewModeSetting()
	r.UserSettings[constants.ReviewTargetModeOption] = r.createReviewTargetModeSetting()
}

// registerBotSettings adds all bot-wide settings to the registry.
func (r *SettingRegistry) registerBotSettings() {
	r.BotSettings[constants.ReviewerIDsOption] = r.createReviewerIDsSetting()
	r.BotSettings[constants.AdminIDsOption] = r.createAdminIDsSetting()
	r.BotSettings[constants.SessionLimitOption] = r.createSessionLimitSetting()
}

// createStreamerModeSetting creates the streamer mode setting.
func (r *SettingRegistry) createStreamerModeSetting() Setting {
	return Setting{
		Key:          constants.StreamerModeOption,
		Name:         "Streamer Mode",
		Description:  "Toggle censoring of sensitive information",
		Type:         SettingTypeBool,
		DefaultValue: false,
		Validators:   []SettingValidator{validateBool},
		ValueGetter: func(us *UserSetting, _ *BotSetting) string {
			return strconv.FormatBool(us.StreamerMode)
		},
		ValueUpdater: func(value string, us *UserSetting, _ *BotSetting) error {
			boolVal, _ := strconv.ParseBool(value)
			us.StreamerMode = boolVal
			return nil
		},
	}
}

// createUserDefaultSortSetting creates the user default sort setting.
func (r *SettingRegistry) createUserDefaultSortSetting() Setting {
	return Setting{
		Key:          constants.UserDefaultSortOption,
		Name:         "User Default Sort",
		Description:  "Set what users are shown first in the review menu",
		Type:         SettingTypeEnum,
		DefaultValue: SortByRandom,
		Options: []SettingOption{
			{Value: SortByRandom, Label: "Random", Description: "Selected by random", Emoji: "🔀"},
			{Value: SortByConfidence, Label: "Confidence", Description: "Selected by confidence", Emoji: "🔮"},
			{Value: SortByLastUpdated, Label: "Last Updated", Description: "Selected by last updated time", Emoji: "📅"},
			{Value: SortByReputation, Label: "Bad Reputation", Description: "Selected by bad reputation", Emoji: "👎"},
		},
		Validators: []SettingValidator{
			validateEnum([]string{SortByRandom, SortByConfidence, SortByLastUpdated, SortByReputation}),
		},
		ValueGetter: func(us *UserSetting, _ *BotSetting) string {
			return us.UserDefaultSort
		},
		ValueUpdater: func(value string, us *UserSetting, _ *BotSetting) error {
			us.UserDefaultSort = value
			return nil
		},
	}
}

// createGroupDefaultSortSetting creates the group default sort setting.
func (r *SettingRegistry) createGroupDefaultSortSetting() Setting {
	return Setting{
		Key:          constants.GroupDefaultSortOption,
		Name:         "Group Default Sort",
		Description:  "Set what groups are shown first in the review menu",
		Type:         SettingTypeEnum,
		DefaultValue: SortByRandom,
		Options: []SettingOption{
			{Value: SortByRandom, Label: "Random", Description: "Selected by random", Emoji: "🔀"},
			{Value: SortByConfidence, Label: "Confidence", Description: "Selected by confidence", Emoji: "🔍"},
			{Value: SortByFlaggedUsers, Label: "Flagged Users", Description: "Selected by flagged users", Emoji: "👥"},
			{Value: SortByReputation, Label: "Bad Reputation", Description: "Selected by bad reputation", Emoji: "👎"},
		},
		Validators: []SettingValidator{
			validateEnum([]string{SortByRandom, SortByConfidence, SortByFlaggedUsers, SortByReputation}),
		},
		ValueGetter: func(us *UserSetting, _ *BotSetting) string {
			return us.GroupDefaultSort
		},
		ValueUpdater: func(value string, us *UserSetting, _ *BotSetting) error {
			us.GroupDefaultSort = value
			return nil
		},
	}
}

// createReviewModeSetting creates the review mode setting.
func (r *SettingRegistry) createReviewModeSetting() Setting {
	return Setting{
		Key:          constants.ReviewModeOption,
		Name:         "Review Mode",
		Description:  "Switch between training and standard review modes",
		Type:         SettingTypeEnum,
		DefaultValue: StandardReviewMode,
		Options: []SettingOption{
			{
				Value:       TrainingReviewMode,
				Label:       "Training Mode",
				Description: "Practice reviewing without affecting the system",
				Emoji:       "🎓",
			},
			{
				Value:       StandardReviewMode,
				Label:       "Standard Mode",
				Description: "Normal review mode for actual moderation",
				Emoji:       "⚠️",
			},
		},
		Validators: []SettingValidator{
			validateEnum([]string{TrainingReviewMode, StandardReviewMode}),
		},
		ValueGetter: func(us *UserSetting, _ *BotSetting) string {
			return FormatReviewMode(us.ReviewMode)
		},
		ValueUpdater: func(value string, us *UserSetting, _ *BotSetting) error {
			us.ReviewMode = value
			return nil
		},
	}
}

// createSessionLimitSetting creates the session limit setting.
func (r *SettingRegistry) createSessionLimitSetting() Setting {
	return Setting{
		Key:          constants.SessionLimitOption,
		Name:         "Session Limit",
		Description:  "Set the maximum number of concurrent sessions",
		Type:         SettingTypeNumber,
		DefaultValue: uint64(0),
		Validators:   []SettingValidator{validateNumber},
		ValueGetter: func(_ *UserSetting, bs *BotSetting) string {
			return strconv.FormatUint(bs.SessionLimit, 10)
		},
		ValueUpdater: func(value string, _ *UserSetting, bs *BotSetting) error {
			limit, err := strconv.ParseUint(value, 10, 64)
			if err != nil {
				return err
			}
			bs.SessionLimit = limit
			return nil
		},
	}
}

// createReviewerIDsSetting creates the reviewer IDs setting.
func (r *SettingRegistry) createReviewerIDsSetting() Setting {
	return Setting{
		Key:          constants.ReviewerIDsOption,
		Name:         "Reviewer IDs",
		Description:  "Set which users can review using the bot",
		Type:         SettingTypeID,
		DefaultValue: []uint64{},
		Validators:   []SettingValidator{validateDiscordID},
		ValueGetter: func(_ *UserSetting, bs *BotSetting) string {
			return utils.FormatIDs(bs.ReviewerIDs)
		},
		ValueUpdater: func(value string, _ *UserSetting, bs *BotSetting) error {
			id, err := strconv.ParseUint(value, 10, 64)
			if err != nil {
				return err
			}
			exists := false
			for i, reviewerID := range bs.ReviewerIDs {
				if reviewerID == id {
					bs.ReviewerIDs = append(bs.ReviewerIDs[:i], bs.ReviewerIDs[i+1:]...)
					exists = true
					break
				}
			}
			if !exists {
				bs.ReviewerIDs = append(bs.ReviewerIDs, id)
			}
			return nil
		},
	}
}

// createAdminIDsSetting creates the admin IDs setting.
func (r *SettingRegistry) createAdminIDsSetting() Setting {
	return Setting{
		Key:          constants.AdminIDsOption,
		Name:         "Admin IDs",
		Description:  "Set which users can access bot settings",
		Type:         SettingTypeID,
		DefaultValue: []uint64{},
		Validators:   []SettingValidator{validateDiscordID},
		ValueGetter: func(_ *UserSetting, bs *BotSetting) string {
			return utils.FormatIDs(bs.AdminIDs)
		},
		ValueUpdater: func(value string, _ *UserSetting, bs *BotSetting) error {
			id, err := strconv.ParseUint(value, 10, 64)
			if err != nil {
				return err
			}
			exists := false
			for i, adminID := range bs.AdminIDs {
				if adminID == id {
					bs.AdminIDs = append(bs.AdminIDs[:i], bs.AdminIDs[i+1:]...)
					exists = true
					break
				}
			}
			if !exists {
				bs.AdminIDs = append(bs.AdminIDs, id)
			}
			return nil
		},
	}
}

// createReviewTargetModeSetting creates the review target mode setting.
func (r *SettingRegistry) createReviewTargetModeSetting() Setting {
	return Setting{
		Key:          constants.ReviewTargetModeOption,
		Name:         "Review Target Mode",
		Description:  "Switch between reviewing flagged items and re-reviewing confirmed items",
		Type:         SettingTypeEnum,
		DefaultValue: FlaggedReviewTarget,
		Options: []SettingOption{
			{
				Value:       FlaggedReviewTarget,
				Label:       "Flagged Items",
				Description: "Review newly flagged items",
				Emoji:       "🔍",
			},
			{
				Value:       ConfirmedReviewTarget,
				Label:       "Confirmed Items",
				Description: "Re-review previously confirmed items",
				Emoji:       "🔄",
			},
		},
		Validators: []SettingValidator{
			validateEnum([]string{FlaggedReviewTarget, ConfirmedReviewTarget}),
		},
		ValueGetter: func(us *UserSetting, _ *BotSetting) string {
			return FormatReviewTargetMode(us.ReviewTargetMode)
		},
		ValueUpdater: func(value string, us *UserSetting, _ *BotSetting) error {
			us.ReviewTargetMode = value
			return nil
		},
	}
}
