package guild

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/rest"
	"github.com/disgoorg/snowflake/v2"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/interaction"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/bot/utils"
	view "github.com/robalyx/rotector/internal/bot/views/guild"
	"github.com/robalyx/rotector/internal/database/types"
	"go.uber.org/zap"
)

// ScanMenu handles the scan results display and ban operations.
type ScanMenu struct {
	layout *Layout
	page   *interaction.Page
}

// NewScanMenu creates a new scan results menu.
func NewScanMenu(layout *Layout) *ScanMenu {
	m := &ScanMenu{layout: layout}
	m.page = &interaction.Page{
		Name: constants.GuildScanPageName,
		Message: func(s *session.Session) *discord.MessageUpdateBuilder {
			return view.NewScanBuilder(s).Build()
		},
		ShowHandlerFunc:   m.Show,
		ButtonHandlerFunc: m.handleButton,
		SelectHandlerFunc: m.handleSelectMenu,
		ModalHandlerFunc:  m.handleModal,
	}
	return m
}

// Show prepares and displays the guild scan interface.
func (m *ScanMenu) Show(ctx *interaction.Context, s *session.Session) {
	// Skip scanning if we already have results
	if session.GuildScanUserGuilds.Get(s) != nil {
		// Apply filters if we have results
		m.applyFilters(s)
		return
	}

	// Get guild ID from session
	guildID := session.GuildStatsID.Get(s)
	if guildID == 0 {
		ctx.Error("Invalid guild ID.")
		return
	}

	// Get scan type from session
	scanType := session.GuildScanType.Get(s)

	// Get all guild members using Discord API
	var members []discord.Member
	var after snowflake.ID

	for {
		chunk, err := ctx.Event().Client().Rest().GetMembers(snowflake.ID(guildID), 1000, after)
		if err != nil {
			m.layout.logger.Error("Failed to get guild members",
				zap.Error(err),
				zap.Uint64("guild_id", guildID))
			ctx.Error("Failed to get guild members. Please try again.")
			return
		}

		members = append(members, chunk...)

		// Check if we got less than 1000 members (last page)
		if len(chunk) < 1000 {
			break
		}

		// Update after for next page
		after = chunk[len(chunk)-1].User.ID
	}

	// Extract member IDs
	memberIDs := make([]uint64, len(members))
	for i, member := range members {
		memberIDs[i] = uint64(member.User.ID)
	}

	switch scanType {
	case constants.GuildScanTypeCondo:
		// Handle condo server scan
		if err := m.handleCondoScan(ctx.Context(), s, memberIDs); err != nil {
			m.layout.logger.Error("Failed to handle condo scan",
				zap.Error(err),
				zap.Uint64("guild_id", guildID))
			ctx.Error("Failed to scan guild members. Please try again.")
		}

	case constants.GuildScanTypeMessages:
		// Handle message scan
		if err := m.handleMessageScan(ctx.Context(), s, memberIDs); err != nil {
			m.layout.logger.Error("Failed to handle message scan",
				zap.Error(err),
				zap.Uint64("guild_id", guildID))
			ctx.Error("Failed to scan messages. Please try again.")
		}

	default:
		ctx.Error("Invalid scan type.")
	}
}

// handleCondoScan processes the condo server scan.
func (m *ScanMenu) handleCondoScan(ctx context.Context, s *session.Session, memberIDs []uint64) error {
	// Get flagged server memberships for these users
	flaggedMembers, err := m.layout.db.Model().Sync().GetFlaggedServerMembers(ctx, memberIDs)
	if err != nil {
		return fmt.Errorf("failed to get flagged members: %w", err)
	}

	// Get server names for all flagged servers
	var serverIDs []uint64
	for _, guilds := range flaggedMembers {
		for _, guild := range guilds {
			serverIDs = append(serverIDs, guild.ServerID)
		}
	}

	// Get server info from database
	serverInfo, err := m.layout.db.Model().Sync().GetServerInfo(ctx, serverIDs)
	if err != nil {
		return fmt.Errorf("failed to get server info: %w", err)
	}

	// Convert server info to name map
	guildNames := make(map[uint64]string)
	for _, info := range serverInfo {
		guildNames[info.ServerID] = info.Name
	}

	// Store results in session
	session.GuildScanUserGuilds.Set(s, flaggedMembers)
	session.GuildScanGuildNames.Set(s, guildNames)
	session.PaginationTotalItems.Set(s, len(flaggedMembers))

	// Apply filters
	m.applyFilters(s)
	return nil
}

