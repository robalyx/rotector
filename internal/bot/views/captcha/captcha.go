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
		imgBuffer: session.CaptchaImageBuffer.Get(s),
	}
}

// Build creates a Discord message with CAPTCHA information.
func (b *Builder) Build() *discord.MessageUpdateBuilder {
	builder := discord.NewMessageUpdateBuilder()

	// Create components for the main container
	var components []discord.ContainerSubComponent

	// Add text display
	components = append(components, discord.NewTextDisplay(
		"## 🔒 CAPTCHA Verification Required\nPlease solve this CAPTCHA to continue reviewing.\nThe CAPTCHA contains 6 digits."))

	// Add CAPTCHA image if available
	if b.imgBuffer != nil {
		builder.AddFiles(discord.NewFile("captcha.png", "", b.imgBuffer))

		components = append(components,
			discord.NewMediaGallery(discord.MediaGalleryItem{
				Media: discord.UnfurledMediaItem{
					URL: "attachment://captcha.png",
				},
			}),
		)
	}

	// Create main container with all components
	mainContainer := discord.NewContainer(components...).
		WithAccentColor(constants.DefaultContainerColor)

	builder.AddComponents(mainContainer)

	// Add interactive components
	builder.AddComponents(discord.NewActionRow(
		discord.NewSecondaryButton("◀️ Back", constants.BackButtonCustomID),
		discord.NewSecondaryButton("🔄 Refresh", constants.CaptchaRefreshButtonCustomID),
		discord.NewPrimaryButton("Enter Answer", constants.CaptchaAnswerButtonCustomID),
	))

	return builder
}
