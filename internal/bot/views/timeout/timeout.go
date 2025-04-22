package timeout

import (
	"fmt"
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
	timestampStr := fmt.Sprintf("<t:%d:R>", b.nextReviewTime.Unix())
	embed := discord.NewEmbedBuilder().
		SetTitle("Time for a Break! 🌟").
		SetDescription(fmt.Sprintf(
			"Great work on your reviews! To ensure that you're taking care of yourself, we've scheduled a short break.\n\n"+
				"You can resume reviewing %s\n\n"+
				"While you wait, here are some relaxing activities you can try:\n"+
				"- Take a short walk outside\n"+
				"- Practice some light stretching\n"+
				"- Grab a glass of water\n"+
				"- Rest your eyes from the screen\n"+
				"- Take some deep breaths\n\n"+
				"Thank you for your dedication to keeping the community safe! 💪",
			timestampStr,
		)).
		SetColor(constants.DefaultEmbedColor).
		Build()

	return discord.NewMessageUpdateBuilder().
		SetEmbeds(embed).
		AddActionRow(
			discord.NewSecondaryButton("◀️ Back", constants.BackButtonCustomID),
		)
}
