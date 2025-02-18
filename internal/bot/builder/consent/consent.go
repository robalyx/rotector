package consent

import (
	"github.com/disgoorg/disgo/discord"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/session"
)

// Builder creates the visual layout for the consent interface.
type Builder struct {
	userID uint64
}

// NewBuilder creates a new consent builder.
func NewBuilder(s *session.Session) *Builder {
	return &Builder{
		userID: s.UserID(),
	}
}

// Build creates a Discord message with terms of service in an embed.
func (b *Builder) Build() *discord.MessageUpdateBuilder {
	embed := discord.NewEmbedBuilder().
		SetTitle("Terms of Service").
		SetDescription(constants.TermsOfServiceText).
		SetColor(constants.DefaultEmbedColor).
		Build()

	return discord.NewMessageUpdateBuilder().
		SetEmbeds(embed).
		AddActionRow(
			discord.NewSuccessButton("Accept", constants.ConsentAcceptButtonCustomID),
			discord.NewDangerButton("Reject", constants.ConsentRejectButtonCustomID),
		)
}
