package builders

import (
	"fmt"

	"github.com/disgoorg/disgo/discord"
	"github.com/jaxron/roapi.go/pkg/api/types"
	"github.com/rotector/rotector/internal/common/database"
)

// OutfitsEmbed builds the embed for the outfit viewer message.
type OutfitsEmbed struct {
	user     *database.PendingUser
	outfits  []types.Outfit
	start    int
	page     int
	total    int
	file     *discord.File
	fileName string
}

// NewOutfitsEmbed creates a new OutfitsEmbed.
func NewOutfitsEmbed(user *database.PendingUser, outfits []types.Outfit, start, page, total int, file *discord.File, fileName string) *OutfitsEmbed {
	return &OutfitsEmbed{
		user:     user,
		outfits:  outfits,
		start:    start,
		page:     page,
		total:    total,
		file:     file,
		fileName: fileName,
	}
}

// Build constructs and returns the discord.Embed.
func (b *OutfitsEmbed) Build() *discord.MessageUpdateBuilder {
	embed := discord.NewEmbedBuilder().
		SetTitle(fmt.Sprintf("User Outfits (Page %d/%d)", b.page+1, b.total)).
		SetDescription(fmt.Sprintf("```%s (%d)```", b.user.Name, b.user.ID)).
		SetImage("attachment://" + b.fileName).
		SetColor(0x312D2B)

	for i, outfit := range b.outfits {
		embed.AddField(fmt.Sprintf("Outfit %d", b.start+i+1), outfit.Name, true)
	}

	components := []discord.ContainerComponent{
		discord.NewActionRow(
			discord.NewSecondaryButton("◀️", string(ViewerBackToReview)),
			discord.NewSecondaryButton("⏮️", string(ViewerFirstPage)).WithDisabled(b.page == 0),
			discord.NewSecondaryButton("◀️", string(ViewerPrevPage)).WithDisabled(b.page == 0),
			discord.NewSecondaryButton("▶️", string(ViewerNextPage)).WithDisabled(b.page == b.total-1),
			discord.NewSecondaryButton("⏭️", string(ViewerLastPage)).WithDisabled(b.page == b.total-1),
		),
	}

	return discord.NewMessageUpdateBuilder().
		SetEmbeds(embed.Build()).
		SetFiles(b.file).
		AddContainerComponents(components...)
}
