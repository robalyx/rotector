package selector

import (
	"fmt"
	"strconv"
	"strings"

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
	var mainContent, sessionsContent strings.Builder

	// Create header
	mainContent.WriteString("## Session Manager\n")
	mainContent.WriteString("You have existing sessions. Please select whether to create a new session or continue an existing one.")

	// Add existing sessions
	if len(b.sessions) > 0 {
		sessionsContent.WriteString("### Existing Sessions\n")

		for _, s := range b.sessions {
			sessionsContent.WriteString(fmt.Sprintf("-# Session %d\n", s.MessageID))
			sessionsContent.WriteString(fmt.Sprintf("-# Page: %s\n", s.PageName))
			sessionsContent.WriteString(fmt.Sprintf("-# Last Used: <t:%d:R>\n\n", s.LastUsed.Unix()))
		}
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

	// Create container
	container := discord.NewContainer(
		discord.NewTextDisplay(mainContent.String()),
	).WithAccentColor(constants.DefaultContainerColor)

	// Add sessions section if we have any
	if len(b.sessions) > 0 {
		container = container.AddComponents(
			discord.NewLargeSeparator(),
			discord.NewTextDisplay(sessionsContent.String()),
		)
	}

	// Add action row separator and menu
	container = container.AddComponents(
		discord.NewLargeSeparator(),
		discord.NewActionRow(
			discord.NewStringSelectMenu(constants.SelectorSelectMenuCustomID, "Select an action", options...),
		),
	)

	return discord.NewMessageUpdateBuilder().
		AddComponents(container)
}
