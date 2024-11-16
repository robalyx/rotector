package dashboard

import (
	"context"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/snowflake/v2"
	"github.com/rotector/rotector/internal/bot/constants"
	"github.com/rotector/rotector/internal/bot/handlers/dashboard/builders"
	"github.com/rotector/rotector/internal/bot/interfaces"
	"github.com/rotector/rotector/internal/bot/pagination"
	"github.com/rotector/rotector/internal/bot/session"
	"github.com/rotector/rotector/internal/common/database"
	"github.com/rotector/rotector/internal/common/worker"
	"go.uber.org/zap"
)

// Menu handles the display and interaction logic for the main dashboard.
// It works with the dashboard builder to show statistics and provide
// navigation to different sections of the bot.
type Menu struct {
	handler *Handler
	page    *pagination.Page
}

// NewMenu creates a Menu and sets up its page with message builders and
// interaction handlers. The page is configured to show statistics and
// handle navigation to other sections.
func NewMenu(h *Handler) *Menu {
	m := Menu{handler: h}
	m.page = &pagination.Page{
		Name: "Dashboard",
		Message: func(s *session.Session) *discord.MessageUpdateBuilder {
			var botSettings *database.BotSetting
			s.GetInterface(constants.SessionKeyBotSettings, &botSettings)
			var activeUsers []snowflake.ID
			s.GetInterface(constants.SessionKeyActiveUsers, &activeUsers)
			var workerStatuses []worker.Status
			s.GetInterface(constants.SessionKeyWorkerStatuses, &workerStatuses)

			userID := s.GetUint64(constants.SessionKeyUserID)
			confirmedCount := s.GetInt(constants.SessionKeyConfirmedCount)
			flaggedCount := s.GetInt(constants.SessionKeyFlaggedCount)
			clearedCount := s.GetInt(constants.SessionKeyClearedCount)
			imageBuffer := s.GetBuffer(constants.SessionKeyImageBuffer)

			return builders.NewDashboardBuilder(botSettings, userID, confirmedCount, flaggedCount, clearedCount, imageBuffer, activeUsers, workerStatuses).Build()
		},
		SelectHandlerFunc: m.handleSelectMenu,
		ButtonHandlerFunc: m.handleButton,
	}
	return &m
}

// ShowDashboard prepares and displays the dashboard interface.
func (m *Menu) ShowDashboard(event interfaces.CommonEvent, s *session.Session, content string) {
	// Get worker statuses
	workerStatuses, err := m.handler.workerMonitor.GetAllStatuses(context.Background())
	if err != nil {
		m.handler.logger.Error("Failed to get worker statuses", zap.Error(err))
	}

	// Load current user counts from database
	confirmedCount, err := m.handler.db.Users().GetConfirmedUsersCount(context.Background())
	if err != nil {
		m.handler.logger.Error("Failed to get confirmed users count", zap.Error(err))
	}

	flaggedCount, err := m.handler.db.Users().GetFlaggedUsersCount(context.Background())
	if err != nil {
		m.handler.logger.Error("Failed to get flagged users count", zap.Error(err))
	}

	clearedCount, err := m.handler.db.Users().GetClearedUsersCount(context.Background())
	if err != nil {
		m.handler.logger.Error("Failed to get cleared users count", zap.Error(err))
	}

	// Get hourly stats for the chart
	hourlyStats, err := m.handler.db.Stats().GetHourlyStats(context.Background())
	if err != nil {
		m.handler.logger.Error("Failed to get hourly stats", zap.Error(err))
	}

	m.handler.logger.Info("Hourly stats", zap.Any("stats", hourlyStats))

	// Generate statistics chart (always create a chart, even with empty stats)
	statsChart, err := builders.NewChartBuilder(hourlyStats).Build()
	if err != nil {
		m.handler.logger.Error("Failed to build stats chart", zap.Error(err))
	}

	// Get list of currently active reviewers
	activeUsers := m.handler.sessionManager.GetActiveUsers(context.Background())

	// Store data in session
	s.Set(constants.SessionKeyConfirmedCount, confirmedCount)
	s.Set(constants.SessionKeyFlaggedCount, flaggedCount)
	s.Set(constants.SessionKeyClearedCount, clearedCount)
	s.SetBuffer(constants.SessionKeyImageBuffer, statsChart)
	s.Set(constants.SessionKeyActiveUsers, activeUsers)
	s.Set(constants.SessionKeyWorkerStatuses, workerStatuses)

	m.handler.paginationManager.NavigateTo(event, s, m.page, content)
}

// handleSelectMenu processes select menu interactions by routing to the
// appropriate section based on the selected option.
func (m *Menu) handleSelectMenu(event *events.ComponentInteractionCreate, s *session.Session, customID string, option string) {
	if customID != constants.ActionSelectMenuCustomID {
		return
	}

	// Get bot settings to check reviewer status
	var settings *database.BotSetting
	s.GetInterface(constants.SessionKeyBotSettings, &settings)

	switch option {
	case constants.StartReviewCustomID:
		m.handler.reviewHandler.ShowReviewMenu(event, s)
	case constants.UserSettingsCustomID:
		m.handler.settingsHandler.ShowUserSettings(event, s)
	case constants.BotSettingsCustomID:
		if !settings.IsAdmin(uint64(event.User().ID)) {
			m.handler.logger.Error("User is not in admin list but somehow attempted to access bot settings", zap.Uint64("user_id", uint64(event.User().ID)))
			m.handler.paginationManager.RespondWithError(event, "You do not have permission to access bot settings.")
			return
		}
		m.handler.settingsHandler.ShowBotSettings(event, s)
	case constants.LogActivityBrowserCustomID:
		if !settings.IsReviewer(uint64(event.User().ID)) {
			m.handler.logger.Error("User is not in reviewer list but somehow attempted to access log browser", zap.Uint64("user_id", uint64(event.User().ID)))
			m.handler.paginationManager.RespondWithError(event, "You do not have permission to access log browser.")
			return
		}
		m.handler.logHandler.ShowLogMenu(event, s)
	case constants.QueueManagerCustomID:
		if !settings.IsReviewer(uint64(event.User().ID)) {
			m.handler.logger.Error("User is not in reviewer list but somehow attempted to access queue manager", zap.Uint64("user_id", uint64(event.User().ID)))
			m.handler.paginationManager.RespondWithError(event, "You do not have permission to access queue manager.")
			return
		}
		m.handler.queueHandler.ShowQueueMenu(event, s)
	}
}

// handleButton processes button interactions, mainly handling refresh requests
// to update the dashboard statistics.
func (m *Menu) handleButton(event *events.ComponentInteractionCreate, s *session.Session, customID string) {
	if customID == constants.RefreshButtonCustomID {
		m.ShowDashboard(event, s, "Refreshed dashboard.")
	}
}
