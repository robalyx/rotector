package session

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/database"
	"github.com/robalyx/rotector/internal/database/types"
	"github.com/robalyx/rotector/internal/database/types/enum"
)

// Validation errors.
var (
	ErrInvalidNumber         = errors.New("value must be a valid number")
	ErrInvalidIDFormat       = errors.New("invalid Discord ID format")
	ErrSelfAssignment        = errors.New("you cannot add/remove yourself")
	ErrInvalidOption         = errors.New("invalid option selected")
	ErrInvalidBoolValue      = errors.New("value must be true or false")
	ErrWelcomeMessageTooLong = errors.New("welcome message cannot exceed 512 characters")
	ErrAnnouncementTooLong   = errors.New("announcement message cannot exceed 512 characters")
	ErrNotReviewer           = errors.New("you are not an official reviewer")
	ErrMissingInput          = errors.New("missing input")
)

// Validator is a function that validates setting input.
// It takes the value to validate and the ID of the user making the change.
type Validator func(value string, userID uint64) error

// ValueGetter is a function that retrieves the value of a setting.
type ValueGetter func(s *Session) string

// ValueUpdater is a function type where customID is provided to identify which action is being performed.
type ValueUpdater func(customID string, inputs []string, s *Session) error

// Setting defines the structure and behavior of a single setting.
type Setting struct {
	Key          string                `json:"key"`          // Unique identifier for the setting
	Name         string                `json:"name"`         // Display name
	Description  string                `json:"description"`  // Help text explaining the setting
	Type         enum.SettingType      `json:"type"`         // Data type of the setting
	DefaultValue any                   `json:"defaultValue"` // Default value
	Options      []types.SettingOption `json:"options"`      // Available options for enum types
	Validators   []Validator           `json:"-"`            // Functions to validate input
	ValueGetter  ValueGetter           `json:"-"`            // Function to retrieve the value
	ValueUpdater ValueUpdater          `json:"-"`            // Function to update the value
}

// SettingRegistry manages the available settings.
type SettingRegistry struct {
	UserSettings map[string]*Setting // User-specific settings
	BotSettings  map[string]*Setting // Bot-wide settings
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
	// Create a map for O(1) lookups
	optionMap := make(map[string]struct{}, len(validOptions))
	for _, opt := range validOptions {
		optionMap[opt] = struct{}{}
	}

	return func(value string, _ uint64) error {
		if _, valid := optionMap[value]; !valid {
			return fmt.Errorf("%w: must be one of %v", ErrInvalidOption, validOptions)
		}

		return nil
	}
}

// validateBool checks if a string is a valid boolean value ("true" or "false").
func validateBool(value string, _ uint64) error {
	if value != "true" && value != "false" {
		return ErrInvalidBoolValue
	}

	return nil
}

// validateNumber checks if a string is a valid non-negative number.
func validateNumber(value string, _ uint64) error {
	num, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidNumber, err)
	}

	if num < 0 {
		return fmt.Errorf("%w: %d", ErrInvalidNumber, num)
	}

	return nil
}

// NewSettingRegistry creates and initializes the setting registry.
func NewSettingRegistry() *SettingRegistry {
	r := &SettingRegistry{
		UserSettings: make(map[string]*Setting),
		BotSettings:  make(map[string]*Setting),
	}

	r.registerUserSettings()
	r.registerBotSettings()

	return r
}

// registerUserSettings adds all user-specific settings to the registry.
func (r *SettingRegistry) registerUserSettings() {
	r.UserSettings[constants.StreamerModeOption] = r.createStreamerModeSetting()
	r.UserSettings[constants.ReviewModeOption] = r.createReviewModeSetting()
	r.UserSettings[constants.ReviewTargetModeOption] = r.createReviewTargetModeSetting()
}

// registerBotSettings adds all bot-wide settings to the registry.
func (r *SettingRegistry) registerBotSettings() {
	r.BotSettings[constants.ReviewerIDsOption] = r.createReviewerIDsSetting()
	r.BotSettings[constants.AdminIDsOption] = r.createAdminIDsSetting()
	r.BotSettings[constants.SessionLimitOption] = r.createSessionLimitSetting()
	r.BotSettings[constants.WelcomeMessageOption] = r.createWelcomeMessageSetting()
	r.BotSettings[constants.AnnouncementTypeOption] = r.createAnnouncementTypeSetting()
	r.BotSettings[constants.AnnouncementMessageOption] = r.createAnnouncementMessageSetting()
}

