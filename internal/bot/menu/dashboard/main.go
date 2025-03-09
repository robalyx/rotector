package dashboard

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/snowflake/v2"
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
	voteStats, err := m.layout.db.Models().Votes().GetUserVoteStats(
		context.Background(),
		uint64(event.User().ID),
		enum.LeaderboardPeriodAllTime,
	)
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
func (m *Menu) handleSelectMenu(
	event *events.ComponentInteractionCreate, s *session.Session, r *pagination.Respond, customID, option string,
) {
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
	case constants.LookupRobloxUserButtonCustomID:
		m.handleLookupRobloxUser(event, s, r)
	case constants.LookupRobloxGroupButtonCustomID:
		m.handleLookupRobloxGroup(event, s, r)
	case constants.LookupDiscordUserButtonCustomID:
		m.handleLookupDiscordUser(event, s, r)
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
	case constants.GuildOwnerMenuButtonCustomID:
		if !session.IsGuildOwner.Get(s) && !isAdmin {
			r.Error(event, "You must be a guild owner to access these tools.")
			return
		}

		// Set guild ID and name in session
		guildID := uint64(*event.GuildID())
		session.GuildStatsID.Set(s, guildID)

		if guild, err := event.Client().Rest().GetGuild(*event.GuildID(), false); err == nil {
			session.GuildStatsName.Set(s, guild.Name)
		}

		r.Show(event, s, constants.GuildOwnerPageName, "")
	}
}

// handleLookupRobloxUser opens a modal for entering a specific Roblox user ID to lookup.
func (m *Menu) handleLookupRobloxUser(event *events.ComponentInteractionCreate, s *session.Session, r *pagination.Respond) {
	modal := discord.NewModalCreateBuilder().
		SetCustomID(constants.LookupRobloxUserModalCustomID).
		SetTitle("Lookup Roblox User").
		AddActionRow(
			discord.NewTextInput(constants.LookupRobloxUserInputCustomID, discord.TextInputStyleShort, "User ID or UUID").
				WithRequired(true).
				WithPlaceholder("Enter the user ID or UUID to lookup..."),
		)

	r.Modal(event, s, modal)
}

// handleLookupRobloxGroup opens a modal for entering a specific Roblox group ID to lookup.
func (m *Menu) handleLookupRobloxGroup(event *events.ComponentInteractionCreate, s *session.Session, r *pagination.Respond) {
	modal := discord.NewModalCreateBuilder().
		SetCustomID(constants.LookupRobloxGroupModalCustomID).
		SetTitle("Lookup Roblox Group").
		AddActionRow(
			discord.NewTextInput(constants.LookupRobloxGroupInputCustomID, discord.TextInputStyleShort, "Group ID or UUID").
				WithRequired(true).
				WithPlaceholder("Enter the group ID or UUID to lookup..."),
		)

	r.Modal(event, s, modal)
}

// handleLookupDiscordUser opens a modal for entering a specific Discord user ID to lookup.
func (m *Menu) handleLookupDiscordUser(event *events.ComponentInteractionCreate, s *session.Session, r *pagination.Respond) {
	modal := discord.NewModalCreateBuilder().
		SetCustomID(constants.LookupDiscordUserModalCustomID).
		SetTitle("Lookup Discord User").
		AddActionRow(
			discord.NewTextInput(constants.LookupDiscordUserInputCustomID, discord.TextInputStyleShort, "Discord User ID").
				WithRequired(true).
				WithPlaceholder("Enter the Discord user ID to lookup..."),
		)

	r.Modal(event, s, modal)
}

// handleModal processes modal submissions.
func (m *Menu) handleModal(event *events.ModalSubmitInteractionCreate, s *session.Session, r *pagination.Respond) {
	switch event.Data.CustomID {
	case constants.LookupRobloxUserModalCustomID:
		m.handleLookupRobloxUserModalSubmit(event, s, r)
	case constants.LookupRobloxGroupModalCustomID:
		m.handleLookupRobloxGroupModalSubmit(event, s, r)
	case constants.LookupDiscordUserModalCustomID:
		m.handleLookupDiscordUserModalSubmit(event, s, r)
	}
}

// handleLookupRobloxUserModalSubmit processes the user ID input and opens the review menu.
func (m *Menu) handleLookupRobloxUserModalSubmit(
	event *events.ModalSubmitInteractionCreate, s *session.Session, r *pagination.Respond,
) {
	// Get the user ID input
	userIDStr := event.Data.Text(constants.LookupRobloxUserInputCustomID)

	// Parse profile URL if provided
	parsedURL, err := utils.ExtractUserIDFromURL(userIDStr)
	if err == nil {
		userIDStr = parsedURL
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
		Details:           map[string]any{},
	})
}

// handleLookupRobloxGroupModalSubmit processes the group ID input and opens the review menu.
func (m *Menu) handleLookupRobloxGroupModalSubmit(
	event *events.ModalSubmitInteractionCreate, s *session.Session, r *pagination.Respond,
) {
	// Get the group ID input
	groupIDStr := event.Data.Text(constants.LookupRobloxGroupInputCustomID)

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
		Details:           map[string]any{},
	})
}

// handleLookupDiscordUserModalSubmit processes the Discord user ID input and shows the user's flagged servers.
func (m *Menu) handleLookupDiscordUserModalSubmit(
	event *events.ModalSubmitInteractionCreate, s *session.Session, r *pagination.Respond,
) {
	// Get the Discord user ID input
	discordUserIDStr := event.Data.Text(constants.LookupDiscordUserInputCustomID)

	// Parse the Discord user ID
	discordUserID, err := strconv.ParseUint(discordUserIDStr, 10, 64)
	if err != nil {
		r.Cancel(event, s, "Please provide a valid Discord user ID.")
		return
	}

	// Store the Discord user ID in session
	session.DiscordUserLookupID.Set(s, discordUserID)

	// Attempt to get Discord username if possible
	var username string
	if user, err := event.Client().Rest().GetUser(snowflake.ID(discordUserID)); err == nil {
		username = user.Username
		session.DiscordUserLookupName.Set(s, username)
	}

	// Reset cursors
	session.GuildLookupCursor.Delete(s)
	session.GuildLookupNextCursor.Delete(s)
	session.GuildLookupPrevCursors.Delete(s)
	session.PaginationHasNextPage.Delete(s)
	session.PaginationHasPrevPage.Delete(s)

	// Show the guild lookup page
	r.Show(event, s, constants.GuildLookupPageName, "")

	// Log the lookup action
	m.layout.db.Models().Activities().Log(context.Background(), &types.ActivityLog{
		ActivityTarget:    types.ActivityTarget{},
		ReviewerID:        uint64(event.User().ID),
		ActivityType:      enum.ActivityTypeUserLookupDiscord,
		ActivityTimestamp: time.Now(),
		Details: map[string]any{
			"discord_user_id": discordUserID,
		},
	})
}

// handleButton processes button interactions, mainly handling refresh requests
// to update the dashboard statistics.
func (m *Menu) handleButton(
	event *events.ComponentInteractionCreate, s *session.Session, r *pagination.Respond, customID string,
) {
	if customID == constants.RefreshButtonCustomID {
		session.StatsIsRefreshed.Set(s, false)
		r.Reload(event, s, "Refreshed dashboard.")
	}
}
