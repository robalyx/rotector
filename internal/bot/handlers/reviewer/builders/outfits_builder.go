package builders

import (
	"fmt"

	"github.com/disgoorg/disgo/discord"
	"github.com/jaxron/roapi.go/pkg/api/types"
	"github.com/rotector/rotector/internal/common/database"
)

// OutfitsEmbed builds the embed for the outfit viewer message.
type OutfitsEmbed struct {
	user       *database.PendingUser
	outfits    []types.OutfitData
	start      int
	page       int
	totalPages int
	fileName   string
}

// NewOutfitsEmbed creates a new OutfitsEmbed.
func NewOutfitsEmbed(user *database.PendingUser, outfits []types.OutfitData, start, page, totalPages int, fileName string) *OutfitsEmbed {
	return &OutfitsEmbed{
		user:       user,
		outfits:    outfits,
		start:      start,
		page:       page,
		totalPages: totalPages,
		fileName:   fileName,
	}
}

// Build constructs and returns the discord.Embed.
func (b *OutfitsEmbed) Build() discord.Embed {
	embed := discord.NewEmbedBuilder().
		SetTitle(fmt.Sprintf("User Outfits (Page %d/%d)", b.page+1, b.totalPages)).
		SetDescription(fmt.Sprintf("```%s (%d)```", b.user.Name, b.user.ID)).
		SetImage("attachment://" + b.fileName).
		SetColor(0x312D2B)

	for i, outfit := range b.outfits {
		inline := true
		embed.AddField(fmt.Sprintf("Outfit %d", b.start+i+1), outfit.Name, inline)
	}

	return embed.Build()
}
