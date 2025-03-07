package user

import (
	"fmt"
	"strconv"

	"github.com/disgoorg/disgo/discord"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/bot/utils"
	"github.com/robalyx/rotector/internal/common/storage/database/types"
	"github.com/robalyx/rotector/internal/common/storage/database/types/enum"
	"github.com/robalyx/rotector/internal/common/translator"
)

// CaesarBuilder creates the visual layout for Caesar cipher analysis.
type CaesarBuilder struct {
	user         *types.ReviewUser
	translator   *translator.Translator
	trainingMode bool
	privacyMode  bool
}

// NewCaesarBuilder creates a new Caesar cipher builder.
func NewCaesarBuilder(s *session.Session, translator *translator.Translator) *CaesarBuilder {
	trainingMode := session.UserReviewMode.Get(s) == enum.ReviewModeTraining
	return &CaesarBuilder{
		user:         session.UserTarget.Get(s),
		translator:   translator,
		trainingMode: trainingMode,
		privacyMode:  trainingMode || session.UserStreamerMode.Get(s),
	}
}

// Build creates a Discord message with all Caesar cipher translations.
func (b *CaesarBuilder) Build() *discord.MessageUpdateBuilder {
	// Get original description
	description := b.user.Description
	if description == "" {
		description = constants.NotApplicable
	}

	// Format original description
	formattedDescription := utils.FormatString(utils.TruncateString(description, 200))

	// Create embed for translations
	embed := discord.NewEmbedBuilder().
		SetTitle(constants.UserCaesarPageName).
		SetDescription(fmt.Sprintf(
			"Analyzing description for %s (%s)\n\n**Original Text:**\n%s",
			utils.CensorString(b.user.Name, b.privacyMode),
			utils.CensorString(strconv.FormatUint(b.user.ID, 10), b.privacyMode),
			formattedDescription,
		)).
		SetColor(utils.GetMessageEmbedColor(b.privacyMode))

	// Add all 25 possible shifts
	for shift := 1; shift <= 25; shift++ {
		translated := b.translator.TranslateCaesar(description, shift)
		formattedTranslation := utils.FormatString(utils.TruncateString(translated, 200))
		embed.AddField(
			fmt.Sprintf("Shift %d", shift),
			formattedTranslation,
			true,
		)
	}

	// Create message with navigation
	return discord.NewMessageUpdateBuilder().
		SetEmbeds(embed.Build()).
		AddActionRow(discord.NewSecondaryButton("◀️ Back", constants.BackButtonCustomID))
}