// handleMessageScan processes the message-based scan.
func (m *ScanMenu) handleMessageScan(ctx context.Context, s *session.Session, memberIDs []uint64) error {
	// Get message summaries for all members in the guild
	summaries, err := m.layout.db.Model().Message().GetUserMessageSummaries(ctx, memberIDs)
	if err != nil {
		return fmt.Errorf("failed to get message summaries: %w", err)
	}

	// Store results in session
	session.GuildScanMessageSummaries.Set(s, summaries)
	session.PaginationTotalItems.Set(s, len(summaries))

	// Apply filters
	m.applyFilters(s)
	return nil
}

// applyFilters applies the current filters to the scan results.
func (m *ScanMenu) applyFilters(s *session.Session) {
	scanType := session.GuildScanType.Get(s)
	minJoinDuration := session.GuildScanMinJoinDuration.Get(s)

	// Calculate the minimum join/detection date based on duration
	var minDate time.Time
	if minJoinDuration > 0 {
		minDate = time.Now().Add(-minJoinDuration)
	}

	// Apply filters to message summaries
	if scanType == constants.GuildScanTypeMessages {
		summaries := session.GuildScanMessageSummaries.Get(s)
		filteredSummaries := make(map[uint64]*types.InappropriateUserSummary)

		for userID, summary := range summaries {
			// If min duration is set, check last message date
			if minJoinDuration > 0 {
				if summary.LastDetected.Before(minDate) {
					filteredSummaries[userID] = summary
				}
			} else {
				filteredSummaries[userID] = summary
			}
		}

		// Update session with filtered results
		session.GuildScanFilteredSummaries.Set(s, filteredSummaries)
		session.PaginationTotalItems.Set(s, len(filteredSummaries))
		return
	}

	// Apply filters to guild memberships
	userGuilds := session.GuildScanUserGuilds.Get(s)
	minGuilds := session.GuildScanMinGuilds.Get(s)

	// Apply filters to each user
	filteredUsers := make(map[uint64][]*types.UserGuildInfo)
	for userID, guilds := range userGuilds {
		// Filter guilds by join duration
		var filteredGuilds []*types.UserGuildInfo

		for _, guild := range guilds {
			// If min join duration is set, check join date
			if minJoinDuration > 0 {
				// Only include guilds where the user joined before the min date
				if guild.JoinedAt.Before(minDate) {
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
}

// handleButton processes button interactions.
func (m *ScanMenu) handleButton(ctx *interaction.Context, s *session.Session, customID string) {
	action := session.ViewerAction(customID)
	switch action {
	case session.ViewerFirstPage, session.ViewerPrevPage, session.ViewerNextPage, session.ViewerLastPage:
		total := session.PaginationTotalItems.Get(s)
		maxPage := max((total+constants.GuildScanUsersPerPage-1)/constants.GuildScanUsersPerPage - 1)

		action := session.ViewerAction(customID)
		action.ParsePageAction(s, maxPage)
		ctx.Reload("")
		return
	}

	switch customID {
	case constants.BackButtonCustomID:
		ctx.NavigateBack("")
	case constants.ClearFiltersButtonCustomID:
		// Reset filters to default values
		session.GuildScanMinGuilds.Set(s, 1)
		session.GuildScanMinJoinDuration.Set(s, time.Duration(0))
		m.applyFilters(s)
		ctx.Reload("Filters reset to default values.")
	case constants.ConfirmGuildBansButtonCustomID:
		m.handleConfirmBans(ctx, s)
	}
}

// handleSelectMenu processes select menu interactions.
func (m *ScanMenu) handleSelectMenu(ctx *interaction.Context, s *session.Session, customID, option string) {
	if customID != constants.GuildScanFilterSelectMenuCustomID {
		return
	}

	switch option {
	case constants.GuildScanMinGuildsOption:
		m.showMinGuildsModal(ctx)
	case constants.GuildScanJoinDurationOption:
		m.showJoinDurationModal(ctx, s)
	}
}

// showMinGuildsModal displays a modal for entering the minimum guilds filter.
func (m *ScanMenu) showMinGuildsModal(ctx *interaction.Context) {
	modal := discord.NewModalCreateBuilder().
		SetCustomID(constants.GuildScanMinGuildsModalCustomID).
		SetTitle("Set Minimum Guilds Filter").
		AddActionRow(
			discord.NewTextInput(constants.GuildScanMinGuildsInputCustomID, discord.TextInputStyleShort, "Minimum Guilds").
				WithPlaceholder("Enter minimum number of flagged guilds required").
				WithRequired(true).
				WithValue("1"),
		)

	ctx.Modal(modal)
}

// showJoinDurationModal displays a modal for setting the minimum join duration filter.
func (m *ScanMenu) showJoinDurationModal(ctx *interaction.Context, s *session.Session) {
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
		)

	ctx.Modal(modal)
}

// handleModal processes modal submissions.
func (m *ScanMenu) handleModal(ctx *interaction.Context, s *session.Session) {
	switch ctx.Event().CustomID() {
	case constants.GuildScanMinGuildsModalCustomID:
		// Process minimum guilds filter modal
		minGuildsStr := ctx.Event().ModalData().Text(constants.GuildScanMinGuildsInputCustomID)
		minGuilds, err := strconv.Atoi(minGuildsStr)
		if err != nil {
			ctx.Cancel("Please enter a valid number greater than 0.")
			return
		}

		session.GuildScanMinGuilds.Set(s, max(1, minGuilds))
		session.PaginationPage.Set(s, 0) // Reset to first page when filter changes
		m.applyFilters(s)
		ctx.Reload(fmt.Sprintf("Filter set: Minimum %d flagged guild(s) required.", minGuilds))

	case constants.GuildScanJoinDurationModalCustomID:
		// Get the join duration value from the modal
		durationInput := ctx.Event().ModalData().Text(constants.GuildScanJoinDurationInputCustomID)
		durationInput = strings.TrimSpace(durationInput)

		// Clear the filter if input is empty
		if durationInput == "" {
			session.GuildScanMinJoinDuration.Set(s, 0)
			session.PaginationPage.Set(s, 0) // Reset to first page when filter changes
			m.applyFilters(s)
			ctx.Reload("Cleared minimum join duration filter.")
			return
		}

		// Parse the duration string (e.g., "30m", "24h", "7d", "1d12h")
		duration, err := utils.ParseCombinedDuration(durationInput)
		if err != nil || duration <= 0 {
			ctx.Cancel("Invalid duration format. Please use formats like '30m' for 30 minutes, " +
				"'24h' for 24 hours, '7d' for 7 days, or combined formats like '1d12h'.")
			return
		}

		// Update session and apply filters
		session.GuildScanMinJoinDuration.Set(s, duration)
		session.PaginationPage.Set(s, 0) // Reset to first page when filter changes
		m.applyFilters(s)
		ctx.Reload(fmt.Sprintf(
			"Set minimum join duration to %s. Users must be in guilds for at least this long to be counted.",
			utils.FormatDuration(duration),
		))

	case constants.GuildBanConfirmModalCustomID:
		m.handleBanConfirmModal(ctx, s)
	}
}

// handleConfirmBans processes the ban confirmation.
func (m *ScanMenu) handleConfirmBans(ctx *interaction.Context, s *session.Session) {
	filteredUsers := session.GuildScanFilteredUsers.Get(s)
	if len(filteredUsers) == 0 {
		ctx.Cancel("No users to ban after applying filters.")
		return
	}

	guildID := session.GuildStatsID.Get(s)
	if guildID == 0 {
		ctx.Error("Invalid guild ID.")
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
		)

	ctx.Modal(modal)
}

// handleBanConfirmModal processes the ban confirmation modal submission.
func (m *ScanMenu) handleBanConfirmModal(ctx *interaction.Context, s *session.Session) {
	if ctx.Event().CustomID() != constants.GuildBanConfirmModalCustomID {
		return
	}

	// Double-check if user has Administrator permissions
	if ctx.Event().Member() == nil || !ctx.Event().Member().Permissions.Has(discord.PermissionAdministrator) {
		ctx.Error("You need Administrator permissions to perform mass bans.")
		return
	}

	// Double-check if bot has ban permissions
	if ctx.Event().AppPermissions() == nil || !ctx.Event().AppPermissions().Has(discord.PermissionBanMembers) {
		ctx.Error("The bot doesn't have the necessary permissions to ban members.")
		return
	}

	// Get guild ID from session
	guildID := session.GuildStatsID.Get(s)
	if guildID == 0 {
		ctx.Error("Invalid guild ID.")
		return
	}

	// Get filtered users to ban from session
	filteredUsers := session.GuildScanFilteredUsers.Get(s)
	if len(filteredUsers) == 0 {
		ctx.Cancel("No users to ban after applying filters.")
		return
	}

	// Get ban reason from modal input
	banReason := ctx.Event().ModalData().Text(constants.GuildBanReasonInputCustomID)

	// Execute the bans
	totalBanned, totalFailed, bannedUserIDs := m.executeBans(ctx, guildID, filteredUsers, banReason)

	// Log the ban operation
	err := m.layout.db.Model().GuildBan().LogBanOperation(ctx.Context(), &types.GuildBanLog{
		GuildID:         guildID,
		ReviewerID:      uint64(ctx.Event().User().ID),
		BannedCount:     totalBanned,
		FailedCount:     totalFailed,
		BannedUserIDs:   bannedUserIDs,
		Reason:          banReason,
		MinGuildsFilter: session.GuildScanMinGuilds.Get(s),
		Timestamp:       time.Now(),
	})
	if err != nil {
		m.layout.logger.Error("Failed to log guild ban operation",
			zap.Error(err),
			zap.Uint64("guild_id", guildID))
	}

	// Format response message
	msg := fmt.Sprintf("Successfully banned %d users.", totalBanned)
	if totalFailed > 0 {
		msg += fmt.Sprintf(
			" Failed to ban %d users - please check if the bot's role is higher than these users.",
			totalFailed,
		)
	}

	// Reset session
	session.GuildScanUserGuilds.Delete(s)
	session.GuildScanGuildNames.Delete(s)
	session.GuildScanFilteredUsers.Delete(s)
	session.GuildScanMinGuilds.Delete(s)
	session.PaginationTotalItems.Delete(s)
	session.PaginationPage.Delete(s)

	ctx.Show(constants.GuildOwnerPageName, msg)
}

// executeBans performs the actual banning of users and sends them DM notifications.
func (m *ScanMenu) executeBans(
	ctx *interaction.Context, guildID uint64, filteredUsers map[uint64][]*types.UserGuildInfo, banReason string,
) (totalBanned, totalFailed int, bannedUserIDs []uint64) {
	// Create list of unique user IDs to ban
	userIDs := make([]snowflake.ID, 0, len(filteredUsers))
	for userID := range filteredUsers {
		userIDs = append(userIDs, snowflake.ID(userID))
	}

	bannedUserIDs = make([]uint64, 0, len(filteredUsers))

	// Define batch size for banning users
	const batchSize = 200 // Discord's max batch size

	// Process bans in batches
	for i := 0; i < len(userIDs); i += batchSize {
		end := min(i+batchSize, len(userIDs))
		batchUserIDs := userIDs[i:end]

		bulkBan := discord.BulkBan{
			UserIDs:              batchUserIDs,
			DeleteMessageSeconds: 0,
		}

		// Execute batch ban
		result, err := ctx.Event().Client().Rest().BulkBan(snowflake.ID(guildID), bulkBan, rest.WithReason(banReason))
		if err != nil {
			m.layout.logger.Error("Failed to execute bulk ban batch",
				zap.Error(err),
				zap.Uint64("guild_id", guildID),
				zap.Int("batch_start", i),
				zap.Int("batch_end", end))
			continue
		}

		totalBanned += len(result.BannedUsers)
		totalFailed += len(result.FailedUsers)

		// Add successfully banned users to the list
		for _, userID := range result.BannedUsers {
			bannedUserIDs = append(bannedUserIDs, uint64(userID))
		}

		// Send DM notifications to successfully banned users
		for _, userID := range result.BannedUsers {
			// Create DM channel
			channel, err := ctx.Event().Client().Rest().CreateDMChannel(userID)
			if err != nil {
				m.layout.logger.Error("Failed to create DM channel",
					zap.Error(err),
					zap.Uint64("user_id", uint64(userID)))
				continue
			}

			// Get guild information
			guild, err := ctx.Event().Client().Rest().GetGuild(snowflake.ID(guildID), false)
			if err != nil {
				m.layout.logger.Error("Failed to get guild information",
					zap.Error(err),
					zap.Uint64("guild_id", guildID))
				continue
			}

			// Send ban notification with detailed information
			embed := discord.NewEmbedBuilder().
				SetTitle("🚫 Server Ban Notice").
				SetDescription(fmt.Sprintf("You have been banned from **%s**", guild.Name)).
				AddField("Reason", utils.FormatString(banReason), false).
				AddField("Server ID", fmt.Sprintf("`%d`", guildID), true).
				AddField("Ban Date", fmt.Sprintf("<t:%d:F>", time.Now().Unix()), true).
				SetColor(constants.ErrorEmbedColor).
				SetFooter("This is an automated message. If you believe this ban was in error, "+
					"please use this bot to appeal.", "")

			// Add server icon if available
			if guild.Icon != nil {
				embed.SetThumbnail(*guild.Icon)
			}

			// Send the embed to the DM channel
			_, err = ctx.Event().Client().Rest().CreateMessage(channel.ID(), discord.NewMessageCreateBuilder().
				SetEmbeds(embed.Build()).
				Build())
			if err != nil {
				m.layout.logger.Error("Failed to send ban notification",
					zap.Error(err),
					zap.Uint64("user_id", uint64(userID)))
			}
		}
	}

	return totalBanned, totalFailed, bannedUserIDs
}
