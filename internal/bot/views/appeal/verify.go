package appeal

import (
	"fmt"

	"github.com/disgoorg/disgo/discord"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/session"
)

// VerifyBuilder creates the visual layout for the verification interface.
type VerifyBuilder struct {
	userID uint64
	code   string
}

// NewVerifyBuilder creates a new verification builder.
func NewVerifyBuilder(s *session.Session) *VerifyBuilder {
	return &VerifyBuilder{
		userID: session.VerifyUserID.Get(s),
		code:   session.VerifyCode.Get(s),
	}
}

// Build creates a Discord message showing the verification instructions.
func (b *VerifyBuilder) Build() *discord.MessageUpdateBuilder {
	embed := b.buildEmbed()
	components := b.buildComponents()

	return discord.NewMessageUpdateBuilder().
		SetEmbeds(embed.Build()).
		AddContainerComponents(components...)
}

// buildEmbed creates the main embed with verification instructions.
func (b *VerifyBuilder) buildEmbed() *discord.EmbedBuilder {
	return discord.NewEmbedBuilder().
		SetTitle("Account Verification Required").
		SetDescription(fmt.Sprintf(
			"To verify that you own this account, please follow these steps:\n\n"+
				"1. Go to your Roblox profile: [Click Here](https://www.roblox.com/users/%d/profile)\n"+
				"2. Click the pencil icon next to your description\n"+
				"3. Set your description to exactly:\n```%s```\n"+
				"4. Click the 'Verify' button below once done\n\n"+
				"Note: You can change your description back after verification.",
			b.userID, b.code)).
		SetColor(constants.DefaultEmbedColor)
}

// buildComponents creates the interactive components.
func (b *VerifyBuilder) buildComponents() []discord.ContainerComponent {
	return []discord.ContainerComponent{
		discord.NewActionRow(
			discord.NewSecondaryButton("◀️", constants.BackButtonCustomID),
			discord.NewPrimaryButton("✅ Verify", constants.VerifyDescriptionButtonID),
		),
	}
}
