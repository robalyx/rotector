package dashboard

import (
	"errors"
	"strconv"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/interaction"
	"github.com/robalyx/rotector/internal/bot/core/session"
	view "github.com/robalyx/rotector/internal/bot/views/dashboard"
	"github.com/robalyx/rotector/internal/database/types"
	"github.com/robalyx/rotector/internal/database/types/enum"
	"github.com/robalyx/rotector/pkg/utils"
	"go.uber.org/zap"
)

// Menu handles dashboard operations and their interactions.
type Menu struct {
	layout *Layout
	page   *interaction.Page
}

// NewMenu creates a new dashboard menu.
func NewMenu(layout *Layout) *Menu {
	m := &Menu{layout: layout}
	m.page = &interaction.Page{
		Name: constants.DashboardPageName,
		Message: func(s *session.Session) *discord.MessageUpdateBuilder {
			return view.NewBuilder(s, m.layout.redisClient).Build()
		},
		ShowHandlerFunc:   m.Show,
		SelectHandlerFunc: m.handleSelectMenu,
		ButtonHandlerFunc: m.handleButton,
		ModalHandlerFunc:  m.handleModal,
	}
	return m
}

// Show prepares and displays the dashboard interface.
func (m *Menu) Show(ctx *interaction.Context, s *session.Session) {
	// Skip if dashboard is already refreshed
	if session.StatsIsRefreshed.Get(s) {
		return
	}

	// Get all counts in a single transaction
	userCounts, groupCounts, err := m.layout.db.Service().Stats().GetCurrentCounts(ctx.Context())
	if err != nil {
		m.layout.logger.Error("Failed to get counts", zap.Error(err))
	}

	// Only get vote statistics for non-reviewers
	userID := uint64(ctx.Event().User().ID)
	if !s.BotSettings().IsReviewer(userID) {
		voteStats, err := m.layout.db.Service().Vote().GetUserVoteStats(
			ctx.Context(),
			userID,
			enum.LeaderboardPeriodAllTime,
		)
		if err != nil {
			m.layout.logger.Error("Failed to get vote statistics", zap.Error(err))
			voteStats = &types.VoteAccuracy{DiscordUserID: userID} // Use empty stats on error
		}
		session.StatsVotes.Set(s, voteStats)
	}

	// Get list of currently active reviewers
	activeUsers := m.layout.sessionManager.GetActiveUsers(ctx.Context())

	// Store data in session
	session.StatsUserCounts.Set(s, userCounts)
	session.StatsGroupCounts.Set(s, groupCounts)
	session.StatsActiveUsers.Set(s, activeUsers)
	session.StatsIsRefreshed.Set(s, true)
}

// handleSelectMenu processes select menu interactions.
func (m *Menu) handleSelectMenu(ctx *interaction.Context, s *session.Session, customID, option string) {
	if customID != constants.ActionSelectMenuCustomID {
		return
	}

	event := ctx.Event()
	userID := uint64(event.User().ID)
	isReviewer := s.BotSettings().IsReviewer(userID)
	isAdmin := s.BotSettings().IsAdmin(userID)

	// Check reviewer-only options
	switch option {
	case constants.StartGroupReviewButtonCustomID,
		constants.ActivityBrowserButtonCustomID,
		constants.ChatAssistantButtonCustomID,
		constants.WorkerStatusButtonCustomID,
		constants.ReviewerStatsButtonCustomID:
		if !isReviewer {
			m.layout.logger.Error("Non-reviewer attempted restricted action",
				zap.Uint64("user_id", userID),
				zap.String("action", option))
			ctx.Error("You do not have permission to perform this action.")
			return
		}
	case constants.AdminMenuButtonCustomID:
		if !isAdmin {
			m.layout.logger.Error("Non-admin attempted restricted action",
				zap.Uint64("user_id", userID),
				zap.String("action", option))
			ctx.Error("You do not have permission to perform this action.")
			return
		}
	}

	// Process selected option
	switch option {
	case constants.StartUserReviewButtonCustomID:
		ctx.Show(constants.UserReviewPageName, "")
	case constants.StartGroupReviewButtonCustomID:
		ctx.Show(constants.GroupReviewPageName, "")
	case constants.LookupRobloxUserButtonCustomID:
		m.handleLookupRobloxUser(ctx)
	case constants.LookupRobloxGroupButtonCustomID:
		m.handleLookupRobloxGroup(ctx)
	case constants.LookupDiscordUserButtonCustomID:
		m.handleLookupDiscordUser(ctx)
	case constants.UserSettingsButtonCustomID:
		ctx.Show(constants.UserSettingsPageName, "")
	case constants.ActivityBrowserButtonCustomID:
		ctx.Show(constants.LogPageName, "")
	case constants.LeaderboardMenuButtonCustomID:
		ctx.Show(constants.LeaderboardPageName, "")
	case constants.ChatAssistantButtonCustomID:
		ctx.Show(constants.ChatPageName, "")
	case constants.WorkerStatusButtonCustomID:
		ctx.Show(constants.StatusPageName, "")
	case constants.AppealMenuButtonCustomID:
		ctx.Show(constants.AppealOverviewPageName, "")
	case constants.AdminMenuButtonCustomID:
		ctx.Show(constants.AdminPageName, "")
	case constants.ReviewerStatsButtonCustomID:
		ctx.Show(constants.ReviewerStatsPageName, "")
	case constants.GuildOwnerMenuButtonCustomID:
		if !session.IsGuildOwner.Get(s) && !isAdmin {
			ctx.Error("You must be a guild owner to access these tools.")
			return
		}

		// Set guild ID and name in session
		guildID := uint64(*event.GuildID())
		session.GuildStatsID.Set(s, guildID)

		if guild, err := event.Client().Rest().GetGuild(*event.GuildID(), false); err == nil {
			session.GuildStatsName.Set(s, guild.Name)
		}

		ctx.Show(constants.GuildOwnerPageName, "")
	}
}

