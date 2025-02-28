package guild

import (
	"fmt"

	"github.com/disgoorg/disgo/discord"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/bot/utils"
	"github.com/robalyx/rotector/internal/common/storage/database/types"
)

// LogsBuilder creates the visual layout for the guild ban logs interface.
type LogsBuilder struct {
	logs        []*types.ActivityLog
	hasNextPage bool
	hasPrevPage bool
	privacyMode bool
}

// NewLogsBuilder creates a new logs builder.
func NewLogsBuilder(s *session.Session) *LogsBuilder {
	return &LogsBuilder{
		logs:        session.LogActivities.Get(s),
		hasNextPage: session.PaginationHasNextPage.Get(s),
		hasPrevPage: session.PaginationHasPrevPage.Get(s),
		privacyMode: session.UserStreamerMode.Get(s),
	}
}

// Build creates a Discord message showing the guild ban logs.
func (b *LogsBuilder) Build() *discord.MessageUpdateBuilder {
	// Create embed
	embed := discord.NewEmbedBuilder().
		SetTitle("Guild Ban Operations").
		SetDescription("View history of ban operations performed in this server.").
		SetColor(utils.GetMessageEmbedColor(b.privacyMode))

	// Add log entries with details
	if len(b.logs) > 0 {
		for _, log := range b.logs {
			reason := "No reason provided"
			bannedCount := 0
			failedCount := 0

			// Extract details from the log
			if r, ok := log.Details["reason"].(string); ok && r != "" {
				reason = r
			}
			if c, ok := log.Details["banned_count"].(float64); ok {
				bannedCount = int(c)
			}
			if c, ok := log.Details["failed_count"].(float64); ok {
				failedCount = int(c)
			}

			// Create field content
			content := fmt.Sprintf("**Ban Operation**\nReason: `%s`\nBanned Users: `%d`\nFailed Users: `%d`",
				reason,
				bannedCount,
				failedCount,
			)

			// Add reviewer info
			content += fmt.Sprintf("\nExecuted by: <@%d>", log.ReviewerID)

			embed.AddField(
				fmt.Sprintf("<t:%d:F>", log.ActivityTimestamp.Unix()),
				content,
				false,
			)
		}

		// Add footer with pagination info
		if len(b.logs) > 0 {
			embed.SetFooterText(fmt.Sprintf("Sequence %d | %d logs shown", b.logs[0].Sequence, len(b.logs)))
		}
	} else {
		embed.AddField("No Ban Operations Found", "No ban operations have been performed in this server yet.", false)
	}

	// Create components
	components := b.buildComponents()

	return discord.NewMessageUpdateBuilder().
		SetEmbeds(embed.Build()).
		AddContainerComponents(components...)
}

// buildComponents creates all interactive components for the logs viewer.
func (b *LogsBuilder) buildComponents() []discord.ContainerComponent {
	return []discord.ContainerComponent{
		// Refresh button
		discord.NewActionRow(
			discord.NewSecondaryButton("Refresh Logs", constants.RefreshButtonCustomID),
		),
		// Navigation buttons
		discord.NewActionRow(
			discord.NewSecondaryButton("◀️ Back", constants.BackButtonCustomID),
			discord.NewSecondaryButton("⏮️", string(session.ViewerFirstPage)).WithDisabled(!b.hasPrevPage),
			discord.NewSecondaryButton("◀️", string(session.ViewerPrevPage)).WithDisabled(!b.hasPrevPage),
			discord.NewSecondaryButton("▶️", string(session.ViewerNextPage)).WithDisabled(!b.hasNextPage),
			discord.NewSecondaryButton("⏭️", string(session.ViewerLastPage)).WithDisabled(true), // This is disabled on purpose
		),
	}
}
