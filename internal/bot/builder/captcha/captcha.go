package captcha

import (
	"bytes"

	"github.com/disgoorg/disgo/discord"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/session"
)

// Builder creates the visual layout for the CAPTCHA verification interface.
type Builder struct {
	imgBuffer *bytes.Buffer
}

// NewBuilder creates a new CAPTCHA builder.
func NewBuilder(s *session.Session) *Builder {
	return &Builder{
		imgBuffer: session.CaptchaImage.Get(s),
	}
}

// Build creates a Discord message with CAPTCHA information.
func (b *Builder) Build() *discord.MessageUpdateBuilder {
	embed := discord.NewEmbedBuilder().
		SetTitle("🔒 CAPTCHA Verification Required").
		SetDescription("Please solve this CAPTCHA to continue reviewing.\nThe CAPTCHA contains 6 digits.").
		SetColor(constants.DefaultEmbedColor)

	// Add CAPTCHA image if available
	if b.imgBuffer != nil {
		embed.SetImage("attachment://captcha.png")
	}

	builder := discord.NewMessageUpdateBuilder().
		SetEmbeds(embed.Build()).
		AddActionRow(
			discord.NewSecondaryButton("◀️ Back", constants.BackButtonCustomID),
			discord.NewSecondaryButton("🔄 Refresh", constants.CaptchaRefreshButtonCustomID),
			discord.NewPrimaryButton("Enter Answer", constants.CaptchaAnswerButtonCustomID),
		)

	// Attach CAPTCHA image if available
	if b.imgBuffer != nil {
		builder.SetFiles(discord.NewFile("captcha.png", "", b.imgBuffer))
	}

	return builder
}