// createStreamerModeSetting creates the streamer mode setting.
func (r *SettingRegistry) createStreamerModeSetting() *Setting {
	return &Setting{
		Key:          constants.StreamerModeOption,
		Name:         "Streamer Mode",
		Description:  "Toggle censoring of sensitive information",
		Type:         enum.SettingTypeBool,
		DefaultValue: false,
		Validators:   []Validator{validateBool},
		ValueGetter: func(s *Session) string {
			return strconv.FormatBool(UserStreamerMode.Get(s))
		},
		ValueUpdater: func(_ string, inputs []string, s *Session) error {
			if len(inputs) < 1 {
				return ErrMissingInput
			}
			boolVal, _ := strconv.ParseBool(inputs[0])
			UserStreamerMode.Set(s, boolVal)
			return nil
		},
	}
}

// createReviewModeSetting creates the review mode setting.
func (r *SettingRegistry) createReviewModeSetting() *Setting {
	return &Setting{
		Key:          constants.ReviewModeOption,
		Name:         "Review Mode",
		Description:  "Switch between training and standard review modes",
		Type:         enum.SettingTypeEnum,
		DefaultValue: enum.ReviewModeStandard,
		Options: []types.SettingOption{
			{
				Value:       enum.ReviewModeTraining.String(),
				Label:       "Training Mode",
				Description: "Practice reviewing without affecting the system",
				Emoji:       "🎓",
			},
			{
				Value:       enum.ReviewModeStandard.String(),
				Label:       "Standard Mode",
				Description: "Normal review mode for actual moderation",
				Emoji:       "⚠️",
			},
		},
		Validators: []Validator{
			validateEnum([]string{
				enum.ReviewModeTraining.String(),
				enum.ReviewModeStandard.String(),
			}),
		},
		ValueGetter: func(s *Session) string {
			return UserReviewMode.Get(s).String()
		},
		ValueUpdater: func(_ string, inputs []string, s *Session) error {
			if len(inputs) < 1 {
				return ErrMissingInput
			}
			reviewMode, err := enum.ReviewModeString(inputs[0])
			if err != nil {
				return err
			}

			// Only allow changing to standard mode if user is a reviewer
			if reviewMode == enum.ReviewModeStandard && !s.BotSettings().IsReviewer(UserID.Get(s)) {
				return ErrNotReviewer
			}

			UserReviewMode.Set(s, reviewMode)
			return nil
		},
	}
}

// createReviewTargetModeSetting creates the review target mode setting.
func (r *SettingRegistry) createReviewTargetModeSetting() *Setting {
	return &Setting{
		Key:          constants.ReviewTargetModeOption,
		Name:         "Review Target Mode",
		Description:  "Switch between reviewing different types of items",
		Type:         enum.SettingTypeEnum,
		DefaultValue: enum.ReviewTargetModeFlagged,
		Options: []types.SettingOption{
			{
				Value:       enum.ReviewTargetModeFlagged.String(),
				Label:       "Flagged Items",
				Description: "Review newly flagged items",
				Emoji:       "⏳",
			},
			{
				Value:       enum.ReviewTargetModeConfirmed.String(),
				Label:       "Confirmed Items",
				Description: "Re-review confirmed items",
				Emoji:       "⚠️",
			},
			{
				Value:       enum.ReviewTargetModeCleared.String(),
				Label:       "Cleared Items",
				Description: "Re-review cleared items",
				Emoji:       "✅",
			},
		},
		Validators: []Validator{
			validateEnum([]string{
				enum.ReviewTargetModeFlagged.String(),
				enum.ReviewTargetModeConfirmed.String(),
				enum.ReviewTargetModeCleared.String(),
			}),
		},
		ValueGetter: func(s *Session) string {
			return UserReviewTargetMode.Get(s).String()
		},
		ValueUpdater: func(_ string, inputs []string, s *Session) error {
			if len(inputs) < 1 {
				return ErrMissingInput
			}
			reviewTargetMode, err := enum.ReviewTargetModeString(inputs[0])
			if err != nil {
				return err
			}
			UserReviewTargetMode.Set(s, reviewTargetMode)
			return nil
		},
	}
}

// createSessionLimitSetting creates the session limit setting.
func (r *SettingRegistry) createSessionLimitSetting() *Setting {
	return &Setting{
		Key:          constants.SessionLimitOption,
		Name:         "Session Limit",
		Description:  "Set the maximum number of concurrent sessions",
		Type:         enum.SettingTypeNumber,
		DefaultValue: uint64(0),
		Validators:   []Validator{validateNumber},
		ValueGetter: func(s *Session) string {
			return strconv.FormatUint(BotSessionLimit.Get(s), 10)
		},
		ValueUpdater: func(_ string, inputs []string, s *Session) error {
			if len(inputs) < 1 {
				return ErrMissingInput
			}

			// Get old value for logging
			oldValue := BotSessionLimit.Get(s)

			// Parse and validate new value
			limit, err := strconv.ParseUint(inputs[0], 10, 64)
			if err != nil {
				return err
			}

			// Update the setting
			BotSessionLimit.Set(s, limit)

			// Log the change
			r.logBotSettingChange(context.Background(), s.db, UserID.Get(s),
				constants.SessionLimitOption,
				strconv.FormatUint(oldValue, 10),
				strconv.FormatUint(limit, 10))

			return nil
		},
	}
}

