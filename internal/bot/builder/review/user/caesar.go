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
	page         int
	offset       int
	totalItems   int
	totalPages   int
	trainingMode bool
	privacyMode  bool
}

// NewCaesarBuilder creates a new Caesar cipher builder.
func NewCaesarBuilder(s *session.Session, translator *translator.Translator) *CaesarBuilder {
	trainingMode := session.UserReviewMode.Get(s) == enum.ReviewModeTraining
	return &CaesarBuilder{
		user:         session.UserTarget.Get(s),
		translator:   translator,
		page:         session.PaginationPage.Get(s),
		offset:       session.PaginationOffset.Get(s),
		totalItems:   session.PaginationTotalItems.Get(s),
		totalPages:   session.PaginationTotalPages.Get(s),
		trainingMode: trainingMode,
		privacyMode:  trainingMode || session.UserStreamerMode.Get(s),
	}
}

// Build creates a Discord message with Caesar cipher translations for the current page.
func (b *CaesarBuilder) Build() *discord.MessageUpdateBuilder {
	// Get original description
	description := b.user.Description
	if description == "" {
		description = constants.NotApplicable
	}

	// Format original description
	formattedDescription := utils.FormatString(utils.TruncateString(description, 600))

	// Create embed for translations
	embed := discord.NewEmbedBuilder().
		SetTitle(fmt.Sprintf("%s (Page %d/%d)", constants.UserCaesarPageName, b.page+1, b.totalPages)).
		SetDescription(fmt.Sprintf(
			"Analyzing description for %s (%s)\n\n**Original Text:**\n%s",
			utils.CensorString(b.user.Name, b.privacyMode),
			utils.CensorString(strconv.FormatUint(b.user.ID, 10), b.privacyMode),
			formattedDescription,
		)).
		SetColor(utils.GetMessageEmbedColor(b.privacyMode))

	// Calculate range for current page
	startShift := b.offset + 1
	endShift := min(startShift+constants.CaesarTranslationsPerPage-1, b.totalItems)

	// Add translations for current page
	for shift := startShift; shift <= endShift; shift++ {
		translated := b.translator.TranslateCaesar(description, shift)
		formattedTranslation := utils.FormatString(utils.TruncateString(translated, 600))
		embed.AddField(
			fmt.Sprintf("Shift %d", shift),
			formattedTranslation,
			false,
		)
	}

	// Create message with navigation
	builder := discord.NewMessageUpdateBuilder().
		SetEmbeds(embed.Build())

	// Add navigation buttons
	builder.AddActionRow(
		discord.NewSecondaryButton("◀️ Back", constants.BackButtonCustomID),
		discord.NewSecondaryButton("⏮️", string(session.ViewerFirstPage)).WithDisabled(b.page == 0),
		discord.NewSecondaryButton("◀️", string(session.ViewerPrevPage)).WithDisabled(b.page == 0),
		discord.NewSecondaryButton("▶️", string(session.ViewerNextPage)).WithDisabled(b.page == b.totalPages-1),
		discord.NewSecondaryButton("⏭️", string(session.ViewerLastPage)).WithDisabled(b.page == b.totalPages-1),
	)

	return builder
}
