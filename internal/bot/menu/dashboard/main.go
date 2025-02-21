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

// Menu handles dashboard operations and their interactions.
type Menu struct {
	layout *Layout
	page   *pagination.Page
}

// NewMenu creates a new dashboard menu.
func NewMenu(layout *Layout) *Menu {
	m := &Menu{layout: layout}
	m.page = &pagination.Page{
		Name: constants.DashboardPageName,
		Message: func(s *session.Session) *discord.MessageUpdateBuilder {
			return builder.NewBuilder(s, m.layout.redisClient).Build()
		},
		ShowHandlerFunc:   m.Show,
		SelectHandlerFunc: m.handleSelectMenu,
		ButtonHandlerFunc: m.handleButton,
		ModalHandlerFunc:  m.handleModal,
	}
	return m
}

// Show prepares and displays the dashboard interface.
func (m *Menu) Show(event interfaces.CommonEvent, s *session.Session, _ *pagination.Respond) {
	// Skip if dashboard is already refreshed
	if session.StatsIsRefreshed.Get(s) {
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

	// Store data in session
	session.StatsUserCounts.Set(s, userCounts)
	session.StatsGroupCounts.Set(s, groupCounts)
	session.StatsActiveUsers.Set(s, activeUsers)
	session.StatsVotes.Set(s, voteStats)
	session.StatsIsRefreshed.Set(s, true)
}

// handleSelectMenu processes select menu interactions.
func (m *Menu) handleSelectMenu(event *events.ComponentInteractionCreate, s *session.Session, r *pagination.Respond, customID, option string) {
	if customID != constants.ActionSelectMenuCustomID {
		return
	}

	userID := uint64(event.User().ID)
	isReviewer := s.BotSettings().IsReviewer(userID)
	isAdmin := s.BotSettings().IsAdmin(userID)

	// Check reviewer-only options
	switch option {
	case constants.ActivityBrowserButtonCustomID,
		constants.QueueManagerButtonCustomID,
		constants.ChatAssistantButtonCustomID,
		constants.WorkerStatusButtonCustomID,
		constants.ReviewerStatsButtonCustomID:
		if !isReviewer {
			m.layout.logger.Error("Non-reviewer attempted restricted action",
				zap.Uint64("user_id", userID),
				zap.String("action", option))
			r.Error(event, "You do not have permission to perform this action.")
			return
		}
	case constants.AdminMenuButtonCustomID:
		if !isAdmin {
			m.layout.logger.Error("Non-admin attempted restricted action",
				zap.Uint64("user_id", userID),
				zap.String("action", option))
			r.Error(event, "You do not have permission to perform this action.")
			return
		}
	}

	// Process selected option
	switch option {
	case constants.StartUserReviewButtonCustomID:
		r.Show(event, s, constants.UserReviewPageName, "")
	case constants.StartGroupReviewButtonCustomID:
		r.Show(event, s, constants.GroupReviewPageName, "")
	case constants.LookupUserButtonCustomID:
		m.handleLookupUser(event, r)
	case constants.LookupGroupButtonCustomID:
		m.handleLookupGroup(event, r)
	case constants.UserSettingsButtonCustomID:
		r.Show(event, s, constants.UserSettingsPageName, "")
	case constants.ActivityBrowserButtonCustomID:
		r.Show(event, s, constants.LogPageName, "")
	case constants.LeaderboardMenuButtonCustomID:
		r.Show(event, s, constants.LeaderboardPageName, "")
	case constants.QueueManagerButtonCustomID:
		r.Show(event, s, constants.QueuePageName, "")
	case constants.ChatAssistantButtonCustomID:
		r.Show(event, s, constants.ChatPageName, "")
	case constants.WorkerStatusButtonCustomID:
		r.Show(event, s, constants.StatusPageName, "")
	case constants.AppealMenuButtonCustomID:
		r.Show(event, s, constants.AppealOverviewPageName, "")
	case constants.AdminMenuButtonCustomID:
		r.Show(event, s, constants.AdminPageName, "")
	case constants.ReviewerStatsButtonCustomID:
		r.Show(event, s, constants.ReviewerStatsPageName, "")
	}
}

// handleLookupUser opens a modal for entering a specific user ID to lookup.
func (m *Menu) handleLookupUser(event *events.ComponentInteractionCreate, r *pagination.Respond) {
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
		r.Error(event, "Failed to open the user lookup modal. Please try again.")
	}
}

