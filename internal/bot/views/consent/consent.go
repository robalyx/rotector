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
		userID: session.UserID.Get(s),
	}
}

// Build creates a Discord message with terms of service.
func (b *Builder) Build() *discord.MessageUpdateBuilder {
	// Create main container
	mainContainer := discord.NewContainer(
		discord.NewTextDisplay("## Terms of Service\n"+constants.TermsOfServiceText),
		discord.NewLargeSeparator(),
		discord.NewActionRow(
			discord.NewSuccessButton("Accept", constants.ConsentAcceptButtonCustomID),
			discord.NewDangerButton("Reject", constants.ConsentRejectButtonCustomID),
			discord.NewSecondaryButton("Appeals", constants.AppealMenuButtonCustomID),
		),
	).WithAccentColor(constants.DefaultContainerColor)

	return discord.NewMessageUpdateBuilder().
		AddComponents(mainContainer)
}
