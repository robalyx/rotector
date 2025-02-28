package guild

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/disgo/rest"
	"github.com/disgoorg/snowflake/v2"
	builder "github.com/robalyx/rotector/internal/bot/builder/guild"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/pagination"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/bot/interfaces"
	"github.com/robalyx/rotector/internal/bot/utils"
	"github.com/robalyx/rotector/internal/common/storage/database/types"
	"github.com/robalyx/rotector/internal/common/storage/database/types/enum"
	"go.uber.org/zap"
)

// ScanMenu handles the scan results display and ban operations.
type ScanMenu struct {
	layout *Layout
	page   *pagination.Page
}

// NewScanMenu creates a new scan results menu.
func NewScanMenu(layout *Layout) *ScanMenu {
	m := &ScanMenu{layout: layout}
	m.page = &pagination.Page{
		Name: constants.GuildScanPageName,
		Message: func(s *session.Session) *discord.MessageUpdateBuilder {
			return builder.NewScanBuilder(s).Build()
		},
		ShowHandlerFunc:   m.Show,
		ButtonHandlerFunc: m.handleButton,
		SelectHandlerFunc: m.handleSelectMenu,
		ModalHandlerFunc:  m.handleModal,
	}
	return m
}

// Show prepares and displays the scan results interface.
func (m *ScanMenu) Show(event interfaces.CommonEvent, s *session.Session, r *pagination.Respond) {
	// Skip scanning if we already have results
	if session.GuildScanUserGuilds.Get(s) != nil {
		// Apply filters if we have results
		m.applyFilters(s)
		return
	}

	guildID := session.GuildStatsID.Get(s)
	if guildID == 0 {
		r.Error(event, "Invalid guild ID.")
		return
	}

	// Get all guild members
	var allMembers []discord.Member
	var after snowflake.ID
	limit := 1000 // Discord's max limit per request

	for {
		members, err := event.Client().Rest().GetMembers(snowflake.ID(guildID), limit, after)
		if err != nil {
			m.layout.logger.Error("Failed to get guild members",
				zap.Error(err),
				zap.Uint64("guild_id", guildID),
				zap.Uint64("after", uint64(after)))
			r.Error(event, "Failed to fetch guild members. Please try again.")
			return
		}

		allMembers = append(allMembers, members...)

		// Break if we got less than the limit (means we've reached the end)
		if len(members) < limit {
			break
		}

		// Set after to the last member's ID for next page
		after = members[len(members)-1].User.ID
	}

	// Extract member IDs
	memberIDs := make([]uint64, len(allMembers))
	for i, member := range allMembers {
		memberIDs[i] = uint64(member.User.ID)
	}

	// Get flagged members
	userGuilds, err := m.layout.db.Models().Sync().GetFlaggedServerMembers(context.Background(), memberIDs)
	if err != nil {
		m.layout.logger.Error("Failed to get flagged members",
			zap.Error(err),
			zap.Uint64("guild_id", guildID))
		r.Error(event, "Failed to check member statuses. Please try again.")
		return
	}

	// If no flagged users found, show message
	if len(userGuilds) == 0 {
		r.Cancel(event, s, "No flagged users found in this server.")
		return
	}

	// Get server names for all flagged users
	serverIDs := make([]uint64, 0)
	for _, guilds := range userGuilds {
		for _, guild := range guilds {
			serverIDs = append(serverIDs, guild.ServerID)
		}
	}

	// Get server info from database
	servers, err := m.layout.db.Models().Sync().GetServerInfo(context.Background(), serverIDs)
	if err != nil {
		m.layout.logger.Error("Failed to get server info",
			zap.Error(err),
			zap.Uint64s("server_ids", serverIDs))
		r.Error(event, "Failed to get server information. Please try again.")
		return
	}

	// Create map of server IDs to names
	serverNames := make(map[uint64]string)
	for _, server := range servers {
		serverNames[server.ServerID] = server.Name
		m.layout.logger.Info("Server info",
			zap.Uint64("server_id", server.ServerID),
			zap.String("server_name", server.Name))
	}

	// Store results in session
	session.GuildScanUserGuilds.Set(s, userGuilds)
	session.GuildScanGuildNames.Set(s, serverNames)
	session.PaginationPage.Set(s, 0)

	// Initialize or reset filters
	if session.GuildScanMinGuilds.Get(s) == 0 {
		session.GuildScanMinGuilds.Set(s, 1) // Default to minimum 1 guild
	}

	// Apply filters to get the filtered results
	m.applyFilters(s)

	m.layout.logger.Info("Guild scan completed",
		zap.Uint64("guild_id", guildID),
		zap.Int("total_members", len(allMembers)),
		zap.Int("flagged_users", len(userGuilds)))
}

