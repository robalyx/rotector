package user

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"

	"github.com/disgoorg/disgo/discord"
	apiTypes "github.com/jaxron/roapi.go/pkg/api/types"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/bot/utils"
	"github.com/robalyx/rotector/internal/bot/views/review/shared"
	"github.com/robalyx/rotector/internal/database/types"
	"github.com/robalyx/rotector/internal/database/types/enum"
)

// OutfitsBuilder creates the visual layout for viewing a user's outfits.
type OutfitsBuilder struct {
	user           *types.ReviewUser
	outfits        []*apiTypes.Outfit
	flaggedOutfits map[string]struct{}
	start          int
	page           int
	totalItems     int
	totalPages     int
	imageBuffer    *bytes.Buffer
	isStreaming    bool
	isReviewer     bool
	trainingMode   bool
	privacyMode    bool
}

// NewOutfitsBuilder creates a new outfits builder.
func NewOutfitsBuilder(s *session.Session) *OutfitsBuilder {
	trainingMode := session.UserReviewMode.Get(s) == enum.ReviewModeTraining
	return &OutfitsBuilder{
		user:           session.UserTarget.Get(s),
		outfits:        session.UserOutfits.Get(s),
		flaggedOutfits: session.UserFlaggedOutfits.Get(s),
		start:          session.PaginationOffset.Get(s),
		page:           session.PaginationPage.Get(s),
		totalItems:     session.PaginationTotalItems.Get(s),
		totalPages:     session.PaginationTotalPages.Get(s),
		imageBuffer:    session.ImageBuffer.Get(s),
		isStreaming:    session.PaginationIsStreaming.Get(s),
		isReviewer:     s.BotSettings().IsReviewer(session.UserID.Get(s)),
		trainingMode:   trainingMode,
		privacyMode:    trainingMode || session.UserStreamerMode.Get(s),
	}
}

// Build creates a Discord message with a grid of outfit thumbnails and information.
func (b *OutfitsBuilder) Build() *discord.MessageUpdateBuilder {
	// Create file attachment for the outfit thumbnails grid
	fileName := fmt.Sprintf("outfits_%d_%d.png", b.user.ID, b.page)

	// Build content
	var content strings.Builder
	content.WriteString("## User Outfits\n")
	content.WriteString(fmt.Sprintf("```%s (%s)```",
		utils.CensorString(b.user.Name, b.privacyMode),
		utils.CensorString(strconv.FormatUint(b.user.ID, 10), b.privacyMode),
	))

	// Add outfits list
	for i, outfit := range b.outfits {
		// Add row indicator at the start of each row
		if i%constants.OutfitGridColumns == 0 {
			content.WriteString(fmt.Sprintf("\n\n**Row %d:**", (i/constants.OutfitGridColumns)+1))
		}

		// Check if outfit is flagged and add indicator
		indicator := ""
		if _, ok := b.flaggedOutfits[outfit.Name]; ok {
			indicator = " ⚠️"
		}

		// Add outfit name
		content.WriteString(fmt.Sprintf("\n**%d.** %s%s", b.start+i+1, outfit.Name, indicator))
	}

	// Add page info at the bottom
	content.WriteString(fmt.Sprintf("\n\n-# Page %d/%d", b.page+1, b.totalPages+1))

	// Build container with all components
	container := discord.NewContainer(
		discord.NewTextDisplay(content.String()),
		discord.NewMediaGallery(discord.MediaGalleryItem{
			Media: discord.UnfurledMediaItem{
				URL: "attachment://" + fileName,
			},
		}),
	).WithAccentColor(utils.GetContainerColor(b.privacyMode))

	// Add outfit reason section if exists
	if reason, ok := b.user.Reasons[enum.UserReasonTypeOutfit]; ok {
		var reasonContent strings.Builder
		reasonContent.WriteString(shared.BuildSingleReasonDisplay(
			b.privacyMode,
			enum.UserReasonTypeOutfit,
			reason,
			200,
			strconv.FormatUint(b.user.ID, 10),
			b.user.Name,
			b.user.DisplayName))

		container = container.AddComponents(
			discord.NewLargeSeparator(),
			discord.NewTextDisplay(reasonContent.String()),
			discord.NewLargeSeparator(),
		)
	}

	// Add edit button if reviewer and not in training mode
	if b.isReviewer && !b.trainingMode {
		container = container.AddComponents(
			discord.NewActionRow(
				discord.NewSecondaryButton("Edit Reason", constants.EditReasonButtonCustomID),
			),
		)
	}

	// Add pagination buttons if not streaming
	if !b.isStreaming {
		container = container.AddComponents(
			discord.NewActionRow(
				discord.NewSecondaryButton("⏮️", string(session.ViewerFirstPage)).WithDisabled(b.page == 0),
				discord.NewSecondaryButton("◀️", string(session.ViewerPrevPage)).WithDisabled(b.page == 0),
				discord.NewSecondaryButton("▶️", string(session.ViewerNextPage)).WithDisabled(b.page == b.totalPages),
				discord.NewSecondaryButton("⏭️", string(session.ViewerLastPage)).WithDisabled(b.page == b.totalPages),
			),
		)
	}

	// Build message with container and back button
	builder := discord.NewMessageUpdateBuilder().
		SetFiles(discord.NewFile(fileName, "", b.imageBuffer)).
		AddComponents(container)

	// Add back button if not streaming
	if !b.isStreaming {
		builder.AddComponents(
			discord.NewActionRow(
				discord.NewSecondaryButton("◀️ Back", constants.BackButtonCustomID),
			),
		)
	}

	return builder
}
