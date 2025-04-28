package guild

import (
	"fmt"
	"strings"

	"github.com/disgoorg/disgo/discord"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/bot/utils"
	"github.com/robalyx/rotector/internal/database/types"
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
	// Create main container
	var content strings.Builder
	content.WriteString(fmt.Sprintf("## Message History - %s\n", b.guildName))
	content.WriteString(fmt.Sprintf("Showing inappropriate messages from <@%d> in this server.\n", b.userID))

	if len(b.messages) == 0 {
		content.WriteString("### No Messages Found\n")
		content.WriteString("No inappropriate messages found for this user in the current page.")
	} else {
		// Add message entries
		for i, msg := range b.messages {
			msgContent := utils.TruncateString(msg.Content, 100)
			msgContent = utils.FormatString(msgContent)

			content.WriteString(fmt.Sprintf("### Message %d - <t:%d:F>\n", i+1, msg.DetectedAt.Unix()))
			content.WriteString(fmt.Sprintf("Reason: `%s`\n", msg.Reason))
			content.WriteString(fmt.Sprintf("Confidence: `%.2f%%`\n", msg.Confidence*100))
			content.WriteString(msgContent + "\n")
		}

		// Add pagination info if available
		content.WriteString(fmt.Sprintf("\n-# Showing %d messages", len(b.messages)))
	}

	// Create container with pagination
	container := discord.NewContainer(
		discord.NewTextDisplay(content.String()),
	)

	// Add pagination buttons if we have pages
	if b.hasNextPage || b.hasPrevPage {
		container.AddComponents(
			discord.NewLargeSeparator(),
			discord.NewActionRow(
				discord.NewSecondaryButton("⏮️", string(session.ViewerFirstPage)).WithDisabled(!b.hasPrevPage),
				discord.NewSecondaryButton("◀️", string(session.ViewerPrevPage)).WithDisabled(!b.hasPrevPage),
				discord.NewSecondaryButton("▶️", string(session.ViewerNextPage)).WithDisabled(!b.hasNextPage),
				discord.NewSecondaryButton("⏭️", string(session.ViewerLastPage)).WithDisabled(true), // This is disabled on purpose
			),
		)
	}

	// Create message update builder
	return discord.NewMessageUpdateBuilder().
		AddComponents(container.WithAccentColor(constants.DefaultContainerColor)).
		AddComponents(discord.NewActionRow(
			discord.NewSecondaryButton("◀️ Back", constants.BackButtonCustomID),
		))
}