// handleLookupGroup opens a modal for entering a specific group ID to lookup.
func (m *Menu) handleLookupGroup(event *events.ComponentInteractionCreate, r *pagination.Respond) {
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
		r.Error(event, "Failed to open the group lookup modal. Please try again.")
	}
}

// handleModal processes modal submissions.
func (m *Menu) handleModal(event *events.ModalSubmitInteractionCreate, s *session.Session, r *pagination.Respond) {
	switch event.Data.CustomID {
	case constants.LookupUserModalCustomID:
		m.handleLookupUserModalSubmit(event, s, r)
	case constants.LookupGroupModalCustomID:
		m.handleLookupGroupModalSubmit(event, s, r)
	}
}

// handleLookupUserModalSubmit processes the user ID input and opens the review menu.
func (m *Menu) handleLookupUserModalSubmit(event *events.ModalSubmitInteractionCreate, s *session.Session, r *pagination.Respond) {
	// Get the user ID input
	userIDStr := event.Data.Text(constants.LookupUserInputCustomID)

	// Parse profile URL if provided
	if utils.IsRobloxProfileURL(userIDStr) {
		var err error
		userIDStr, err = utils.ExtractUserIDFromURL(userIDStr)
		if err != nil {
			r.Error(event, "Invalid Roblox profile URL. Please provide a valid URL or ID.")
			return
		}
	}

	// Get user from database
	user, err := m.layout.db.Models().Users().GetUserByID(context.Background(), userIDStr, types.UserFieldAll)
	if err != nil {
		switch {
		case errors.Is(err, types.ErrUserNotFound):
			r.Cancel(event, s, "Failed to find user. They may not be in our database.")
		case errors.Is(err, types.ErrInvalidUserID):
			r.Cancel(event, s, "Please provide a valid user ID or UUID.")
		default:
			m.layout.logger.Error("Failed to fetch user", zap.Error(err))
			r.Error(event, "Failed to fetch user for review. Please try again.")
		}
		return
	}

	// Store user in session and show review menu
	session.UserTarget.Set(s, user)
	r.Show(event, s, constants.UserReviewPageName, "")

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
func (m *Menu) handleLookupGroupModalSubmit(event *events.ModalSubmitInteractionCreate, s *session.Session, r *pagination.Respond) {
	// Get the group ID input
	groupIDStr := event.Data.Text(constants.LookupGroupInputCustomID)

	// Parse group URL if provided
	if utils.IsRobloxGroupURL(groupIDStr) {
		var err error
		groupIDStr, err = utils.ExtractGroupIDFromURL(groupIDStr)
		if err != nil {
			r.Error(event, "Invalid Roblox group URL. Please provide a valid URL or ID.")
			return
		}
	}

	// Get group from database
	group, err := m.layout.db.Models().Groups().GetGroupByID(context.Background(), groupIDStr, types.GroupFieldAll)
	if err != nil {
		switch {
		case errors.Is(err, types.ErrGroupNotFound):
			r.Cancel(event, s, "Failed to find group. It may not be in our database.")
		case errors.Is(err, types.ErrInvalidGroupID):
			r.Cancel(event, s, "Please provide a valid group ID or UUID.")
		default:
			m.layout.logger.Error("Failed to fetch group", zap.Error(err))
			r.Error(event, "Failed to fetch group for review. Please try again.")
		}
		return
	}

	// Store group in session and show review menu
	session.GroupTarget.Set(s, group)
	r.Show(event, s, constants.GroupReviewPageName, "")

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
func (m *Menu) handleButton(event *events.ComponentInteractionCreate, s *session.Session, r *pagination.Respond, customID string) {
	if customID == constants.RefreshButtonCustomID {
		session.StatsIsRefreshed.Set(s, false)
		r.Reload(event, s, "Refreshed dashboard.")
	}
}
