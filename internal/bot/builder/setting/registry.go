package setting

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/rotector/rotector/internal/bot/constants"
	"github.com/rotector/rotector/internal/common/storage/database/types"
	"github.com/rotector/rotector/internal/common/utils"
)

// Validation errors.
var (
	ErrInvalidIDFormat       = errors.New("invalid Discord ID format")
	ErrSelfAssignment        = errors.New("you cannot add/remove yourself")
	ErrInvalidOption         = errors.New("invalid option selected")
	ErrInvalidBoolValue      = errors.New("value must be true or false")
	ErrWelcomeMessageTooLong = errors.New("welcome message cannot exceed 512 characters")
)

// Validator is a function that validates setting input.
// It takes the value to validate and the ID of the user making the change.
type Validator func(value string, userID uint64) error

// ValueGetter is a function that retrieves the value of a setting.
type ValueGetter func(userSettings *types.UserSetting, botSettings *types.BotSetting) string

// ValueUpdater is a function that updates the value of a setting.
type ValueUpdater func(value string, userSettings *types.UserSetting, botSettings *types.BotSetting) error

// Setting defines the structure and behavior of a single setting.
type Setting struct {
	Key          string                `json:"key"`          // Unique identifier for the setting
	Name         string                `json:"name"`         // Display name
	Description  string                `json:"description"`  // Help text explaining the setting
	Type         types.SettingType     `json:"type"`         // Data type of the setting
	DefaultValue interface{}           `json:"defaultValue"` // Default value
	Options      []types.SettingOption `json:"options"`      // Available options for enum types
	Validators   []Validator           `json:"-"`            // Functions to validate input
	ValueGetter  ValueGetter           `json:"-"`            // Function to retrieve the value
	ValueUpdater ValueUpdater          `json:"-"`            // Function to update the value
}

