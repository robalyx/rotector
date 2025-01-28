package dashboard

import (
	"context"
	"errors"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	builder "github.com/robalyx/rotector/internal/bot/builder/dashboard"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/pagination"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/bot/interfaces"
	"github.com/robalyx/rotector/internal/bot/utils"
	"github.com/robalyx/rotector/internal/common/storage/database/types"
	"github.com/robalyx/rotector/internal/common/storage/database/types/enum"
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
		Name: constants.DashboardPageName,
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
	if session.StatsIsRefreshed.Get(s) {
		m.layout.paginationManager.NavigateTo(event, s, m.page, content)
		return
	}

	// Get all counts in a single transaction
	userCounts, groupCounts, err := m.layout.db.Models().Stats().GetCurrentCounts(context.Background())
	if err != nil {
		m.layout.logger.Error("Failed to get counts", zap.Error(err))
	}

	// Get vote statistics for the user
	voteStats, err := m.layout.db.Models().Votes().GetUserVoteStats(context.Background(), uint64(event.User().ID), enum.LeaderboardPeriodAllTime)
	if err != nil {
		m.layout.logger.Error("Failed to get vote statistics", zap.Error(err))
		voteStats = &types.VoteAccuracy{DiscordUserID: uint64(event.User().ID)} // Use empty stats on error
	}

	// Get list of currently active reviewers
	activeUsers := m.layout.sessionManager.GetActiveUsers(context.Background())

	// Get worker statuses
	workerStatuses, err := m.layout.workerMonitor.GetAllStatuses(context.Background())
	if err != nil {
		m.layout.logger.Error("Failed to get worker statuses", zap.Error(err))
	}

	// Store data in session
	session.StatsUserCounts.Set(s, userCounts)
	session.StatsGroupCounts.Set(s, groupCounts)
	session.StatsActiveUsers.Set(s, activeUsers)
	session.StatusWorkers.Set(s, workerStatuses)
	session.StatsVotes.Set(s, voteStats)
	session.StatsIsRefreshed.Set(s, true)

	m.layout.paginationManager.NavigateTo(event, s, m.page, content)
}

// handleSelectMenu processes select menu interactions.
func (m *MainMenu) handleSelectMenu(event *events.ComponentInteractionCreate, s *session.Session, customID string, option string) {
	if customID != constants.ActionSelectMenuCustomID {
		return
	}

	isReviewer := s.BotSettings().IsReviewer(uint64(event.User().ID))
	isAdmin := s.BotSettings().IsAdmin(uint64(event.User().ID))

	switch option {
	case constants.StartUserReviewButtonCustomID:
		m.layout.userReviewLayout.ShowReviewMenu(event, s)
	case constants.StartGroupReviewButtonCustomID:
		m.layout.groupReviewLayout.Show(event, s)
	case constants.LookupUserButtonCustomID:
		m.handleLookupUser(event)
	case constants.LookupGroupButtonCustomID:
		m.handleLookupGroup(event)
	case constants.UserSettingsButtonCustomID:
		m.layout.settingLayout.ShowUser(event, s)
	case constants.ActivityBrowserButtonCustomID:
		if !isReviewer {
			m.layout.logger.Error("User is not in reviewer list but somehow attempted to access log browser", zap.Uint64("user_id", uint64(event.User().ID)))
			m.layout.paginationManager.RespondWithError(event, "You do not have permission to access log browser.")
			return
		}
		m.layout.logLayout.Show(event, s)
	case constants.LeaderboardMenuButtonCustomID:
		m.layout.leaderboardLayout.Show(event, s)
	case constants.QueueManagerButtonCustomID:
		if !isReviewer {
			m.layout.logger.Error("User is not in reviewer list but somehow attempted to access queue manager", zap.Uint64("user_id", uint64(event.User().ID)))
			m.layout.paginationManager.RespondWithError(event, "You do not have permission to access queue manager.")
			return
		}
		m.layout.queueLayout.Show(event, s)
	case constants.ChatAssistantButtonCustomID:
		if !isReviewer {
			m.layout.logger.Error("User is not in reviewer list but somehow attempted to access chat assistant", zap.Uint64("user_id", uint64(event.User().ID)))
			m.layout.paginationManager.RespondWithError(event, "You do not have permission to access the chat assistant.")
			return
		}
		m.layout.chatLayout.Show(event, s)
	case constants.WorkerStatusButtonCustomID:
		if !isReviewer {
			m.layout.logger.Error("User is not in reviewer list but somehow attempted to access worker status", zap.Uint64("user_id", uint64(event.User().ID)))
			m.layout.paginationManager.RespondWithError(event, "You do not have permission to access worker status.")
			return
		}
		m.layout.statusLayout.Show(event, s)
	case constants.AppealMenuButtonCustomID:
		m.layout.appealLayout.ShowOverview(event, s, "")
	case constants.AdminMenuButtonCustomID:
		if !isAdmin {
			m.layout.logger.Error("Non-admin attempted to access admin menu",
				zap.Uint64("user_id", uint64(event.User().ID)))
			m.layout.paginationManager.RespondWithError(event, "You do not have permission to access admin tools.")
			return
		}
		m.layout.adminLayout.Show(event, s)
	}
}

