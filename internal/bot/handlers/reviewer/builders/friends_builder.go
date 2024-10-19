package builders

import (
	"fmt"

	"github.com/disgoorg/disgo/discord"
	"github.com/jaxron/roapi.go/pkg/api/types"
	"github.com/rotector/rotector/internal/common/database"
)

// FriendsEmbed builds the embed for the friends viewer message.
type FriendsEmbed struct {
	user           *database.PendingUser
	friends        []types.UserResponse
	flaggedFriends map[uint64]string
	start          int
	page           int
	total          int
	file           *discord.File
	fileName       string
}

// NewFriendsEmbed creates a new FriendsEmbed.
func NewFriendsEmbed(user *database.PendingUser, friends []types.UserResponse, flaggedFriends map[uint64]string, start, page, total int, file *discord.File, fileName string) *FriendsEmbed {
	return &FriendsEmbed{
		user:           user,
		friends:        friends,
		flaggedFriends: flaggedFriends,
		start:          start,
		page:           page,
		total:          total,
		file:           file,
		fileName:       fileName,
	}
}

// Build constructs and returns the discord.Embed.
func (b *FriendsEmbed) Build() *discord.MessageUpdateBuilder {
	embed := discord.NewEmbedBuilder().
		SetTitle(fmt.Sprintf("User Friends (Page %d/%d)", b.page+1, b.total)).
		SetDescription(fmt.Sprintf("```%s (%d)```", b.user.Name, b.user.ID)).
		SetImage("attachment://" + b.fileName).
		SetColor(0x312D2B)

	components := []discord.ContainerComponent{
		discord.NewActionRow(
			discord.NewSecondaryButton("◀️", string(ViewerBackToReview)),
			discord.NewSecondaryButton("⏮️", string(ViewerFirstPage)).WithDisabled(b.page == 0),
			discord.NewSecondaryButton("◀️", string(ViewerPrevPage)).WithDisabled(b.page == 0),
			discord.NewSecondaryButton("▶️", string(ViewerNextPage)).WithDisabled(b.page == b.total-1),
			discord.NewSecondaryButton("⏭️", string(ViewerLastPage)).WithDisabled(b.page == b.total-1),
		),
	}

	for i, friend := range b.friends {
		fieldName := fmt.Sprintf("Friend %d", b.start+i+1)
		fieldValue := fmt.Sprintf("[%s](https://www.roblox.com/users/%d/profile)", friend.Name, friend.ID)

		// Add flagged or pending status if needed
		if flagged, ok := b.flaggedFriends[friend.ID]; ok {
			if flagged == "flagged" {
				fieldName += " ⚠️"
			} else if flagged == "pending" {
				fieldName += " ⏳"
			}
		}

		embed.AddField(fieldName, fieldValue, true)
	}

	return discord.NewMessageUpdateBuilder().
		SetEmbeds(embed.Build()).
		AddContainerComponents(components...).
		SetFiles(b.file)
}