// Registry manages the available settings.
type Registry struct {
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
func validateEnum(validOptions []string) Validator {
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

// NewRegistry creates and initializes the setting registry.
func NewRegistry() *Registry {
	r := &Registry{
		UserSettings: make(map[string]Setting),
		BotSettings:  make(map[string]Setting),
	}

	r.registerUserSettings()
	r.registerBotSettings()

	return r
}

// registerUserSettings adds all user-specific settings to the registry.
func (r *Registry) registerUserSettings() {
	r.UserSettings[constants.StreamerModeOption] = r.createStreamerModeSetting()
	r.UserSettings[constants.UserDefaultSortOption] = r.createUserDefaultSortSetting()
	r.UserSettings[constants.GroupDefaultSortOption] = r.createGroupDefaultSortSetting()
	r.UserSettings[constants.ReviewModeOption] = r.createReviewModeSetting()
	r.UserSettings[constants.ReviewTargetModeOption] = r.createReviewTargetModeSetting()
}

// registerBotSettings adds all bot-wide settings to the registry.
func (r *Registry) registerBotSettings() {
	r.BotSettings[constants.ReviewerIDsOption] = r.createReviewerIDsSetting()
	r.BotSettings[constants.AdminIDsOption] = r.createAdminIDsSetting()
	r.BotSettings[constants.SessionLimitOption] = r.createSessionLimitSetting()
	r.BotSettings[constants.WelcomeMessageOption] = r.createWelcomeMessageSetting()
}

// createStreamerModeSetting creates the streamer mode setting.
func (r *Registry) createStreamerModeSetting() Setting {
	return Setting{
		Key:          constants.StreamerModeOption,
		Name:         "Streamer Mode",
		Description:  "Toggle censoring of sensitive information",
		Type:         types.SettingTypeBool,
		DefaultValue: false,
		Validators:   []Validator{validateBool},
		ValueGetter: func(us *types.UserSetting, _ *types.BotSetting) string {
			return strconv.FormatBool(us.StreamerMode)
		},
		ValueUpdater: func(value string, us *types.UserSetting, _ *types.BotSetting) error {
			boolVal, _ := strconv.ParseBool(value)
			us.StreamerMode = boolVal
			return nil
		},
	}
}

// createUserDefaultSortSetting creates the user default sort setting.
func (r *Registry) createUserDefaultSortSetting() Setting {
	return Setting{
		Key:          constants.UserDefaultSortOption,
		Name:         "User Default Sort",
		Description:  "Set what users are shown first in the review menu",
		Type:         types.SettingTypeEnum,
		DefaultValue: types.SortByRandom,
		Options: []types.SettingOption{
			{Value: string(types.SortByRandom), Label: "Random", Description: "Selected by random", Emoji: "🔀"},
			{Value: string(types.SortByConfidence), Label: "Confidence", Description: "Selected by confidence", Emoji: "🔮"},
			{Value: string(types.SortByLastUpdated), Label: "Last Updated", Description: "Selected by last updated time", Emoji: "📅"},
			{Value: string(types.SortByReputation), Label: "Bad Reputation", Description: "Selected by bad reputation", Emoji: "👎"},
		},
		Validators: []Validator{
			validateEnum([]string{
				string(types.SortByRandom),
				string(types.SortByConfidence),
				string(types.SortByLastUpdated),
				string(types.SortByReputation),
			}),
		},
		ValueGetter: func(us *types.UserSetting, _ *types.BotSetting) string {
			return string(us.UserDefaultSort)
		},
		ValueUpdater: func(value string, us *types.UserSetting, _ *types.BotSetting) error {
			us.UserDefaultSort = types.SortBy(value)
			return nil
		},
	}
}

// createGroupDefaultSortSetting creates the group default sort setting.
func (r *Registry) createGroupDefaultSortSetting() Setting {
	return Setting{
		Key:          constants.GroupDefaultSortOption,
		Name:         "Group Default Sort",
		Description:  "Set what groups are shown first in the review menu",
		Type:         types.SettingTypeEnum,
		DefaultValue: types.SortByRandom,
		Options: []types.SettingOption{
			{Value: string(types.SortByRandom), Label: "Random", Description: "Selected by random", Emoji: "🔀"},
			{Value: string(types.SortByConfidence), Label: "Confidence", Description: "Selected by confidence", Emoji: "🔍"},
			{Value: string(types.SortByReputation), Label: "Bad Reputation", Description: "Selected by bad reputation", Emoji: "👎"},
		},
		Validators: []Validator{
			validateEnum([]string{
				string(types.SortByRandom),
				string(types.SortByConfidence),
				string(types.SortByReputation),
			}),
		},
		ValueGetter: func(us *types.UserSetting, _ *types.BotSetting) string {
			return us.GroupDefaultSort.FormatDisplay()
		},
		ValueUpdater: func(value string, us *types.UserSetting, _ *types.BotSetting) error {
			us.GroupDefaultSort = types.SortBy(value)
			return nil
		},
	}
}

// createReviewModeSetting creates the review mode setting.
func (r *Registry) createReviewModeSetting() Setting {
	return Setting{
		Key:          constants.ReviewModeOption,
		Name:         "Review Mode",
		Description:  "Switch between training and standard review modes",
		Type:         types.SettingTypeEnum,
		DefaultValue: types.StandardReviewMode,
		Options: []types.SettingOption{
			{
				Value:       string(types.TrainingReviewMode),
				Label:       "Training Mode",
				Description: "Practice reviewing without affecting the system",
				Emoji:       "🎓",
			},
			{
				Value:       string(types.StandardReviewMode),
				Label:       "Standard Mode",
				Description: "Normal review mode for actual moderation",
				Emoji:       "⚠️",
			},
		},
		Validators: []Validator{
			validateEnum([]string{
				string(types.TrainingReviewMode),
				string(types.StandardReviewMode),
			}),
		},
		ValueGetter: func(us *types.UserSetting, _ *types.BotSetting) string {
			return us.ReviewMode.FormatDisplay()
		},
		ValueUpdater: func(value string, us *types.UserSetting, _ *types.BotSetting) error {
			us.ReviewMode = types.ReviewMode(value)
			return nil
		},
	}
}

// createSessionLimitSetting creates the session limit setting.
func (r *Registry) createSessionLimitSetting() Setting {
	return Setting{
		Key:          constants.SessionLimitOption,
		Name:         "Session Limit",
		Description:  "Set the maximum number of concurrent sessions",
		Type:         types.SettingTypeNumber,
		DefaultValue: uint64(0),
		Validators:   []Validator{validateNumber},
		ValueGetter: func(_ *types.UserSetting, bs *types.BotSetting) string {
			return strconv.FormatUint(bs.SessionLimit, 10)
		},
		ValueUpdater: func(value string, _ *types.UserSetting, bs *types.BotSetting) error {
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
func (r *Registry) createReviewerIDsSetting() Setting {
	return Setting{
		Key:          constants.ReviewerIDsOption,
		Name:         "Reviewer IDs",
		Description:  "Set which users can review using the bot",
		Type:         types.SettingTypeID,
		DefaultValue: []uint64{},
		Validators:   []Validator{validateDiscordID},
		ValueGetter: func(_ *types.UserSetting, bs *types.BotSetting) string {
			return utils.FormatIDs(bs.ReviewerIDs)
		},
		ValueUpdater: func(value string, _ *types.UserSetting, bs *types.BotSetting) error {
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
func (r *Registry) createAdminIDsSetting() Setting {
	return Setting{
		Key:          constants.AdminIDsOption,
		Name:         "Admin IDs",
		Description:  "Set which users can access bot settings",
		Type:         types.SettingTypeID,
		DefaultValue: []uint64{},
		Validators:   []Validator{validateDiscordID},
		ValueGetter: func(_ *types.UserSetting, bs *types.BotSetting) string {
			return utils.FormatIDs(bs.AdminIDs)
		},
		ValueUpdater: func(value string, _ *types.UserSetting, bs *types.BotSetting) error {
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
func (r *Registry) createReviewTargetModeSetting() Setting {
	return Setting{
		Key:          constants.ReviewTargetModeOption,
		Name:         "Review Target Mode",
		Description:  "Switch between reviewing flagged items and re-reviewing confirmed items",
		Type:         types.SettingTypeEnum,
		DefaultValue: types.FlaggedReviewTarget,
		Options: []types.SettingOption{
			{
				Value:       string(types.FlaggedReviewTarget),
				Label:       "Flagged Items",
				Description: "Review newly flagged items",
				Emoji:       "🔍",
			},
			{
				Value:       string(types.ConfirmedReviewTarget),
				Label:       "Confirmed Items",
				Description: "Re-review previously confirmed items",
				Emoji:       "🔄",
			},
		},
		Validators: []Validator{
			validateEnum([]string{
				string(types.FlaggedReviewTarget),
				string(types.ConfirmedReviewTarget),
			}),
		},
		ValueGetter: func(us *types.UserSetting, _ *types.BotSetting) string {
			return us.ReviewTargetMode.FormatDisplay()
		},
		ValueUpdater: func(value string, us *types.UserSetting, _ *types.BotSetting) error {
			us.ReviewTargetMode = types.ReviewTargetMode(value)
			return nil
		},
	}
}

// createWelcomeMessageSetting creates the welcome message setting.
func (r *Registry) createWelcomeMessageSetting() Setting {
	return Setting{
		Key:          constants.WelcomeMessageOption,
		Name:         "Welcome Message",
		Description:  "Set the welcome message shown on the dashboard",
		Type:         types.SettingTypeText,
		DefaultValue: "",
		Validators: []Validator{
			func(value string, _ uint64) error {
				if len(value) > 512 {
					return ErrWelcomeMessageTooLong
				}
				return nil
			},
		},
		ValueGetter: func(_ *types.UserSetting, bs *types.BotSetting) string {
			if bs.WelcomeMessage == "" {
				return "No welcome message set"
			}
			return bs.WelcomeMessage
		},
		ValueUpdater: func(value string, _ *types.UserSetting, bs *types.BotSetting) error {
			bs.WelcomeMessage = value
			return nil
		},
	}
}
