package guild

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/disgoorg/disgo/discord"
	builder "github.com/robalyx/rotector/internal/bot/builder/guild"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/interaction"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/common/storage/database/types"
	"go.uber.org/zap"
)

// LogsMenu handles the display of guild ban operation logs.
type LogsMenu struct {
	layout *Layout
	page   *interaction.Page
}

// NewLogsMenu creates a new logs menu for viewing guild ban operations.
func NewLogsMenu(layout *Layout) *LogsMenu {
	m := &LogsMenu{layout: layout}
	m.page = &interaction.Page{
		Name: constants.GuildLogsPageName,
		Message: func(s *session.Session) *discord.MessageUpdateBuilder {
			return builder.NewLogsBuilder(s).Build()
		},
		ShowHandlerFunc:   m.Show,
		ButtonHandlerFunc: m.handleButton,
		SelectHandlerFunc: m.handleSelectMenu,
	}
	return m
}

// Show prepares and displays the guild ban logs interface.
func (m *LogsMenu) Show(ctx *interaction.Context, s *session.Session) {
	// Get guild ID from session
	guildID := session.GuildStatsID.Get(s)
	if guildID == 0 {
		ctx.Error("Invalid guild ID.")
		return
	}

	// Get cursor from session if it exists
	cursor := session.GuildBanLogCursor.Get(s)

	// Fetch filtered logs from database
	logs, nextCursor, err := m.layout.db.Model().GuildBan().GetGuildBanLogs(
		ctx.Context(),
		guildID,
		cursor,
		constants.LogsPerPage,
	)
	if err != nil {
		m.layout.logger.Error("Failed to get guild ban logs", zap.Error(err))
		ctx.Error("Failed to retrieve ban log data. Please try again.")
		return
	}

	// Get previous cursors array
	prevCursors := session.GuildBanLogPrevCursors.Get(s)

	// Store results and cursor in session
	session.GuildBanLogs.Set(s, logs)
	session.GuildBanLogCursor.Set(s, cursor)
	session.GuildBanLogNextCursor.Set(s, nextCursor)
	session.PaginationHasNextPage.Set(s, nextCursor != nil)
	session.PaginationHasPrevPage.Set(s, len(prevCursors) > 0)
}

// handleButton processes button interactions.
func (m *LogsMenu) handleButton(ctx *interaction.Context, s *session.Session, customID string) {
	switch customID {
	case constants.BackButtonCustomID:
		ctx.NavigateBack("")
	case constants.RefreshButtonCustomID:
		// Reset logs and reload
		session.GuildBanLogCursor.Delete(s)
		session.GuildBanLogNextCursor.Delete(s)
		session.GuildBanLogPrevCursors.Delete(s)
		session.PaginationHasNextPage.Delete(s)
		session.PaginationHasPrevPage.Delete(s)
		ctx.Reload("")
	case string(session.ViewerFirstPage),
		string(session.ViewerPrevPage),
		string(session.ViewerNextPage),
		string(session.ViewerLastPage):
		m.handlePagination(ctx, s, session.ViewerAction(customID))
	}
}

// handlePagination processes page navigation for logs.
func (m *LogsMenu) handlePagination(ctx *interaction.Context, s *session.Session, action session.ViewerAction) {
	switch action {
	case session.ViewerNextPage:
		if session.PaginationHasNextPage.Get(s) {
			cursor := session.GuildBanLogCursor.Get(s)
			nextCursor := session.GuildBanLogNextCursor.Get(s)
			prevCursors := session.GuildBanLogPrevCursors.Get(s)

			session.GuildBanLogCursor.Set(s, nextCursor)
			session.GuildBanLogPrevCursors.Set(s, append(prevCursors, cursor))
			ctx.Reload("")
		}
	case session.ViewerPrevPage:
		prevCursors := session.GuildBanLogPrevCursors.Get(s)

		if len(prevCursors) > 0 {
			lastIdx := len(prevCursors) - 1
			session.GuildBanLogPrevCursors.Set(s, prevCursors[:lastIdx])
			session.GuildBanLogCursor.Set(s, prevCursors[lastIdx])
			ctx.Reload("")
		}
	case session.ViewerFirstPage:
		session.GuildBanLogCursor.Set(s, nil)
		session.GuildBanLogPrevCursors.Set(s, make([]*types.LogCursor, 0))
		ctx.Reload("")
	case session.ViewerLastPage:
		// Not currently supported
		return
	}
}

// handleSelectMenu processes select menu interactions.
func (m *LogsMenu) handleSelectMenu(ctx *interaction.Context, s *session.Session, customID, option string) {
	if customID != constants.GuildBanLogReportSelectMenuCustomID {
		return
	}

	// Parse log ID from option
	logID, err := strconv.ParseInt(option, 10, 64)
	if err != nil {
		ctx.Error("Invalid log ID selected.")
		return
	}

	// Get logs from session
	logs := session.GuildBanLogs.Get(s)
	if logs == nil {
		ctx.Error("No logs available.")
		return
	}

	// Find the selected log
	var selectedLog *types.GuildBanLog
	for _, log := range logs {
		if log.ID == logID {
			selectedLog = log
			break
		}
	}

	if selectedLog == nil {
		ctx.Error("Selected log not found.")
		return
	}

	// Get guild memberships for banned users
	userGuilds, err := m.layout.db.Model().Sync().GetFlaggedServerMembers(
		ctx.Context(),
		selectedLog.BannedUserIDs,
	)
	if err != nil {
		m.layout.logger.Error("Failed to get banned user guild memberships",
			zap.Error(err),
			zap.Int64("log_id", logID))
		ctx.Error("Failed to generate report. Please try again.")
		return
	}

	// Get unique server IDs
	serverIDSet := make(map[uint64]struct{})
	for _, guilds := range userGuilds {
		for _, guild := range guilds {
			serverIDSet[guild.ServerID] = struct{}{}
		}
	}

	// Get server names in a database query
	serverIDs := make([]uint64, 0, len(serverIDSet))
	for serverID := range serverIDSet {
		serverIDs = append(serverIDs, serverID)
	}

	serverInfo, err := m.layout.db.Model().Sync().GetServerInfo(ctx.Context(), serverIDs)
	if err != nil {
		m.layout.logger.Error("Failed to get server names",
			zap.Error(err),
			zap.Int64("log_id", logID))
		ctx.Error("Failed to generate report. Please try again.")
		return
	}

	// Create server name map
	serverNames := make(map[uint64]string)
	for _, info := range serverInfo {
		serverNames[info.ServerID] = info.Name
	}

	// Generate CSV content
	var csvContent strings.Builder
	csvContent.WriteString("User ID,Server Names\n")

	for userID, guilds := range userGuilds {
		var serverList []string
		for _, guild := range guilds {
			serverName := serverNames[guild.ServerID]
			if serverName == "" {
				serverName = constants.UnknownServer
			}
			// Escape quotes in server names for CSV
			serverList = append(serverList, strings.ReplaceAll(serverName, "\"", "\"\""))
		}

		// Write user ID and their server list
		csvContent.WriteString(fmt.Sprintf("%d,\"%s\"\n",
			userID,
			strings.Join(serverList, ", "),
		))
	}

	// Create filename with timestamp
	filename := fmt.Sprintf("ban_report_%s.csv",
		selectedLog.Timestamp.Format("2006-01-02_15-04-05"))

	// Send response with CSV file
	file := discord.NewFile(filename, "text/csv", strings.NewReader(csvContent.String()))
	ctx.RespondWithFiles(fmt.Sprintf("Attached CSV report for ban operation #%d", selectedLog.ID), file)
}
