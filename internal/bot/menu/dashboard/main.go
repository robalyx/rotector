package dashboard

import (
	"context"
	"errors"
	"strconv"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	builder "github.com/rotector/rotector/internal/bot/builder/dashboard"
	"github.com/rotector/rotector/internal/bot/constants"
	"github.com/rotector/rotector/internal/bot/core/pagination"
	"github.com/rotector/rotector/internal/bot/core/session"
	"github.com/rotector/rotector/internal/bot/interfaces"
	"github.com/rotector/rotector/internal/common/storage/database/types"
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
			return builder.NewBuilder(s, m.layout.redisClient).Build()
		},
		SelectHandlerFunc: m.handleSelectMenu,
		ButtonHandlerFunc: m.handleButton,
		ModalHandlerFunc:  m.handleModal,
	}
	return m
}

// Show prepares and displays the dashboard interface.
func (m *MainMenu) Show(event interfaces.CommonEvent, s *session.Session, content string) {
	// If the dashboard is already refreshed, directly navigate to the page
	if s.GetBool(constants.SessionKeyIsRefreshed) {
		m.layout.paginationManager.NavigateTo(event, s, m.page, content)
		return
	}

	// Get all counts in a single transaction
	userCounts, groupCounts, err := m.layout.db.Stats().GetCurrentCounts(context.Background())
	if err != nil {
		m.layout.logger.Error("Failed to get counts", zap.Error(err))
	}

	// Get list of currently active reviewers
	activeUsers := m.layout.sessionManager.GetActiveUsers(context.Background())

	// Get worker statuses
	workerStatuses, err := m.layout.workerMonitor.GetAllStatuses(context.Background())
	if err != nil {
		m.layout.logger.Error("Failed to get worker statuses", zap.Error(err))
	}

	// Store data in session
	s.Set(constants.SessionKeyUserID, uint64(event.User().ID))
	s.Set(constants.SessionKeyUserCounts, userCounts)
	s.Set(constants.SessionKeyGroupCounts, groupCounts)
	s.Set(constants.SessionKeyActiveUsers, activeUsers)
	s.Set(constants.SessionKeyWorkerStatuses, workerStatuses)
	s.Set(constants.SessionKeyIsRefreshed, true)

	m.layout.paginationManager.NavigateTo(event, s, m.page, content)
}

// handleSelectMenu processes select menu interactions.
func (m *MainMenu) handleSelectMenu(event *events.ComponentInteractionCreate, s *session.Session, customID string, option string) {
	if customID != constants.ActionSelectMenuCustomID {
		return
	}

	// Get bot settings to check reviewer status
	var settings *types.BotSetting
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
	case constants.ChatAssistantCustomID:
		if !settings.IsReviewer(uint64(event.User().ID)) {
			m.layout.logger.Error("User is not in reviewer list but somehow attempted to access chat assistant", zap.Uint64("user_id", uint64(event.User().ID)))
			m.layout.paginationManager.RespondWithError(event, "You do not have permission to access the chat assistant.")
			return
		}
		m.layout.chatLayout.Show(event, s)
	case constants.LookupUserCustomID:
		m.handleLookupUser(event)
	case constants.LookupGroupCustomID:
		m.handleLookupGroup(event)
	case constants.AppealMenuCustomID:
		m.layout.appealLayout.ShowOverview(event, s, "")
	}
}

// handleLookupUser opens a modal for entering a specific user ID to review.
func (m *MainMenu) handleLookupUser(event *events.ComponentInteractionCreate) {
	modal := discord.NewModalCreateBuilder().
		SetCustomID(constants.LookupUserModalCustomID).
		SetTitle("Lookup User").
		AddActionRow(
			discord.NewTextInput(constants.LookupUserInputCustomID, discord.TextInputStyleShort, "User ID").
				WithRequired(true).
				WithPlaceholder("Enter the user ID to lookup..."),
		).
		Build()

	if err := event.Modal(modal); err != nil {
		m.layout.logger.Error("Failed to create user lookup modal", zap.Error(err))
		m.layout.paginationManager.RespondWithError(event, "Failed to open the user lookup modal. Please try again.")
	}
}