// handleLookupRobloxUser opens a modal for entering a specific Roblox user ID to lookup.
func (m *Menu) handleLookupRobloxUser(ctx *interaction.Context) {
	modal := discord.NewModalCreateBuilder().
		SetCustomID(constants.LookupRobloxUserModalCustomID).
		SetTitle("Lookup Roblox User").
		AddActionRow(
			discord.NewTextInput(constants.LookupRobloxUserInputCustomID, discord.TextInputStyleShort, "User ID or UUID").
				WithRequired(true).
				WithPlaceholder("Enter the user ID or UUID to lookup..."),
		)

	ctx.Modal(modal)
}

// handleLookupRobloxGroup opens a modal for entering a specific Roblox group ID to lookup.
func (m *Menu) handleLookupRobloxGroup(ctx *interaction.Context) {
	modal := discord.NewModalCreateBuilder().
		SetCustomID(constants.LookupRobloxGroupModalCustomID).
		SetTitle("Lookup Roblox Group").
		AddActionRow(
			discord.NewTextInput(constants.LookupRobloxGroupInputCustomID, discord.TextInputStyleShort, "Group ID or UUID").
				WithRequired(true).
				WithPlaceholder("Enter the group ID or UUID to lookup..."),
		)

	ctx.Modal(modal)
}

// handleLookupDiscordUser opens a modal for entering a specific Discord user ID to lookup.
func (m *Menu) handleLookupDiscordUser(ctx *interaction.Context) {
	modal := discord.NewModalCreateBuilder().
		SetCustomID(constants.LookupDiscordUserModalCustomID).
		SetTitle("Lookup Discord User").
		AddActionRow(
			discord.NewTextInput(constants.LookupDiscordUserInputCustomID, discord.TextInputStyleShort, "Discord User ID").
				WithRequired(true).
				WithPlaceholder("Enter the Discord user ID to lookup..."),
		)

	ctx.Modal(modal)
}

// handleModal processes modal submissions.
func (m *Menu) handleModal(ctx *interaction.Context, s *session.Session) {
	switch ctx.Event().CustomID() {
	case constants.LookupRobloxUserModalCustomID:
		m.handleLookupRobloxUserModalSubmit(ctx, s)
	case constants.LookupRobloxGroupModalCustomID:
		m.handleLookupRobloxGroupModalSubmit(ctx, s)
	case constants.LookupDiscordUserModalCustomID:
		m.handleLookupDiscordUserModalSubmit(ctx, s)
	}
}

