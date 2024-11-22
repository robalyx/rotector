package setting

import (
	"context"
	"strconv"
	"strings"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/rotector/rotector/internal/bot/builder/setting"
	"github.com/rotector/rotector/internal/bot/constants"
	"github.com/rotector/rotector/internal/bot/core/pagination"
	"github.com/rotector/rotector/internal/bot/core/session"
	"github.com/rotector/rotector/internal/common/storage/database/models"
	"go.uber.org/zap"
)

// Menu handles the interface for changing individual settings.
type Menu struct {
	handler *Handler
	page    *pagination.Page
}

// NewMenu creates a Menu and sets up its page with message builders and
// interaction handlers for changing settings.
func NewMenu(h *Handler) *Menu {
	m := &Menu{handler: h}
	m.page = &pagination.Page{
		Name: "Setting Change Menu",
		Message: func(s *session.Session) *discord.MessageUpdateBuilder {
			var options []discord.StringSelectMenuOption
			s.GetInterface(constants.SessionKeyOptions, &options)

			return setting.NewUpdateBuilder(s).
				AddOptions(options...).
				Build()
		},
		SelectHandlerFunc: m.handleSettingChange,
		ButtonHandlerFunc: m.handleSettingButton,
		ModalHandlerFunc:  m.handleSettingModal,
	}
	return m
}

// ShowMenu prepares and displays the settings change interface by loading
// the current value and available options for the selected setting.
func (m *Menu) ShowMenu(event *events.ComponentInteractionCreate, s *session.Session, settingName, settingType, customID string) {
	// Store setting information in session for the message builder
	s.Set(constants.SessionKeySettingName, settingName)
	s.Set(constants.SessionKeySettingType, settingType)
	s.Set(constants.SessionKeyCustomID, customID)

	// Load current value and options based on setting type
	var currentValue string
	var options []discord.StringSelectMenuOption

	switch customID {
	case constants.StreamerModeOption:
		// Load user settings for streamer mode toggle
		var settings *models.UserSetting
		s.GetInterface(constants.SessionKeyUserSettings, &settings)

		currentValue = strconv.FormatBool(settings.StreamerMode)
		options = []discord.StringSelectMenuOption{
			discord.NewStringSelectMenuOption("Enable", "true"),
			discord.NewStringSelectMenuOption("Disable", "false"),
		}

	case constants.UserDefaultSortOption:
		// Load user settings for user sort preference
		var settings *models.UserSetting
		s.GetInterface(constants.SessionKeyUserSettings, &settings)

		currentValue = settings.UserDefaultSort
		options = []discord.StringSelectMenuOption{
			discord.NewStringSelectMenuOption("Random", models.SortByRandom),
			discord.NewStringSelectMenuOption("Confidence", models.SortByConfidence),
			discord.NewStringSelectMenuOption("Last Updated", models.SortByLastUpdated),
			discord.NewStringSelectMenuOption("Bad Reputation", models.SortByReputation),
		}

	case constants.GroupDefaultSortOption:
		// Load user settings for group sort preference
		var settings *models.UserSetting
		s.GetInterface(constants.SessionKeyUserSettings, &settings)

		currentValue = settings.GroupDefaultSort
		options = []discord.StringSelectMenuOption{
			discord.NewStringSelectMenuOption("Random", models.SortByRandom),
			discord.NewStringSelectMenuOption("Confidence", models.SortByConfidence),
			discord.NewStringSelectMenuOption("Flagged Users", models.SortByFlaggedUsers),
			discord.NewStringSelectMenuOption("Bad Reputation", models.SortByReputation),
		}

	case constants.ReviewModeOption:
		// Load user settings for review mode selection
		var settings *models.UserSetting
		s.GetInterface(constants.SessionKeyUserSettings, &settings)

		currentValue = models.FormatReviewMode(settings.ReviewMode)
		options = []discord.StringSelectMenuOption{
			discord.NewStringSelectMenuOption(
				models.FormatReviewMode(models.TrainingReviewMode),
				models.TrainingReviewMode,
			).WithDescription("Practice reviewing without affecting the system"),
			discord.NewStringSelectMenuOption(
				models.FormatReviewMode(models.StandardReviewMode),
				models.StandardReviewMode,
			).WithDescription("Normal review mode for actual moderation"),
		}

	case constants.ReviewerIDsOption, constants.AdminIDsOption:
		// Create modal for ID input
		modal := discord.NewModalCreateBuilder().
			SetCustomID(customID).
			SetTitle("Toggle " + settingName).
			AddActionRow(
				discord.NewTextInput(
					"id_input",
					discord.TextInputStyleShort,
					"User ID",
				).WithRequired(true).
					WithPlaceholder("Enter the user ID to toggle..."),
			).
			Build()

		// Show modal to user
		if err := event.Modal(modal); err != nil {
			m.handler.logger.Error("Failed to create modal", zap.Error(err))
			m.handler.paginationManager.RespondWithError(event, "Failed to open the ID input form. Please try again.")
			return
		}

		m.handler.paginationManager.UpdatePage(s, m.page)
		return
	}

	// Store values in session for the message builder
	s.Set(constants.SessionKeyCurrentValue, currentValue)
	s.Set(constants.SessionKeyOptions, options)

	m.handler.paginationManager.NavigateTo(event, s, m.page, "")
}