// createReviewerIDsSetting creates the reviewer IDs setting.
func (r *SettingRegistry) createReviewerIDsSetting() *Setting {
	return &Setting{
		Key:          constants.ReviewerIDsOption,
		Name:         "Reviewer IDs",
		Description:  "Manage authorized reviewer IDs",
		Type:         enum.SettingTypeID,
		DefaultValue: []uint64{},
		Validators:   []Validator{validateDiscordID},
		ValueGetter: func(s *Session) string {
			reviewerIDs := BotReviewerIDs.Get(s)
			return fmt.Sprintf("%d reviewer(s) authorized", len(reviewerIDs))
		},
		ValueUpdater: func(_ string, inputs []string, s *Session) error {
			if len(inputs) < 1 {
				return ErrMissingInput
			}

			// Get old IDs for logging
			oldIDs := BotReviewerIDs.Get(s)

			id, err := strconv.ParseUint(inputs[0], 10, 64)
			if err != nil {
				return fmt.Errorf("%w: %w", ErrInvalidIDFormat, err)
			}

			// Check the reviewer IDs from the session
			reviewerIDs := BotReviewerIDs.Get(s)

			// Toggle the ID
			found := false
			for i, reviewerID := range reviewerIDs {
				if reviewerID == id {
					reviewerIDs = slices.Delete(reviewerIDs, i, i+1)
					found = true
					break
				}
			}

			if !found {
				reviewerIDs = append(reviewerIDs, id)
			}

			// Set the reviewer IDs in the session
			BotReviewerIDs.Set(s, reviewerIDs)

			// Log the change
			r.logBotSettingChange(context.Background(), s.db, UserID.Get(s),
				constants.ReviewerIDsOption,
				fmt.Sprintf("%v", oldIDs),
				fmt.Sprintf("%v", reviewerIDs))

			return nil
		},
	}
}

// createAdminIDsSetting creates the admin IDs setting.
func (r *SettingRegistry) createAdminIDsSetting() *Setting {
	return &Setting{
		Key:          constants.AdminIDsOption,
		Name:         "Admin IDs",
		Description:  "Manage authorized admin IDs",
		Type:         enum.SettingTypeID,
		DefaultValue: []uint64{},
		Validators:   []Validator{validateDiscordID},
		ValueGetter: func(s *Session) string {
			adminIDs := BotAdminIDs.Get(s)
			return fmt.Sprintf("%d admin(s) authorized", len(adminIDs))
		},
		ValueUpdater: func(_ string, inputs []string, s *Session) error {
			if len(inputs) < 1 {
				return ErrMissingInput
			}

			// Get old IDs for logging
			oldIDs := BotAdminIDs.Get(s)

			id, err := strconv.ParseUint(inputs[0], 10, 64)
			if err != nil {
				return fmt.Errorf("%w: %w", ErrInvalidIDFormat, err)
			}

			// Check the admin IDs from the session
			adminIDs := BotAdminIDs.Get(s)

			// Toggle the ID
			found := false
			for i, adminID := range adminIDs {
				if adminID == id {
					adminIDs = slices.Delete(adminIDs, i, i+1)
					found = true
					break
				}
			}

			if !found {
				adminIDs = append(adminIDs, id)
			}

			// Set the admin IDs in the session
			BotAdminIDs.Set(s, adminIDs)

			// Log the change
			r.logBotSettingChange(context.Background(), s.db, UserID.Get(s),
				constants.AdminIDsOption,
				fmt.Sprintf("%v", oldIDs),
				fmt.Sprintf("%v", adminIDs))

			return nil
		},
	}
}

// createWelcomeMessageSetting creates the welcome message setting.
func (r *SettingRegistry) createWelcomeMessageSetting() *Setting {
	return &Setting{
		Key:          constants.WelcomeMessageOption,
		Name:         "Welcome Message",
		Description:  "Set the welcome message shown on the dashboard",
		Type:         enum.SettingTypeText,
		DefaultValue: "",
		Validators: []Validator{
			func(value string, _ uint64) error {
				if len(value) > 512 {
					return ErrWelcomeMessageTooLong
				}
				return nil
			},
		},
		ValueGetter: func(s *Session) string {
			welcomeMessage := BotWelcomeMessage.Get(s)
			if welcomeMessage == "" {
				return "No welcome message set"
			}
			return welcomeMessage
		},
		ValueUpdater: func(_ string, inputs []string, s *Session) error {
			if len(inputs) < 1 {
				return ErrMissingInput
			}

			// Get old value for logging
			oldValue := BotWelcomeMessage.Get(s)

			// Update the setting
			BotWelcomeMessage.Set(s, inputs[0])

			// Log the change
			r.logBotSettingChange(context.Background(), s.db, UserID.Get(s),
				constants.WelcomeMessageOption,
				oldValue,
				inputs[0])

			return nil
		},
	}
}