// applyFilters applies the current filters to the scan results.
func (m *ScanMenu) applyFilters(s *session.Session) {
	userGuilds := session.GuildScanUserGuilds.Get(s)
	minGuilds := session.GuildScanMinGuilds.Get(s)
	minJoinDuration := session.GuildScanMinJoinDuration.Get(s)

	// Calculate the minimum join date based on duration
	var minJoinDate time.Time
	if minJoinDuration > 0 {
		minJoinDate = time.Now().Add(-minJoinDuration)
	}

	// Apply filters to each user
	filteredUsers := make(map[uint64][]*types.UserGuildInfo)
	for userID, guilds := range userGuilds {
		// Filter guilds by join duration
		var filteredGuilds []*types.UserGuildInfo

		for _, guild := range guilds {
			// If min join duration is set, check join date
			if minJoinDuration > 0 {
				// Only include guilds where the user joined before the min join date
				if guild.JoinedAt.Before(minJoinDate) {
					filteredGuilds = append(filteredGuilds, guild)
				}
			} else {
				// No duration filter, include all guilds
				filteredGuilds = append(filteredGuilds, guild)
			}
		}

		// Apply minimum guilds filter
		if len(filteredGuilds) >= minGuilds {
			filteredUsers[userID] = filteredGuilds
		}
	}

	// Update session with filtered results
	session.GuildScanFilteredUsers.Set(s, filteredUsers)
	session.PaginationTotalItems.Set(s, len(filteredUsers))

	// Count occurrences of each guild
	guildCounts := make(map[uint64]int)
	for _, guilds := range filteredUsers {
		for _, guild := range guilds {
			guildCounts[guild.ServerID]++
		}
	}

	// Convert to slice for sorting
	topGuilds := make([]*types.GuildCount, 0, len(guildCounts))
	for guildID, count := range guildCounts {
		topGuilds = append(topGuilds, &types.GuildCount{
			ServerID: guildID,
			Count:    count,
		})
	}

	// Sort by count in descending order
	sort.Slice(topGuilds, func(i, j int) bool {
		return topGuilds[i].Count > topGuilds[j].Count
	})

	// Store top guilds in session
	session.GuildScanTopGuilds.Set(s, topGuilds)
}

// handleButton processes button interactions.
func (m *ScanMenu) handleButton(event *events.ComponentInteractionCreate, s *session.Session, r *pagination.Respond, customID string) {
	switch customID {
	case constants.BackButtonCustomID:
		r.NavigateBack(event, s, "")
	case constants.ClearFiltersButtonCustomID:
		// Reset filters to default values
		session.GuildScanMinGuilds.Set(s, 1)
		session.GuildScanMinJoinDuration.Set(s, time.Duration(0))
		m.applyFilters(s)
		r.Reload(event, s, "Filters reset to default values.")
	case constants.ConfirmGuildBansButtonCustomID:
		m.handleConfirmBans(event, s, r)
	case string(session.ViewerFirstPage), string(session.ViewerPrevPage), string(session.ViewerNextPage), string(session.ViewerLastPage):
		// Handle pagination actions
		// Calculate max page and validate navigation action
		total := session.PaginationTotalItems.Get(s)
		maxPage := max((total+constants.GuildScanUsersPerPage-1)/constants.GuildScanUsersPerPage - 1)

		action := session.ViewerAction(customID)
		action.ParsePageAction(s, action, maxPage)
		r.Reload(event, s, "")
	}
}

// handleSelectMenu processes select menu interactions.
func (m *ScanMenu) handleSelectMenu(event *events.ComponentInteractionCreate, s *session.Session, _ *pagination.Respond, customID, option string) {
	if customID != constants.GuildScanFilterSelectMenuCustomID {
		return
	}

	switch option {
	case constants.GuildScanMinGuildsOption:
		m.showMinGuildsModal(event)
	case constants.GuildScanJoinDurationOption:
		m.showJoinDurationModal(event, s)
	}
}

// showMinGuildsModal displays a modal for entering the minimum guilds filter.
func (m *ScanMenu) showMinGuildsModal(event *events.ComponentInteractionCreate) {
	modal := discord.NewModalCreateBuilder().
		SetCustomID(constants.GuildScanMinGuildsModalCustomID).
		SetTitle("Set Minimum Guilds Filter").
		AddActionRow(
			discord.NewTextInput(constants.GuildScanMinGuildsInputCustomID, discord.TextInputStyleShort, "Minimum Guilds").
				WithPlaceholder("Enter minimum number of flagged guilds required").
				WithRequired(true).
				WithValue("1"),
		).
		Build()

	if err := event.Modal(modal); err != nil {
		m.layout.logger.Error("Failed to create min guilds modal",
			zap.Error(err))
	}
}