// handleSettingChange processes select menu interactions by saving the new value
// and refreshing the settings display.
func (m *Menu) handleSettingChange(event *events.ComponentInteractionCreate, s *session.Session, customID string, option string) {
	settingName := s.GetString(constants.SessionKeySettingName)
	settingType := s.GetString(constants.SessionKeySettingType)

	m.saveSetting(s, settingType, customID, option)
	m.ShowMenu(event, s, settingName, settingType, customID)
}

// handleSettingButton processes button interactions.
func (m *Menu) handleSettingButton(event *events.ComponentInteractionCreate, s *session.Session, customID string) {
	split := strings.Split(customID, "_")
	if len(split) > 1 && split[1] == constants.BackButtonCustomID {
		// Return to the appropriate settings menu based on setting type
		settingType := split[0]
		if strings.HasPrefix(settingType, constants.UserSettingPrefix) {
			m.handler.userMenu.ShowMenu(event, s)
		} else if strings.HasPrefix(settingType, constants.BotSettingPrefix) {
			m.handler.botMenu.ShowMenu(event, s)
		}
	}
}

// handleSettingModal processes modal interactions.
func (m *Menu) handleSettingModal(event *events.ModalSubmitInteractionCreate, s *session.Session) {
	customID := event.Data.CustomID
	switch customID {
	case constants.ReviewerIDsOption, constants.AdminIDsOption:
		// Get ID input from modal
		idStr := event.Data.Text("id_input")
		if _, err := strconv.ParseUint(idStr, 10, 64); err != nil {
			m.handler.logger.Error("Failed to parse ID input", zap.Error(err))
			m.handler.botMenu.ShowMenu(event, s)
			return
		}

		// Save the setting
		settingType := s.GetString(constants.SessionKeySettingType)
		m.saveSetting(s, settingType, customID, idStr)

		// Refresh the bot settings menu
		m.handler.botMenu.ShowMenu(event, s)
	}
}

// saveSetting routes the setting change to the appropriate save function.
func (m *Menu) saveSetting(s *session.Session, settingType, customID, option string) {
	switch settingType {
	case constants.UserSettingPrefix:
		m.saveUserSetting(s, customID, option)
	case constants.BotSettingPrefix:
		m.saveBotSetting(s, customID, option)
	default:
		m.handler.logger.Warn("unknown setting type", zap.String("settingType", settingType))
	}
}

// saveUserSetting updates user-specific settings in both the database and session.
func (m *Menu) saveUserSetting(s *session.Session, customID, option string) {
	var settings *models.UserSetting
	s.GetInterface(constants.SessionKeyUserSettings, &settings)

	switch customID {
	case constants.StreamerModeOption:
		// Parse and save streamer mode toggle
		var err error
		if settings.StreamerMode, err = strconv.ParseBool(option); err != nil {
			m.handler.logger.Error("Failed to parse streamer mode", zap.Error(err))
			return
		}

	case constants.UserDefaultSortOption:
		settings.UserDefaultSort = option

	case constants.GroupDefaultSortOption:
		settings.GroupDefaultSort = option

	case constants.ReviewModeOption:
		settings.ReviewMode = option

	default:
		m.handler.logger.Warn("Unknown user setting", zap.String("customID", customID))
		return
	}

	// Save to database and update session
	if err := m.handler.db.Settings().SaveUserSettings(context.Background(), settings); err != nil {
		m.handler.logger.Error("Failed to save user settings", zap.Error(err))
		return
	}

	s.Set(constants.SessionKeyUserSettings, settings)
}

// saveBotSetting updates bot-wide settings in the models.
func (m *Menu) saveBotSetting(s *session.Session, customID, option string) {
	// Parse the ID
	id, err := strconv.ParseUint(option, 10, 64)
	if err != nil {
		m.handler.logger.Error("Failed to parse ID input", zap.Error(err))
		return
	}

	// Toggle the ID based on setting type
	if customID == constants.ReviewerIDsOption {
		if err := m.handler.db.Settings().ToggleReviewerID(context.Background(), id); err != nil {
			m.handler.logger.Error("Failed to toggle reviewer ID", zap.Error(err))
			return
		}
	} else if customID == constants.AdminIDsOption {
		if err := m.handler.db.Settings().ToggleAdminID(context.Background(), id); err != nil {
			m.handler.logger.Error("Failed to toggle admin ID", zap.Error(err))
			return
		}
	}

	// Update bot settings in session
	settings, err := m.handler.db.Settings().GetBotSettings(context.Background())
	if err != nil {
		m.handler.logger.Error("Failed to fetch bot settings", zap.Error(err))
		return
	}
	s.Set(constants.SessionKeyBotSettings, settings)

	// Close the target user's session to reflect the change
	m.handler.sessionManager.CloseSession(context.Background(), id)
}