// handleLookupGroup opens a modal for entering a specific group ID to review.
func (m *MainMenu) handleLookupGroup(event *events.ComponentInteractionCreate) {
	modal := discord.NewModalCreateBuilder().
		SetCustomID(constants.LookupGroupModalCustomID).
		SetTitle("Lookup Group").
		AddActionRow(
			discord.NewTextInput(constants.LookupGroupInputCustomID, discord.TextInputStyleShort, "Group ID").
				WithRequired(true).
				WithPlaceholder("Enter the group ID to lookup..."),
		).
		Build()

	if err := event.Modal(modal); err != nil {
		m.layout.logger.Error("Failed to create group lookup modal", zap.Error(err))
		m.layout.paginationManager.RespondWithError(event, "Failed to open the group lookup modal. Please try again.")
	}
}

// handleModal processes modal submissions.
func (m *MainMenu) handleModal(event *events.ModalSubmitInteractionCreate, s *session.Session) {
	switch event.Data.CustomID {
	case constants.LookupUserModalCustomID:
		m.handleLookupUserModalSubmit(event, s)
	case constants.LookupGroupModalCustomID:
		m.handleLookupGroupModalSubmit(event, s)
	}
}

// handleLookupUserModalSubmit processes the user ID input and opens the review menu.
func (m *MainMenu) handleLookupUserModalSubmit(event *events.ModalSubmitInteractionCreate, s *session.Session) {
	// Get and validate the user ID input
	userIDStr := event.Data.Text(constants.LookupUserInputCustomID)
	userID, err := strconv.ParseUint(userIDStr, 10, 64)
	if err != nil {
		m.layout.paginationManager.NavigateTo(event, s, m.page, "Invalid user ID format. Please enter a valid number.")
		return
	}

	// Get user from database
	user, err := m.layout.db.Users().GetUserByID(context.Background(), userID, types.UserFields{}, true)
	if err != nil {
		if errors.Is(err, types.ErrUserNotFound) {
			m.layout.paginationManager.NavigateTo(event, s, m.page, "Failed to find user. They may not be in our database or is reserved.")
			return
		}
		m.layout.logger.Error("Failed to fetch user", zap.Error(err))
		m.layout.paginationManager.RespondWithError(event, "Failed to fetch user for review. Please try again.")
		return
	}

	// Store user in session and show review menu
	s.Set(constants.SessionKeyTarget, user)
	m.layout.userReviewLayout.ShowReviewMenu(event, s)
}

// handleLookupGroupModalSubmit processes the group ID input and opens the review menu.
func (m *MainMenu) handleLookupGroupModalSubmit(event *events.ModalSubmitInteractionCreate, s *session.Session) {
	// Get and validate the group ID input
	groupIDStr := event.Data.Text(constants.LookupGroupInputCustomID)
	groupID, err := strconv.ParseUint(groupIDStr, 10, 64)
	if err != nil {
		m.layout.paginationManager.NavigateTo(event, s, m.page, "Invalid group ID format. Please enter a valid number.")
		return
	}

	// Get group from database
	group, err := m.layout.db.Groups().GetGroupByID(context.Background(), groupID, types.GroupFields{})
	if err != nil {
		if errors.Is(err, types.ErrGroupNotFound) {
			m.layout.paginationManager.NavigateTo(event, s, m.page, "Failed to find group. It may not be in our database or is reserved.")
			return
		}
		m.layout.logger.Error("Failed to fetch group", zap.Error(err))
		m.layout.paginationManager.RespondWithError(event, "Failed to fetch group for review. Please try again.")
		return
	}

	// Store group in session and show review menu
	s.Set(constants.SessionKeyGroupTarget, group)
	m.layout.groupReviewLayout.Show(event, s)
}

// handleButton processes button interactions, mainly handling refresh requests
// to update the dashboard statistics.
func (m *MainMenu) handleButton(event *events.ComponentInteractionCreate, s *session.Session, customID string) {
	if customID == constants.RefreshButtonCustomID {
		s.Set(constants.SessionKeyIsRefreshed, false)
		m.Show(event, s, "Refreshed dashboard.")
	}
}