// handleLookupUser opens a modal for entering a specific user ID to lookup.
func (m *MainMenu) handleLookupUser(event *events.ComponentInteractionCreate) {
	modal := discord.NewModalCreateBuilder().
		SetCustomID(constants.LookupUserModalCustomID).
		SetTitle("Lookup User").
		AddActionRow(
			discord.NewTextInput(constants.LookupUserInputCustomID, discord.TextInputStyleShort, "User ID or UUID").
				WithRequired(true).
				WithPlaceholder("Enter the user ID or UUID to lookup..."),
		).
		Build()
	if err := event.Modal(modal); err != nil {
		m.layout.logger.Error("Failed to create user lookup modal", zap.Error(err))
		m.layout.paginationManager.RespondWithError(event, "Failed to open the user lookup modal. Please try again.")
	}
}

// handleLookupGroup opens a modal for entering a specific group ID to lookup.
func (m *MainMenu) handleLookupGroup(event *events.ComponentInteractionCreate) {
	modal := discord.NewModalCreateBuilder().
		SetCustomID(constants.LookupGroupModalCustomID).
		SetTitle("Lookup Group").
		AddActionRow(
			discord.NewTextInput(constants.LookupGroupInputCustomID, discord.TextInputStyleShort, "Group ID or UUID").
				WithRequired(true).
				WithPlaceholder("Enter the group ID or UUID to lookup..."),
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
	// Get the user ID input
	userIDStr := event.Data.Text(constants.LookupUserInputCustomID)

	// Parse profile URL if provided
	if utils.IsRobloxProfileURL(userIDStr) {
		var err error
		userIDStr, err = utils.ExtractUserIDFromURL(userIDStr)
		if err != nil {
			m.layout.paginationManager.NavigateTo(event, s, m.page, "Invalid Roblox profile URL. Please provide a valid URL or ID.")
			return
		}
	}

	// Get user from database
	user, err := m.layout.db.Models().Users().GetUserByID(context.Background(), userIDStr, types.UserFields{})
	if err != nil {
		switch {
		case errors.Is(err, types.ErrUserNotFound):
			m.layout.paginationManager.NavigateTo(event, s, m.page, "Failed to find user. They may not be in our database.")
		case errors.Is(err, types.ErrInvalidUserID):
			m.layout.paginationManager.NavigateTo(event, s, m.page, "Please provide a valid user ID or UUID.")
		default:
			m.layout.logger.Error("Failed to fetch user", zap.Error(err))
			m.layout.paginationManager.RespondWithError(event, "Failed to fetch user for review. Please try again.")
		}
		return
	}

	// Store user in session and show review menu
	session.UserTarget.Set(s, user)
	m.layout.userReviewLayout.ShowReviewMenu(event, s)

	// Log the lookup action
	m.layout.db.Models().Activities().Log(context.Background(), &types.ActivityLog{
		ActivityTarget: types.ActivityTarget{
			UserID: user.ID,
		},
		ReviewerID:        uint64(event.User().ID),
		ActivityType:      enum.ActivityTypeUserLookup,
		ActivityTimestamp: time.Now(),
		Details:           map[string]interface{}{},
	})
}

// handleLookupGroupModalSubmit processes the group ID input and opens the review menu.
func (m *MainMenu) handleLookupGroupModalSubmit(event *events.ModalSubmitInteractionCreate, s *session.Session) {
	// Get the group ID input
	groupIDStr := event.Data.Text(constants.LookupGroupInputCustomID)

	// Parse group URL if provided
	if utils.IsRobloxGroupURL(groupIDStr) {
		var err error
		groupIDStr, err = utils.ExtractGroupIDFromURL(groupIDStr)
		if err != nil {
			m.layout.paginationManager.NavigateTo(event, s, m.page, "Invalid Roblox group URL. Please provide a valid URL or ID.")
			return
		}
	}

	// Get group from database
	group, err := m.layout.db.Models().Groups().GetGroupByID(context.Background(), groupIDStr, types.GroupFields{})
	if err != nil {
		switch {
		case errors.Is(err, types.ErrGroupNotFound):
			m.layout.paginationManager.NavigateTo(event, s, m.page, "Failed to find group. It may not be in our database.")
		case errors.Is(err, types.ErrInvalidGroupID):
			m.layout.paginationManager.NavigateTo(event, s, m.page, "Please provide a valid group ID or UUID.")
		default:
			m.layout.logger.Error("Failed to fetch group", zap.Error(err))
			m.layout.paginationManager.RespondWithError(event, "Failed to fetch group for review. Please try again.")
		}
		return
	}

	// Store group in session and show review menu
	session.GroupTarget.Set(s, group)
	m.layout.groupReviewLayout.Show(event, s)

	// Log the lookup action
	m.layout.db.Models().Activities().Log(context.Background(), &types.ActivityLog{
		ActivityTarget: types.ActivityTarget{
			GroupID: group.ID,
		},
		ReviewerID:        uint64(event.User().ID),
		ActivityType:      enum.ActivityTypeGroupLookup,
		ActivityTimestamp: time.Now(),
		Details:           map[string]interface{}{},
	})
}

// handleButton processes button interactions, mainly handling refresh requests
// to update the dashboard statistics.
func (m *MainMenu) handleButton(event *events.ComponentInteractionCreate, s *session.Session, customID string) {
	if customID == constants.RefreshButtonCustomID {
		session.StatsIsRefreshed.Set(s, false)
		m.Show(event, s, "Refreshed dashboard.")
	}
}