// showJoinDurationModal displays a modal for setting the minimum join duration filter.
func (m *ScanMenu) showJoinDurationModal(event *events.ComponentInteractionCreate, s *session.Session) {
	// Get current minimum join duration
	minJoinDuration := session.GuildScanMinJoinDuration.Get(s)

	// Create placeholder text based on current value
	placeholder := "Enter duration (e.g., 7d, 24h, 1d12h)"
	if minJoinDuration > 0 {
		placeholder = "Current: " + utils.FormatDuration(minJoinDuration)
	}

	// Create and show the modal
	modal := discord.NewModalCreateBuilder().
		SetCustomID(constants.GuildScanJoinDurationModalCustomID).
		SetTitle("Set Minimum Join Duration").
		AddActionRow(
			discord.TextInputComponent{
				CustomID:    constants.GuildScanJoinDurationInputCustomID,
				Style:       discord.TextInputStyleShort,
				Label:       "Duration (30m, 24h, 7d, etc.)",
				Placeholder: placeholder,
				Required:    false,
			},
		).
		Build()
	if err := event.Modal(modal); err != nil {
		m.layout.logger.Error("Failed to create join duration modal", zap.Error(err))
	}
}

// handleModal processes modal submissions.
func (m *ScanMenu) handleModal(event *events.ModalSubmitInteractionCreate, s *session.Session, r *pagination.Respond) {
	switch event.Data.CustomID {
	case constants.GuildScanMinGuildsModalCustomID:
		// Process minimum guilds filter modal
		minGuildsStr := event.Data.Text(constants.GuildScanMinGuildsInputCustomID)
		minGuilds, err := strconv.Atoi(minGuildsStr)
		if err != nil {
			r.Cancel(event, s, "Please enter a valid number greater than 0.")
			return
		}

		session.GuildScanMinGuilds.Set(s, max(1, minGuilds))
		session.PaginationPage.Set(s, 0) // Reset to first page when filter changes
		m.applyFilters(s)
		r.Reload(event, s, fmt.Sprintf("Filter set: Minimum %d flagged guild(s) required.", minGuilds))

	case constants.GuildScanJoinDurationModalCustomID:
		// Get the join duration value from the modal
		durationInput := event.Data.Text(constants.GuildScanJoinDurationInputCustomID)
		durationInput = strings.TrimSpace(durationInput)

		// Clear the filter if input is empty
		if durationInput == "" {
			session.GuildScanMinJoinDuration.Set(s, 0)
			session.PaginationPage.Set(s, 0) // Reset to first page when filter changes
			m.applyFilters(s)
			r.Reload(event, s, "Cleared minimum join duration filter.")
			return
		}

		// Parse the duration string (e.g., "30m", "24h", "7d", "1d12h")
		duration, err := utils.ParseCombinedDuration(durationInput)
		if err != nil || duration <= 0 {
			r.Cancel(event, s, "Invalid duration format. Please use formats like '30m' for 30 minutes, '24h' for 24 hours, '7d' for 7 days, or combined formats like '1d12h'.")
			return
		}

		// Update session and apply filters
		session.GuildScanMinJoinDuration.Set(s, duration)
		session.PaginationPage.Set(s, 0) // Reset to first page when filter changes
		m.applyFilters(s)
		r.Reload(event, s, fmt.Sprintf("Set minimum join duration to %s. Users must be in guilds for at least this long to be counted.", utils.FormatDuration(duration)))

	case constants.GuildBanConfirmModalCustomID:
		m.handleBanConfirmModal(event, s, r)
	}
}