// handleLookupRobloxUserModalSubmit processes the user ID input and opens the review menu.
func (m *Menu) handleLookupRobloxUserModalSubmit(ctx *interaction.Context, s *session.Session) {
	// Get the user ID input
	userIDStr := ctx.Event().ModalData().Text(constants.LookupRobloxUserInputCustomID)

	// Parse profile URL if provided
	parsedURL, err := utils.ExtractUserIDFromURL(userIDStr)
	if err == nil {
		userIDStr = parsedURL
	}

	// Get user from database
	user, err := m.layout.db.Service().User().GetUserByID(ctx.Context(), userIDStr, types.UserFieldAll)
	if err != nil {
		switch {
		case errors.Is(err, types.ErrUserNotFound):
			ctx.Cancel("Failed to find user. They may not be in our database.")
		case errors.Is(err, types.ErrInvalidUserID):
			ctx.Cancel("Please provide a valid user ID or UUID.")
		default:
			m.layout.logger.Error("Failed to fetch user", zap.Error(err))
			ctx.Error("Failed to fetch user for review. Please try again.")
		}
		return
	}

	// Store user in session and show review menu
	session.UserTarget.Set(s, user)
	ctx.Show(constants.UserReviewPageName, "")

	// Log the lookup action
	m.layout.db.Model().Activity().Log(ctx.Context(), &types.ActivityLog{
		ActivityTarget: types.ActivityTarget{
			UserID: user.ID,
		},
		ReviewerID:        uint64(ctx.Event().User().ID),
		ActivityType:      enum.ActivityTypeUserLookup,
		ActivityTimestamp: time.Now(),
		Details:           map[string]any{},
	})
}

// handleLookupRobloxGroupModalSubmit processes the group ID input and opens the review menu.
func (m *Menu) handleLookupRobloxGroupModalSubmit(ctx *interaction.Context, s *session.Session) {
	// Get the group ID input
	groupIDStr := ctx.Event().ModalData().Text(constants.LookupRobloxGroupInputCustomID)

	// Parse group URL if provided
	if utils.IsRobloxGroupURL(groupIDStr) {
		var err error
		groupIDStr, err = utils.ExtractGroupIDFromURL(groupIDStr)
		if err != nil {
			ctx.Cancel("Invalid Roblox group URL. Please provide a valid URL or ID.")
			return
		}
	}

	// Get group from database
	group, err := m.layout.db.Service().Group().GetGroupByID(ctx.Context(), groupIDStr, types.GroupFieldAll)
	if err != nil {
		switch {
		case errors.Is(err, types.ErrGroupNotFound):
			ctx.Cancel("Failed to find group. It may not be in our database.")
		case errors.Is(err, types.ErrInvalidGroupID):
			ctx.Cancel("Please provide a valid group ID or UUID.")
		default:
			m.layout.logger.Error("Failed to fetch group", zap.Error(err))
			ctx.Error("Failed to fetch group for review. Please try again.")
		}
		return
	}

	// Store group in session and show review menu
	session.GroupTarget.Set(s, group)
	ctx.Show(constants.GroupReviewPageName, "")

	// Log the lookup action
	m.layout.db.Model().Activity().Log(ctx.Context(), &types.ActivityLog{
		ActivityTarget: types.ActivityTarget{
			GroupID: group.ID,
		},
		ReviewerID:        uint64(ctx.Event().User().ID),
		ActivityType:      enum.ActivityTypeGroupLookup,
		ActivityTimestamp: time.Now(),
		Details:           map[string]any{},
	})
}

// handleLookupDiscordUserModalSubmit processes the Discord user ID input and shows the user's flagged servers.
func (m *Menu) handleLookupDiscordUserModalSubmit(ctx *interaction.Context, s *session.Session) {
	// Get the Discord user ID input
	discordUserIDStr := ctx.Event().ModalData().Text(constants.LookupDiscordUserInputCustomID)

	// Parse the Discord user ID
	discordUserID, err := strconv.ParseUint(discordUserIDStr, 10, 64)
	if err != nil {
		ctx.Cancel("Please provide a valid Discord user ID.")
		return
	}

	// Store the Discord user ID in session
	session.DiscordUserLookupID.Set(s, discordUserID)

	// Show the guild lookup page
	ctx.Show(constants.GuildLookupPageName, "")

	// Log the lookup action
	m.layout.db.Model().Activity().Log(ctx.Context(), &types.ActivityLog{
		ActivityTarget: types.ActivityTarget{
			UserID: discordUserID,
		},
		ReviewerID:        uint64(ctx.Event().User().ID),
		ActivityType:      enum.ActivityTypeUserLookupDiscord,
		ActivityTimestamp: time.Now(),
		Details:           map[string]any{},
	})
}

// handleButton processes button interactions, mainly handling refresh requests
// to update the dashboard statistics.
func (m *Menu) handleButton(ctx *interaction.Context, s *session.Session, customID string) {
	if customID == constants.RefreshButtonCustomID {
		session.StatsIsRefreshed.Set(s, false)
		ctx.Reload("Refreshed dashboard.")
	}
}