// createAnnouncementTypeSetting creates the announcement type setting.
func (r *SettingRegistry) createAnnouncementTypeSetting() *Setting {
	return &Setting{
		Key:          constants.AnnouncementTypeOption,
		Name:         "Announcement Type",
		Description:  "Set the type of announcement to display",
		Type:         enum.SettingTypeEnum,
		DefaultValue: enum.AnnouncementTypeNone,
		Options: []types.SettingOption{
			{Value: enum.AnnouncementTypeNone.String(), Label: "None", Description: "No announcement", Emoji: "❌"},
			{Value: enum.AnnouncementTypeInfo.String(), Label: "Info", Description: "Information announcement", Emoji: "ℹ️"},
			{Value: enum.AnnouncementTypeWarning.String(), Label: "Warning", Description: "Warning announcement", Emoji: "⚠️"},
			{Value: enum.AnnouncementTypeSuccess.String(), Label: "Success", Description: "Success announcement", Emoji: "✅"},
			{Value: enum.AnnouncementTypeError.String(), Label: "Error", Description: "Error announcement", Emoji: "🚫"},
			{Value: enum.AnnouncementTypeMaintenance.String(), Label: "Maintenance", Description: "System maintenance", Emoji: "🔧"},
		},
		Validators: []Validator{
			validateEnum([]string{
				enum.AnnouncementTypeNone.String(),
				enum.AnnouncementTypeInfo.String(),
				enum.AnnouncementTypeWarning.String(),
				enum.AnnouncementTypeSuccess.String(),
				enum.AnnouncementTypeError.String(),
				enum.AnnouncementTypeMaintenance.String(),
			}),
		},
		ValueGetter: func(s *Session) string {
			return BotAnnouncementType.Get(s).String()
		},
		ValueUpdater: func(_ string, inputs []string, s *Session) error {
			if len(inputs) < 1 {
				return ErrMissingInput
			}

			// Get old value for logging
			oldValue := BotAnnouncementType.Get(s)

			announcementType, err := enum.AnnouncementTypeString(inputs[0])
			if err != nil {
				return err
			}

			// Update the setting
			BotAnnouncementType.Set(s, announcementType)

			// Log the change
			r.logBotSettingChange(context.Background(), s.db, UserID.Get(s),
				constants.AnnouncementTypeOption,
				oldValue.String(),
				announcementType.String())

			return nil
		},
	}
}

// createAnnouncementMessageSetting creates the announcement message setting.
func (r *SettingRegistry) createAnnouncementMessageSetting() *Setting {
	return &Setting{
		Key:          constants.AnnouncementMessageOption,
		Name:         "Announcement Message",
		Description:  "Set the announcement message to display",
		Type:         enum.SettingTypeText,
		DefaultValue: "",
		Validators: []Validator{
			func(value string, _ uint64) error {
				if len(value) > 512 {
					return ErrAnnouncementTooLong
				}
				return nil
			},
		},
		ValueGetter: func(s *Session) string {
			announcementMessage := BotAnnouncementMessage.Get(s)
			if announcementMessage == "" {
				return "No announcement message set"
			}
			return announcementMessage
		},
		ValueUpdater: func(_ string, inputs []string, s *Session) error {
			if len(inputs) < 1 {
				return ErrMissingInput
			}

			// Get old value for logging
			oldValue := BotAnnouncementMessage.Get(s)

			// Update the setting
			BotAnnouncementMessage.Set(s, inputs[0])

			// Log the change
			r.logBotSettingChange(context.Background(), s.db, UserID.Get(s),
				constants.AnnouncementMessageOption,
				oldValue,
				inputs[0])

			return nil
		},
	}
}

// logBotSettingChange is a helper method to handle logging changes to bot settings.
func (r *SettingRegistry) logBotSettingChange(
	ctx context.Context, db database.Client, reviewerID uint64, settingKey, oldValue, newValue string,
) {
	details := map[string]any{
		"setting": settingKey,
		"old":     oldValue,
		"new":     newValue,
	}

	activityLog := &types.ActivityLog{
		ReviewerID:        reviewerID,
		ActivityType:      enum.ActivityTypeBotSettingUpdated,
		ActivityTimestamp: time.Now(),
		Details:           details,
	}

	db.Model().Activity().Log(ctx, activityLog)
}
