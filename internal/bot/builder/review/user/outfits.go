package user

import (
	"bytes"
	"fmt"
	"strconv"

	"github.com/disgoorg/disgo/discord"
	apiTypes "github.com/jaxron/roapi.go/pkg/api/types"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/bot/utils"
	"github.com/robalyx/rotector/internal/common/storage/database/types"
	"github.com/robalyx/rotector/internal/common/storage/database/types/enum"
)

// OutfitsBuilder creates the visual layout for viewing a user's outfits.
type OutfitsBuilder struct {
	user        *types.ReviewUser
	outfits     []*apiTypes.Outfit
	start       int
	page        int
	total       int
	imageBuffer *bytes.Buffer
	isStreaming bool
	privacyMode bool
}

// NewOutfitsBuilder creates a new outfits builder.
func NewOutfitsBuilder(s *session.Session) *OutfitsBuilder {
	return &OutfitsBuilder{
		user:        session.UserTarget.Get(s),
		outfits:     session.UserOutfits.Get(s),
		start:       session.PaginationOffset.Get(s),
		page:        session.PaginationPage.Get(s),
		total:       session.PaginationTotalItems.Get(s),
		imageBuffer: session.ImageBuffer.Get(s),
		isStreaming: session.PaginationIsStreaming.Get(s),
		privacyMode: session.UserReviewMode.Get(s) == enum.ReviewModeTraining || session.UserStreamerMode.Get(s),
	}
}

// Build creates a Discord message with a grid of outfit thumbnails and information.
func (b *OutfitsBuilder) Build() *discord.MessageUpdateBuilder {
	totalPages := (b.total + constants.OutfitsPerPage - 1) / constants.OutfitsPerPage

	// Create file attachment for the outfit thumbnails grid
	fileName := fmt.Sprintf("outfits_%d_%d.png", b.user.ID, b.page)
	file := discord.NewFile(fileName, "", b.imageBuffer)

	// Build base embed with user info
	embed := discord.NewEmbedBuilder().
		SetTitle(fmt.Sprintf("User Outfits (Page %d/%d)", b.page+1, totalPages)).
		SetDescription(fmt.Sprintf(
			"```%s (%s)```",
			utils.CensorString(b.user.Name, b.privacyMode),
			utils.CensorString(strconv.FormatUint(b.user.ID, 10), b.privacyMode),
		)).
		SetImage("attachment://" + fileName).
		SetColor(utils.GetMessageEmbedColor(b.privacyMode))

	// Add fields for each outfit
	for i, outfit := range b.outfits {
		embed.AddField(fmt.Sprintf("Outfit %d", b.start+i+1), outfit.Name, true)
	}

	builder := discord.NewMessageUpdateBuilder().
		SetEmbeds(embed.Build()).
		SetFiles(file)

	// Only add navigation components if not streaming
	if !b.isStreaming {
		builder.AddContainerComponents([]discord.ContainerComponent{
			discord.NewActionRow(
				discord.NewSecondaryButton("◀️", constants.BackButtonCustomID),
				discord.NewSecondaryButton("⏮️", string(session.ViewerFirstPage)).WithDisabled(b.page == 0),
				discord.NewSecondaryButton("◀️", string(session.ViewerPrevPage)).WithDisabled(b.page == 0),
				discord.NewSecondaryButton("▶️", string(session.ViewerNextPage)).WithDisabled(b.page == totalPages-1),
				discord.NewSecondaryButton("⏭️", string(session.ViewerLastPage)).WithDisabled(b.page == totalPages-1),
			),
		}...)
	}

	return builder
}
