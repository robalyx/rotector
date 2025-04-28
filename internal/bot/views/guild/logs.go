package guild

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/disgoorg/disgo/discord"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/bot/utils"
	"github.com/robalyx/rotector/internal/database/types"
)

// LogsBuilder creates the visual layout for the guild ban logs interface.
type LogsBuilder struct {
	logs           []*types.GuildBanLog
	hasNextPage    bool
	hasPrevPage    bool
	privacyMode    bool
	csvReportLogID int64
}

// NewLogsBuilder creates a new logs builder.
func NewLogsBuilder(s *session.Session) *LogsBuilder {
	return &LogsBuilder{
		logs:           session.GuildBanLogs.Get(s),
		hasNextPage:    session.PaginationHasNextPage.Get(s),
		hasPrevPage:    session.PaginationHasPrevPage.Get(s),
		privacyMode:    session.UserStreamerMode.Get(s),
		csvReportLogID: session.GuildBanLogCSVReport.Get(s),
	}
}

// Build creates a Discord message showing the guild ban logs.
func (b *LogsBuilder) Build() *discord.MessageUpdateBuilder {
	var containers []discord.LayoutComponent

	// Create header container
	var headerContent strings.Builder
	headerContent.WriteString("## Guild Ban Operations\n")
	headerContent.WriteString("View history of ban operations performed in this server.")

	containers = append(containers, discord.NewContainer(
		discord.NewTextDisplay(headerContent.String()),
	).WithAccentColor(utils.GetContainerColor(b.privacyMode)))

	// Create logs container if we have logs
	if len(b.logs) > 0 {
		var components []discord.ContainerSubComponent

		for _, log := range b.logs {
			// Create log entry section
			var logContent strings.Builder
			logContent.WriteString(fmt.Sprintf("### Ban #%d\n", log.ID))
			logContent.WriteString(fmt.Sprintf("Timestamp: <t:%d:F>\n", log.Timestamp.Unix()))
			logContent.WriteString(fmt.Sprintf("Reason: `%s`\n", log.Reason))
			logContent.WriteString(fmt.Sprintf("Banned Users: `%d`\n", log.BannedCount))
			logContent.WriteString(fmt.Sprintf("Failed Users: `%d`\n", log.FailedCount))
			logContent.WriteString(fmt.Sprintf("Executed by: <@%d>", log.ReviewerID))

			// Add minimum guilds filter info if applicable
			if log.MinGuildsFilter > 1 {
				logContent.WriteString(fmt.Sprintf("\nMinimum Guilds Filter: `%d`", log.MinGuildsFilter))
			}

			// Add minimum join duration if applicable
			if log.MinJoinDuration > 0 {
				logContent.WriteString(fmt.Sprintf("\nMinimum Join Duration: `%s`", utils.FormatDuration(log.MinJoinDuration)))
			}

			// Create section with CSV report button
			section := discord.NewSection(
				discord.NewTextDisplay(logContent.String()),
			).WithAccessory(
				discord.NewSecondaryButton("Get CSV Report", strconv.FormatInt(log.ID, 10)),
			)
			components = append(components, section)

			// Add file component if this log has a CSV report attached
			if b.csvReportLogID == log.ID {
				filename := fmt.Sprintf("ban_report_%s.csv", log.Timestamp.Format("2006-01-02_15-04-05"))
				components = append(components,
					discord.NewFileComponent("attachment://"+filename),
				)
			}

			// Add separator between logs
			if log.ID != b.logs[len(b.logs)-1].ID {
				components = append(components, discord.NewSmallSeparator())
			}
		}

		// Add footer with pagination info
		components = append(components,
			discord.NewLargeSeparator(),
			discord.NewTextDisplay(fmt.Sprintf("*%d logs shown*", len(b.logs))),
		)

		containers = append(containers, discord.NewContainer(components...).
			WithAccentColor(utils.GetContainerColor(b.privacyMode)))
	} else {
		// Show no logs message
		containers = append(containers, discord.NewContainer(
			discord.NewTextDisplay("### No Ban Operations Found\nNo ban operations have been performed in this server yet."),
		).WithAccentColor(utils.GetContainerColor(b.privacyMode)))
	}

	// Add interactive components
	containers = append(containers, b.buildInteractiveComponents()...)

	// Create message update builder
	return discord.NewMessageUpdateBuilder().AddComponents(containers...)
}

// buildInteractiveComponents creates all interactive components for the logs viewer.
func (b *LogsBuilder) buildInteractiveComponents() []discord.LayoutComponent {
	return []discord.LayoutComponent{
		// Add refresh button
		discord.NewActionRow(
			discord.NewSecondaryButton("Refresh Logs", constants.RefreshButtonCustomID),
		),
		// Add navigation buttons
		discord.NewActionRow(
			discord.NewSecondaryButton("◀️ Back", constants.BackButtonCustomID),
			discord.NewSecondaryButton("⏮️", string(session.ViewerFirstPage)).WithDisabled(!b.hasPrevPage),
			discord.NewSecondaryButton("◀️", string(session.ViewerPrevPage)).WithDisabled(!b.hasPrevPage),
			discord.NewSecondaryButton("▶️", string(session.ViewerNextPage)).WithDisabled(!b.hasNextPage),
			discord.NewSecondaryButton("⏭️", string(session.ViewerLastPage)).WithDisabled(true), // This is disabled on purpose
		),
	}
}
