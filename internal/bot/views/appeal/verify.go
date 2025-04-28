package appeal

import (
	"fmt"
	"strings"

	"github.com/disgoorg/disgo/discord"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/bot/utils"
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
	builder := discord.NewMessageUpdateBuilder()

	// Create main container
	var content strings.Builder
	content.WriteString("## Account Verification Required\n\n")
	content.WriteString("To verify that you own this account, please follow these steps:\n\n")
	content.WriteString(fmt.Sprintf("1. Go to your Roblox profile: [Click Here](https://www.roblox.com/users/%d/profile)\n", b.userID))
	content.WriteString("2. Click the pencil icon next to your description\n")
	content.WriteString(fmt.Sprintf("3. Set your description to exactly:\n```%s```\n", b.code))
	content.WriteString("4. Click the 'Verify' button below once done\n\n")
	content.WriteString("Note: You can change your description back after verification.")

	container := discord.NewContainer(
		discord.NewTextDisplay(content.String()),
		discord.NewLargeSeparator(),
		discord.NewActionRow(
			discord.NewPrimaryButton("✅ Verify", constants.VerifyDescriptionButtonID),
		),
	).WithAccentColor(utils.GetContainerColor(false))

	// Add container and back button
	builder.AddComponents(
		container,
		discord.NewActionRow(
			discord.NewSecondaryButton("◀️ Back", constants.BackButtonCustomID),
		),
	)

	return builder
}
