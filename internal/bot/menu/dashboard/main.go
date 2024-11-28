package dashboard

import (
	"context"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/snowflake/v2"
	builder "github.com/rotector/rotector/internal/bot/builder/dashboard"
	"github.com/rotector/rotector/internal/bot/constants"
	"github.com/rotector/rotector/internal/bot/core/pagination"
	"github.com/rotector/rotector/internal/bot/core/session"
	"github.com/rotector/rotector/internal/bot/interfaces"
	"github.com/rotector/rotector/internal/common/storage/database/models"
	"github.com/rotector/rotector/internal/worker/core"
	"go.uber.org/zap"
)

// MainMenu handles dashboard operations and their interactions.
type MainMenu struct {
	layout *Layout
	page   *pagination.Page
}

// NewMainMenu creates a MainMenu by initializing the dashboard menu and registering its
// page with the pagination manager.
func NewMainMenu(layout *Layout) *MainMenu {
	m := &MainMenu{layout: layout}
	m.page = &pagination.Page{
		Name: "Dashboard",
		Message: func(s *session.Session) *discord.MessageUpdateBuilder {
			var botSettings *models.BotSetting
			s.GetInterface(constants.SessionKeyBotSettings, &botSettings)
			var userCounts *models.UserCounts
			s.GetInterface(constants.SessionKeyUserCounts, &userCounts)
			var groupCounts *models.GroupCounts
			s.GetInterface(constants.SessionKeyGroupCounts, &groupCounts)
			var activeUsers []snowflake.ID
			s.GetInterface(constants.SessionKeyActiveUsers, &activeUsers)
			var workerStatuses []core.Status
			s.GetInterface(constants.SessionKeyWorkerStatuses, &workerStatuses)

			userID := s.GetUint64(constants.SessionKeyUserID)
			userStatsBuffer := s.GetBuffer(constants.SessionKeyUserStatsBuffer)
			groupStatsBuffer := s.GetBuffer(constants.SessionKeyGroupStatsBuffer)

			return builder.NewBuilder(
				botSettings,
				userID,
				userCounts,
				groupCounts,
				userStatsBuffer,
				groupStatsBuffer,
				activeUsers,
				workerStatuses,
			).Build()
		},
		SelectHandlerFunc: m.handleSelectMenu,
		ButtonHandlerFunc: m.handleButton,
	}
	return m
}

// Show prepares and displays the dashboard interface.
func (m *MainMenu) Show(event interfaces.CommonEvent, s *session.Session, content string) {
	// Get bot settings
	botSettings, err := m.layout.db.Settings().GetBotSettings(context.Background())
	if err != nil {
		m.layout.logger.Error("Failed to get bot settings", zap.Error(err))
		return
	}

	// Get all counts in a single transaction
	userCounts, groupCounts, err := m.layout.db.Stats().GetCurrentCounts(context.Background())
	if err != nil {
		m.layout.logger.Error("Failed to get counts", zap.Error(err))
	}

	// Get hourly stats for the chart
	hourlyStats, err := m.layout.db.Stats().GetHourlyStats(context.Background())
	if err != nil {
		m.layout.logger.Error("Failed to get hourly stats", zap.Error(err))
	}

	// Generate statistics charts
	userStatsChart, groupStatsChart, err := builder.NewChartBuilder(hourlyStats).Build()
	if err != nil {
		m.layout.logger.Error("Failed to build stats charts", zap.Error(err))
	}

	// Get list of currently active reviewers
	activeUsers := m.layout.sessionManager.GetActiveUsers(context.Background())

	// Get worker statuses
	workerStatuses, err := m.layout.workerMonitor.GetAllStatuses(context.Background())
	if err != nil {
		m.layout.logger.Error("Failed to get worker statuses", zap.Error(err))
	}

	// Store data in session
	s.Set(constants.SessionKeyBotSettings, botSettings)
	s.Set(constants.SessionKeyUserID, uint64(event.User().ID))
	s.Set(constants.SessionKeyUserCounts, userCounts)
	s.Set(constants.SessionKeyGroupCounts, groupCounts)
	s.SetBuffer(constants.SessionKeyUserStatsBuffer, userStatsChart)
	s.SetBuffer(constants.SessionKeyGroupStatsBuffer, groupStatsChart)
	s.Set(constants.SessionKeyActiveUsers, activeUsers)
	s.Set(constants.SessionKeyWorkerStatuses, workerStatuses)

	m.layout.paginationManager.NavigateTo(event, s, m.page, content)
}

// handleSelectMenu processes select menu interactions by routing to the
// appropriate section based on the selected option.
func (m *MainMenu) handleSelectMenu(event *events.ComponentInteractionCreate, s *session.Session, customID string, option string) {
	if customID != constants.ActionSelectMenuCustomID {
		return
	}

	// Get bot settings to check reviewer status
	var settings *models.BotSetting
	s.GetInterface(constants.SessionKeyBotSettings, &settings)

	switch option {
	case constants.StartUserReviewCustomID:
		m.layout.userReviewLayout.ShowReviewMenu(event, s)
	case constants.StartGroupReviewCustomID:
		m.layout.groupReviewLayout.Show(event, s)
	case constants.UserSettingsCustomID:
		m.layout.settingLayout.ShowUser(event, s)
	case constants.BotSettingsCustomID:
		if !settings.IsAdmin(uint64(event.User().ID)) {
			m.layout.logger.Error("User is not in admin list but somehow attempted to access bot settings", zap.Uint64("user_id", uint64(event.User().ID)))
			m.layout.paginationManager.RespondWithError(event, "You do not have permission to access bot settings.")
			return
		}
		m.layout.settingLayout.ShowBot(event, s)
	case constants.LogActivityBrowserCustomID:
		if !settings.IsReviewer(uint64(event.User().ID)) {
			m.layout.logger.Error("User is not in reviewer list but somehow attempted to access log browser", zap.Uint64("user_id", uint64(event.User().ID)))
			m.layout.paginationManager.RespondWithError(event, "You do not have permission to access log browser.")
			return
		}
		m.layout.logLayout.Show(event, s)
	case constants.QueueManagerCustomID:
		if !settings.IsReviewer(uint64(event.User().ID)) {
			m.layout.logger.Error("User is not in reviewer list but somehow attempted to access queue manager", zap.Uint64("user_id", uint64(event.User().ID)))
			m.layout.paginationManager.RespondWithError(event, "You do not have permission to access queue manager.")
			return
		}
		m.layout.queueLayout.Show(event, s)
	}
}

// handleButton processes button interactions, mainly handling refresh requests
// to update the dashboard statistics.
func (m *MainMenu) handleButton(event *events.ComponentInteractionCreate, s *session.Session, customID string) {
	if customID == constants.RefreshButtonCustomID {
		m.Show(event, s, "Refreshed dashboard.")
	}
}
