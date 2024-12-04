package user

import (
	"bytes"
	"fmt"
	"strconv"

	"github.com/disgoorg/disgo/discord"
	apiTypes "github.com/jaxron/roapi.go/pkg/api/types"
	"github.com/rotector/rotector/internal/bot/constants"
	"github.com/rotector/rotector/internal/bot/core/session"
	"github.com/rotector/rotector/internal/bot/utils"
	"github.com/rotector/rotector/internal/common/storage/database/types"
)

// OutfitsBuilder creates the visual layout for viewing a user's outfits.
// It combines outfit information with thumbnails and supports pagination
// through a grid of outfit previews.
type OutfitsBuilder struct {
	settings    *types.UserSetting
	user        *types.FlaggedUser
	outfits     []apiTypes.Outfit
	start       int
	page        int
	total       int
	imageBuffer *bytes.Buffer
}

// NewOutfitsBuilder creates a new outfits builder.
func NewOutfitsBuilder(s *session.Session) *OutfitsBuilder {
	var settings *types.UserSetting
	s.GetInterface(constants.SessionKeyUserSettings, &settings)
	var user *types.FlaggedUser
	s.GetInterface(constants.SessionKeyTarget, &user)
	var outfits []apiTypes.Outfit
	s.GetInterface(constants.SessionKeyOutfits, &outfits)

	return &OutfitsBuilder{
		settings:    settings,
		user:        user,
		outfits:     outfits,
		start:       s.GetInt(constants.SessionKeyStart),
		page:        s.GetInt(constants.SessionKeyPaginationPage),
		total:       s.GetInt(constants.SessionKeyTotalItems),
		imageBuffer: s.GetBuffer(constants.SessionKeyImageBuffer),
	}
}

// Build creates a Discord message with a grid of outfit thumbnails and information.
// Each outfit entry shows:
// - Outfit name
// - Thumbnail preview
// Navigation buttons are disabled when at the start/end of the list.
func (b *OutfitsBuilder) Build() *discord.MessageUpdateBuilder {
	totalPages := (b.total + constants.OutfitsPerPage - 1) / constants.OutfitsPerPage

	// Create file attachment for the outfit thumbnails grid
	fileName := fmt.Sprintf("outfits_%d_%d.png", b.user.ID, b.page)
	file := discord.NewFile(fileName, "", b.imageBuffer)

	// Build embed with user info and thumbnails
	censor := b.settings.StreamerMode || b.settings.ReviewMode == types.TrainingReviewMode
	embed := discord.NewEmbedBuilder().
		SetTitle(fmt.Sprintf("User Outfits (Page %d/%d)", b.page+1, totalPages)).
		SetDescription(fmt.Sprintf("```%s (%s)```",
			utils.CensorString(b.user.Name, censor),
			utils.CensorString(strconv.FormatUint(b.user.ID, 10), censor),
		)).
		SetImage("attachment://" + fileName).
		SetColor(utils.GetMessageEmbedColor(b.settings.StreamerMode))

	// Add fields for each outfit on the current page
	for i, outfit := range b.outfits {
		embed.AddField(fmt.Sprintf("Outfit %d", b.start+i+1), outfit.Name, true)
	}

	// Add navigation buttons with proper disabled states
	components := []discord.ContainerComponent{
		discord.NewActionRow(
			discord.NewSecondaryButton("◀️", string(constants.BackButtonCustomID)),
			discord.NewSecondaryButton("⏮️", string(utils.ViewerFirstPage)).WithDisabled(b.page == 0),
			discord.NewSecondaryButton("◀️", string(utils.ViewerPrevPage)).WithDisabled(b.page == 0),
			discord.NewSecondaryButton("▶️", string(utils.ViewerNextPage)).WithDisabled(b.page == totalPages-1),
			discord.NewSecondaryButton("⏭️", string(utils.ViewerLastPage)).WithDisabled(b.page == totalPages-1),
		),
	}

	return discord.NewMessageUpdateBuilder().
		SetEmbeds(embed.Build()).
		SetFiles(file).
		AddContainerComponents(components...)
}
