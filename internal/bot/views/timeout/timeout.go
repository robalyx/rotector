package timeout

import (
	"fmt"
	"strings"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/session"
)

// Builder creates the visual layout for the timeout menu.
type Builder struct {
	nextReviewTime time.Time
}

// NewBuilder creates a new timeout menu builder.
func NewBuilder(s *session.Session) *Builder {
	return &Builder{
		nextReviewTime: session.UserReviewBreakNextReviewTime.Get(s),
	}
}

// Build creates a Discord message showing the timeout status.
func (b *Builder) Build() *discord.MessageUpdateBuilder {
	var content strings.Builder

	// Create header and description
	content.WriteString("## Time for a Break! 🌟\n\n")
	content.WriteString("Great work on your reviews! To ensure that you're taking care of yourself, we've scheduled a short break.\n\n")
	content.WriteString(fmt.Sprintf("You can resume reviewing <t:%d:R>\n", b.nextReviewTime.Unix()))

	// Add relaxation suggestions
	content.WriteString("### Suggested Activities\n")
	content.WriteString("- Take a short walk outside\n")
	content.WriteString("- Practice some light stretching\n")
	content.WriteString("- Grab a glass of water\n")
	content.WriteString("- Rest your eyes from the screen\n")
	content.WriteString("- Take some deep breaths\n\n")

	content.WriteString("Thank you for your dedication to keeping the community safe! 💪")

	// Create container with text display and back button
	container := discord.NewContainer(
		discord.NewTextDisplay(content.String()),
		discord.NewLargeSeparator(),
		discord.NewActionRow(
			discord.NewSecondaryButton("◀️ Back", constants.BackButtonCustomID),
		),
	).WithAccentColor(constants.DefaultContainerColor)

	return discord.NewMessageUpdateBuilder().
		AddComponents(container)
}
