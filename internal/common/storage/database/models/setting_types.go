package models

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/rotector/rotector/internal/bot/constants"
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
	SettingTypeBool SettingType = "bool"
	SettingTypeEnum SettingType = "enum"
	SettingTypeID   SettingType = "id"
)

// SettingValidator is a function that validates setting input.
// It takes the value to validate and the ID of the user making the change.
type SettingValidator func(value string, userID uint64) error

// Setting defines the structure and behavior of a single setting.
type Setting struct {
	Key          string             `json:"key"`          // Unique identifier for the setting
	Name         string             `json:"name"`         // Display name
	Description  string             `json:"description"`  // Help text explaining the setting
	Type         SettingType        `json:"type"`         // Data type of the setting
	DefaultValue interface{}        `json:"defaultValue"` // Default value
	Options      []SettingOption    `json:"options"`      // Available options for enum types
	Validators   []SettingValidator `json:"-"`            // Functions to validate input
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

// NewSettingRegistry creates and initializes the setting registry.
func NewSettingRegistry() *SettingRegistry {
	r := &SettingRegistry{
		UserSettings: make(map[string]Setting),
		BotSettings:  make(map[string]Setting),
	}

	// Register user settings
	r.UserSettings[constants.StreamerModeOption] = Setting{
		Key:          constants.StreamerModeOption,
		Name:         "Streamer Mode",
		Description:  "Toggle censoring of sensitive information",
		Type:         SettingTypeBool,
		DefaultValue: false,
		Validators:   []SettingValidator{validateBool},
	}

	r.UserSettings[constants.UserDefaultSortOption] = Setting{
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
	}

	r.UserSettings[constants.GroupDefaultSortOption] = Setting{
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
	}

	r.UserSettings[constants.ReviewModeOption] = Setting{
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
	}

	// Register bot settings
	r.BotSettings[constants.ReviewerIDsOption] = Setting{
		Key:          constants.ReviewerIDsOption,
		Name:         "Reviewer IDs",
		Description:  "Set which users can review using the bot",
		Type:         SettingTypeID,
		DefaultValue: []uint64{},
		Validators:   []SettingValidator{validateDiscordID},
	}

	r.BotSettings[constants.AdminIDsOption] = Setting{
		Key:          constants.AdminIDsOption,
		Name:         "Admin IDs",
		Description:  "Set which users can access bot settings",
		Type:         SettingTypeID,
		DefaultValue: []uint64{},
		Validators:   []SettingValidator{validateDiscordID},
	}

	return r
}
