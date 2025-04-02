package guild

import (
	"fmt"
	"strconv"

	"github.com/disgoorg/disgo/discord"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/bot/utils"
	"github.com/robalyx/rotector/internal/database/types"
)

// LogsBuilder creates the visual layout for the guild ban logs interface.
type LogsBuilder struct {
	logs        []*types.GuildBanLog
	hasNextPage bool
	hasPrevPage bool
	privacyMode bool
}

// NewLogsBuilder creates a new logs builder.
func NewLogsBuilder(s *session.Session) *LogsBuilder {
	return &LogsBuilder{
		logs:        session.GuildBanLogs.Get(s),
		hasNextPage: session.PaginationHasNextPage.Get(s),
		hasPrevPage: session.PaginationHasPrevPage.Get(s),
		privacyMode: session.UserStreamerMode.Get(s),
	}
}

// Build creates a Discord message showing the guild ban logs.
func (b *LogsBuilder) Build() *discord.MessageUpdateBuilder {
	return discord.NewMessageUpdateBuilder().
		SetEmbeds(b.buildEmbed().Build()).
		AddContainerComponents(b.buildComponents()...)
}

// buildEmbed creates the embed showing guild ban logs.
func (b *LogsBuilder) buildEmbed() *discord.EmbedBuilder {
	embed := discord.NewEmbedBuilder().
		SetTitle("Guild Ban Operations").
		SetDescription("View history of ban operations performed in this server.").
		SetColor(utils.GetMessageEmbedColor(b.privacyMode))

	// Add log entries with details
	if len(b.logs) > 0 {
		for _, log := range b.logs {
			// Create field content
			content := fmt.Sprintf("Timestamp: <t:%d:F>\nReason: `%s`\nBanned Users: `%d`\nFailed Users: `%d`",
				log.Timestamp.Unix(),
				log.Reason,
				log.BannedCount,
				log.FailedCount,
			)

			// Add reviewer info
			content += fmt.Sprintf("\nExecuted by: <@%d>", log.ReviewerID)

			// Add minimum guilds filter info if applicable
			if log.MinGuildsFilter > 1 {
				content += fmt.Sprintf("\nMinimum Guilds Filter: `%d`", log.MinGuildsFilter)
			}

			embed.AddField(
				fmt.Sprintf("Ban #%d", log.ID),
				content,
				false,
			)
		}

		// Add footer with pagination info
		if len(b.logs) > 0 {
			embed.SetFooterText(fmt.Sprintf("%d logs shown", len(b.logs)))
		}
	} else {
		embed.AddField("No Ban Operations Found", "No ban operations have been performed in this server yet.", false)
	}

	return embed
}

// buildComponents creates all interactive components for the logs viewer.
func (b *LogsBuilder) buildComponents() []discord.ContainerComponent {
	var components []discord.ContainerComponent

	// Add CSV report select menu if we have logs
	if len(b.logs) > 0 {
		var options []discord.StringSelectMenuOption
		for _, log := range b.logs {
			options = append(options, discord.NewStringSelectMenuOption(
				fmt.Sprintf("Ban #%d", log.ID),
				strconv.FormatInt(log.ID, 10),
			).WithDescription(fmt.Sprintf("Get CSV report for %d banned users", log.BannedCount)))
		}

		components = append(components, discord.NewActionRow(
			discord.NewStringSelectMenu(constants.GuildBanLogReportSelectMenuCustomID, "Get CSV Report", options...),
		))
	}

	// Add refresh button
	components = append(components, discord.NewActionRow(
		discord.NewSecondaryButton("Refresh Logs", constants.RefreshButtonCustomID),
	))

	// Add navigation buttons
	components = append(components, discord.NewActionRow(
		discord.NewSecondaryButton("◀️ Back", constants.BackButtonCustomID),
		discord.NewSecondaryButton("⏮️", string(session.ViewerFirstPage)).WithDisabled(!b.hasPrevPage),
		discord.NewSecondaryButton("◀️", string(session.ViewerPrevPage)).WithDisabled(!b.hasPrevPage),
		discord.NewSecondaryButton("▶️", string(session.ViewerNextPage)).WithDisabled(!b.hasNextPage),
		discord.NewSecondaryButton("⏭️", string(session.ViewerLastPage)).WithDisabled(true), // This is disabled on purpose
	))

	return components
}
