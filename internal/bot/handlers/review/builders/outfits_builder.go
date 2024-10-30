package builders

import (
	"fmt"

	"github.com/disgoorg/disgo/discord"
	"github.com/jaxron/roapi.go/pkg/api/types"
	"github.com/rotector/rotector/internal/bot/constants"
	"github.com/rotector/rotector/internal/bot/session"
	"github.com/rotector/rotector/internal/bot/utils"
	"github.com/rotector/rotector/internal/common/database"
)

// OutfitsEmbed builds the embed for the outfit viewer message.
type OutfitsEmbed struct {
	user         *database.FlaggedUser
	outfits      []types.Outfit
	start        int
	page         int
	total        int
	file         *discord.File
	fileName     string
	streamerMode bool
}

// NewOutfitsEmbed creates a new OutfitsEmbed.
func NewOutfitsEmbed(s *session.Session) *OutfitsEmbed {
	return &OutfitsEmbed{
		user:         s.GetFlaggedUser(constants.SessionKeyTarget),
		outfits:      s.Get(constants.SessionKeyOutfits).([]types.Outfit),
		start:        s.GetInt(constants.SessionKeyStart),
		page:         s.GetInt(constants.SessionKeyPaginationPage),
		total:        s.GetInt(constants.SessionKeyTotalItems),
		file:         s.Get(constants.SessionKeyFile).(*discord.File),
		fileName:     s.GetString(constants.SessionKeyFileName),
		streamerMode: s.GetBool(constants.SessionKeyStreamerMode),
	}
}

// Build constructs and returns the discord.Embed.
func (b *OutfitsEmbed) Build() *discord.MessageUpdateBuilder {
	totalPages := (b.total + constants.OutfitsPerPage - 1) / constants.OutfitsPerPage

	embed := discord.NewEmbedBuilder().
		SetTitle(fmt.Sprintf("User Outfits (Page %d/%d)", b.page+1, totalPages)).
		SetDescription(fmt.Sprintf("```%s (%d)```", b.user.Name, b.user.ID)).
		SetImage("attachment://" + b.fileName).
		SetColor(utils.GetMessageEmbedColor(b.streamerMode))

	for i, outfit := range b.outfits {
		embed.AddField(fmt.Sprintf("Outfit %d", b.start+i+1), outfit.Name, true)
	}

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
		SetFiles(b.file).
		AddContainerComponents(components...)
}
