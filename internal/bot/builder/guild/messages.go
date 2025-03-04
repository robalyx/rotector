package guild

import (
	"fmt"

	"github.com/disgoorg/disgo/discord"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/bot/utils"
	"github.com/robalyx/rotector/internal/common/storage/database/types"
)

// MessagesBuilder creates the visual layout for the message history interface.
type MessagesBuilder struct {
	userID      uint64
	username    string
	guildID     uint64
	guildName   string
	messages    []*types.InappropriateMessage
	hasNextPage bool
	hasPrevPage bool
}

// NewMessagesBuilder creates a new messages builder.
func NewMessagesBuilder(s *session.Session) *MessagesBuilder {
	guildID := session.DiscordUserMessageGuildID.Get(s)
	guildNames := session.DiscordUserGuildNames.Get(s)

	return &MessagesBuilder{
		userID:      session.DiscordUserLookupID.Get(s),
		username:    session.DiscordUserLookupName.Get(s),
		guildID:     guildID,
		guildName:   guildNames[guildID],
		messages:    session.DiscordUserMessages.Get(s),
		hasNextPage: session.PaginationHasNextPage.Get(s),
		hasPrevPage: session.PaginationHasPrevPage.Get(s),
	}
}

// Build creates a Discord message showing the user's message history.
func (b *MessagesBuilder) Build() *discord.MessageUpdateBuilder {
	// Create main embed
	embed := discord.NewEmbedBuilder().
		SetTitle("Message History - " + b.guildName).
		SetDescription(fmt.Sprintf("Showing inappropriate messages from <@%d> in this server.", b.userID))

	if len(b.messages) == 0 {
		embed.AddField("No Messages Found", "No inappropriate messages found for this user in the current page.", false)
	} else {
		// Add message entries
		for i, msg := range b.messages {
			content := utils.TruncateString(msg.Content, 100)
			content = utils.FormatString(content)

			embed.AddField(
				fmt.Sprintf("Message %d - <t:%d:F>", i+1, msg.DetectedAt.Unix()),
				fmt.Sprintf("Reason: `%s`\nConfidence: `%.2f%%`\n%s",
					msg.Reason,
					msg.Confidence*100,
					content,
				),
				false,
			)
		}

		// Add pagination info if available
		embed.SetFooterText(fmt.Sprintf("Showing %d messages", len(b.messages)))
	}

	embed.SetColor(constants.DefaultEmbedColor)

	// Create components
	components := []discord.ContainerComponent{
		// Navigation buttons
		discord.NewActionRow(
			discord.NewSecondaryButton("◀️ Back", constants.BackButtonCustomID),
			discord.NewSecondaryButton("⏮️", string(session.ViewerFirstPage)).WithDisabled(!b.hasPrevPage),
			discord.NewSecondaryButton("◀️", string(session.ViewerPrevPage)).WithDisabled(!b.hasPrevPage),
			discord.NewSecondaryButton("▶️", string(session.ViewerNextPage)).WithDisabled(!b.hasNextPage),
			discord.NewSecondaryButton("⏭️", string(session.ViewerLastPage)).WithDisabled(true), // This is disabled on purpose
		),
		// Refresh button
		discord.NewActionRow(
			discord.NewSecondaryButton("🔄 Refresh", constants.RefreshButtonCustomID),
		),
	}

	return discord.NewMessageUpdateBuilder().
		SetEmbeds(embed.Build()).
		AddContainerComponents(components...)
}
