package session

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/utils"
	"github.com/robalyx/rotector/internal/common/storage/database/types"
	"github.com/robalyx/rotector/internal/common/storage/database/types/enum"
)

// Validation errors.
var (
	ErrInvalidIDFormat       = errors.New("invalid Discord ID format")
	ErrSelfAssignment        = errors.New("you cannot add/remove yourself")
	ErrInvalidOption         = errors.New("invalid option selected")
	ErrInvalidBoolValue      = errors.New("value must be true or false")
	ErrWelcomeMessageTooLong = errors.New("welcome message cannot exceed 512 characters")
	ErrAnnouncementTooLong   = errors.New("announcement message cannot exceed 512 characters")
	ErrDescriptionTooLong    = errors.New("description cannot exceed 512 characters")
	ErrNotReviewer           = errors.New("you are not an official reviewer")
)

// Validator is a function that validates setting input.
// It takes the value to validate and the ID of the user making the change.
type Validator func(value string, userID uint64) error

// ValueGetter is a function that retrieves the value of a setting.
type ValueGetter func(s *Session) string

// ValueUpdater is a function that updates the value of a setting.
type ValueUpdater func(value string, s *Session) error

// Setting defines the structure and behavior of a single setting.
type Setting struct {
	Key          string                `json:"key"`          // Unique identifier for the setting
	Name         string                `json:"name"`         // Display name
	Description  string                `json:"description"`  // Help text explaining the setting
	Type         enum.SettingType      `json:"type"`         // Data type of the setting
	DefaultValue interface{}           `json:"defaultValue"` // Default value
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
	r.BotSettings[constants.APIKeysOption] = r.createAPIKeysSetting()
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
		ValueUpdater: func(value string, s *Session) error {
			boolVal, _ := strconv.ParseBool(value)
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
		ValueUpdater: func(value string, s *Session) error {
			reviewMode, err := enum.ReviewModeString(value)
			if err != nil {
				return err
			}

			// Only allow changing to standard mode if user is a reviewer
			if reviewMode == enum.ReviewModeStandard && !s.BotSettings().IsReviewer(s.UserID()) {
				return ErrNotReviewer
			}

			UserReviewMode.Set(s, reviewMode)
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
		ValueUpdater: func(value string, s *Session) error {
			limit, err := strconv.ParseUint(value, 10, 64)
			if err != nil {
				return err
			}
			BotSessionLimit.Set(s, limit)
			return nil
		},
	}
}

// createReviewerIDsSetting creates the reviewer IDs setting.
func (r *SettingRegistry) createReviewerIDsSetting() *Setting {
	return &Setting{
		Key:          constants.ReviewerIDsOption,
		Name:         "Reviewer IDs",
		Description:  "Set which users can review using the bot",
		Type:         enum.SettingTypeID,
		DefaultValue: []uint64{},
		Validators:   []Validator{validateDiscordID},
		ValueGetter: func(s *Session) string {
			// Check the reviewer IDs from the session
			reviewerIDs := BotReviewerIDs.Get(s)
			if len(reviewerIDs) == 0 {
				return "No reviewers set"
			}

			// Show only first 10 IDs
			displayIDs := utils.FormatIDs(reviewerIDs)
			if len(reviewerIDs) > 10 {
				displayIDs += fmt.Sprintf("\n...and %d more", len(reviewerIDs)-10)
			}
			return displayIDs
		},
		ValueUpdater: func(value string, s *Session) error {
			id, err := strconv.ParseUint(value, 10, 64)
			if err != nil {
				return err
			}

			// Check the reviewer IDs from the session
			reviewerIDs := BotReviewerIDs.Get(s)
			exists := false
			for i, reviewerID := range reviewerIDs {
				if reviewerID == id {
					reviewerIDs = append(reviewerIDs[:i], reviewerIDs[i+1:]...)
					exists = true
					break
				}
			}
			if !exists {
				reviewerIDs = append(reviewerIDs, id)
			}

			// Set the reviewer IDs in the session
			BotReviewerIDs.Set(s, reviewerIDs)
			return nil
		},
	}
}

// createAdminIDsSetting creates the admin IDs setting.
func (r *SettingRegistry) createAdminIDsSetting() *Setting {
	return &Setting{
		Key:          constants.AdminIDsOption,
		Name:         "Admin IDs",
		Description:  "Set which users can access bot settings",
		Type:         enum.SettingTypeID,
		DefaultValue: []uint64{},
		Validators:   []Validator{validateDiscordID},
		ValueGetter: func(s *Session) string {
			// Check the admin IDs from the session
			adminIDs := BotAdminIDs.Get(s)
			if len(adminIDs) == 0 {
				return "No admins set"
			}

			// Show only first 10 IDs
			displayIDs := utils.FormatIDs(adminIDs)
			if len(adminIDs) > 10 {
				displayIDs += fmt.Sprintf("\n...and %d more", len(adminIDs)-10)
			}
			return displayIDs
		},
		ValueUpdater: func(value string, s *Session) error {
			id, err := strconv.ParseUint(value, 10, 64)
			if err != nil {
				return err
			}

			// Check the admin IDs from the session
			adminIDs := BotAdminIDs.Get(s)
			exists := false
			for i, adminID := range adminIDs {
				if adminID == id {
					adminIDs = append(adminIDs[:i], adminIDs[i+1:]...)
					exists = true
					break
				}
			}
			if !exists {
				adminIDs = append(adminIDs, id)
			}

			// Set the admin IDs in the session
			BotAdminIDs.Set(s, adminIDs)
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
				Emoji:       "🔍",
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
		ValueUpdater: func(value string, s *Session) error {
			reviewTargetMode, err := enum.ReviewTargetModeString(value)
			if err != nil {
				return err
			}
			UserReviewTargetMode.Set(s, reviewTargetMode)
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
		ValueUpdater: func(value string, s *Session) error {
			BotWelcomeMessage.Set(s, value)
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
		},
		Validators: []Validator{
			validateEnum([]string{
				enum.AnnouncementTypeNone.String(),
				enum.AnnouncementTypeInfo.String(),
				enum.AnnouncementTypeWarning.String(),
				enum.AnnouncementTypeSuccess.String(),
				enum.AnnouncementTypeError.String(),
			}),
		},
		ValueGetter: func(s *Session) string {
			return BotAnnouncementType.Get(s).String()
		},
		ValueUpdater: func(value string, s *Session) error {
			announcementType, err := enum.AnnouncementTypeString(value)
			if err != nil {
				return err
			}
			BotAnnouncementType.Set(s, announcementType)
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
		ValueUpdater: func(value string, s *Session) error {
			BotAnnouncementMessage.Set(s, value)
			return nil
		},
	}
}

// createAPIKeysSetting creates the API keys setting.
func (r *SettingRegistry) createAPIKeysSetting() *Setting {
	return &Setting{
		Key:          constants.APIKeysOption,
		Name:         "API Keys",
		Description:  "Manage API keys for REST API access",
		Type:         enum.SettingTypeText,
		DefaultValue: []types.APIKeyInfo{},
		Validators: []Validator{
			func(value string, _ uint64) error {
				if len(value) > 100 {
					return ErrDescriptionTooLong
				}
				return nil
			},
		},
		ValueGetter: func(s *Session) string {
			apiKeys := BotAPIKeys.Get(s)
			if len(apiKeys) == 0 {
				return "No API keys configured"
			}

			var sb strings.Builder
			for i, key := range apiKeys {
				if i > 0 {
					sb.WriteString("\n")
				}
				// Only show first 8 chars of key for security
				sb.WriteString(fmt.Sprintf("||%s||: %s (Created: %s)",
					key.Key,
					key.Description,
					key.CreatedAt.Format("2006-01-02")))
			}
			return sb.String()
		},
		ValueUpdater: func(value string, s *Session) error {
			apiKeys := BotAPIKeys.Get(s)
			exists := false
			for i, key := range apiKeys {
				if key.Key == value {
					// Remove existing key
					apiKeys = append(apiKeys[:i], apiKeys[i+1:]...)
					exists = true
					break
				}
			}

			if !exists {
				// Add new key
				newKey := types.APIKeyInfo{
					Key:         utils.GenerateSecureToken(32),
					Description: value,
					CreatedAt:   time.Now(),
				}
				apiKeys = append(apiKeys, newKey)
			}

			BotAPIKeys.Set(s, apiKeys)
			return nil
		},
	}
}