// handleConfirmBans processes the ban confirmation.
func (m *ScanMenu) handleConfirmBans(event *events.ComponentInteractionCreate, s *session.Session, r *pagination.Respond) {
	filteredUsers := session.GuildScanFilteredUsers.Get(s)
	if len(filteredUsers) == 0 {
		r.Error(event, "No users to ban after applying filters.")
		return
	}

	guildID := session.GuildStatsID.Get(s)
	if guildID == 0 {
		r.Error(event, "Invalid guild ID.")
		return
	}

	// Create ban modal for confirmation
	modal := discord.NewModalCreateBuilder().
		SetCustomID(constants.GuildBanConfirmModalCustomID).
		SetTitle("Confirm Mass Ban").
		AddActionRow(
			discord.NewTextInput(constants.GuildBanReasonInputCustomID, discord.TextInputStyleParagraph, "Ban Reason").
				WithRequired(true).
				WithPlaceholder("Enter the reason for banning these users...").
				WithValue("Rotector: Banned for being in inappropriate guilds"),
		).
		Build()

	if err := event.Modal(modal); err != nil {
		m.layout.logger.Error("Failed to create ban confirmation modal",
			zap.Error(err),
			zap.Uint64("guild_id", guildID))
		r.Error(event, "Failed to open confirmation dialog. Please try again.")
		return
	}
}

// handleBanConfirmModal processes the ban confirmation modal submission.
func (m *ScanMenu) handleBanConfirmModal(event *events.ModalSubmitInteractionCreate, s *session.Session, r *pagination.Respond) {
	if event.Data.CustomID != constants.GuildBanConfirmModalCustomID {
		return
	}

	// Double-check if user has Administrator permissions
	if event.Member() == nil || !event.Member().Permissions.Has(discord.PermissionAdministrator) {
		r.Error(event, "You need Administrator permissions to perform mass bans.")
		return
	}

	// Double-check if bot has ban permissions
	if event.AppPermissions() == nil || !event.AppPermissions().Has(discord.PermissionBanMembers) {
		r.Error(event, "The bot doesn't have the necessary permissions to ban members.")
		return
	}

	// Get guild ID from session
	guildID := session.GuildStatsID.Get(s)
	if guildID == 0 {
		r.Error(event, "Invalid guild ID.")
		return
	}

	// Get filtered users to ban from session
	filteredUsers := session.GuildScanFilteredUsers.Get(s)
	if len(filteredUsers) == 0 {
		r.Error(event, "No users to ban after applying filters.")
		return
	}

	// Get ban reason from modal input
	banReason := event.Data.Text(constants.GuildBanReasonInputCustomID)

	// Create list of unique user IDs to ban
	userIDs := make([]snowflake.ID, 0, len(filteredUsers))
	for userID := range filteredUsers {
		userIDs = append(userIDs, snowflake.ID(userID))
	}

	// Define batch size for banning users
	const batchSize = 200 // Discord's max batch size
	totalBanned := 0
	totalFailed := 0

	// Acknowledge the interaction before the potentially long operation
	r.Clear(event, "Processing bans... This may take a moment.")

	// Process bans in batches
	for i := 0; i < len(userIDs); i += batchSize {
		end := min(i+batchSize, len(userIDs))
		batchUserIDs := userIDs[i:end]

		bulkBan := discord.BulkBan{
			UserIDs:              batchUserIDs,
			DeleteMessageSeconds: 0,
		}

		// Execute batch ban
		result, err := event.Client().Rest().BulkBan(snowflake.ID(guildID), bulkBan, rest.WithReason(banReason))
		if err != nil {
			m.layout.logger.Error("Failed to execute bulk ban batch",
				zap.Error(err),
				zap.Uint64("guild_id", guildID),
				zap.Int("batch_start", i),
				zap.Int("batch_end", end))
			// Continue with next batch even if this one failed
			continue
		}

		totalBanned += len(result.BannedUsers)
		totalFailed += len(result.FailedUsers)
	}

	// Log the ban actions
	m.layout.db.Models().Activities().Log(context.Background(), &types.ActivityLog{
		ActivityTarget: types.ActivityTarget{
			GuildID: guildID,
		},
		ReviewerID:        uint64(event.User().ID),
		ActivityType:      enum.ActivityTypeGuildBans,
		ActivityTimestamp: time.Now(),
		Details: map[string]interface{}{
			"reason":            banReason,
			"banned_count":      totalBanned,
			"failed_count":      totalFailed,
			"min_guilds_filter": session.GuildScanMinGuilds.Get(s),
		},
	})

	// Format response message
	msg := fmt.Sprintf("Successfully banned %d users.", totalBanned)
	if totalFailed > 0 {
		msg += fmt.Sprintf(" Failed to ban %d users - please check if the bot's role is higher than these users.", totalFailed)
	}

	// Reset session
	session.GuildScanUserGuilds.Delete(s)
	session.GuildScanGuildNames.Delete(s)
	session.GuildScanFilteredUsers.Delete(s)
	session.GuildScanMinGuilds.Delete(s)
	session.PaginationTotalItems.Delete(s)
	session.PaginationPage.Delete(s)

	r.Show(event, s, constants.GuildOwnerPageName, msg)
}
