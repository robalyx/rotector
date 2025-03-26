package selector

import (
	"fmt"
	"strconv"

	"github.com/disgoorg/disgo/discord"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/session"
)

// Builder creates the visual layout for the selector menu.
type Builder struct {
	sessions []session.Info
}

// NewBuilder creates a new selector builder.
func NewBuilder(s *session.Session) *Builder {
	return &Builder{
		sessions: session.ExistingSessions.Get(s),
	}
}

// Build creates a Discord message showing the selector menu.
func (b *Builder) Build() *discord.MessageUpdateBuilder {
	// Create embed
	embed := discord.NewEmbedBuilder().
		SetTitle("Session Manager").
		SetDescription("You have existing sessions. Please select whether to create a new session or continue an existing one.").
		SetColor(constants.DefaultEmbedColor)

	// Add existing sessions to embed
	for _, s := range b.sessions {
		embed.AddField(
			fmt.Sprintf("Session %d", s.MessageID),
			fmt.Sprintf("Page: %s\nLast Used: <t:%d:R>", s.PageName, s.LastUsed.Unix()),
			false,
		)
	}

	// Create select menu options
	options := []discord.StringSelectMenuOption{
		discord.NewStringSelectMenuOption("Create New Session", constants.SelectorNewButtonCustomID).
			WithEmoji(discord.ComponentEmoji{Name: "➕"}).
			WithDescription("Start a fresh session"),
	}

	// Add existing sessions as options
	for _, s := range b.sessions {
		options = append(options,
			discord.NewStringSelectMenuOption(
				fmt.Sprintf("Continue Session %d", s.MessageID),
				strconv.FormatUint(s.MessageID, 10),
			).
				WithEmoji(discord.ComponentEmoji{Name: "📝"}).
				WithDescription("Resume from "+s.PageName),
		)
	}

	return discord.NewMessageUpdateBuilder().
		SetEmbeds(embed.Build()).
		AddActionRow(
			discord.NewStringSelectMenu(constants.SelectorSelectMenuCustomID, "Select an action", options...),
		)
}
