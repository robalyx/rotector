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
	current        int
	total          int
	fileName       string
}

// NewFriendsEmbed creates a new FriendsEmbed.
func NewFriendsEmbed(user *database.PendingUser, friends []types.UserResponse, flaggedFriends map[uint64]string, start, current, total int, fileName string) *FriendsEmbed {
	return &FriendsEmbed{
		user:           user,
		friends:        friends,
		flaggedFriends: flaggedFriends,
		start:          start,
		current:        current,
		total:          total,
		fileName:       fileName,
	}
}

// Build constructs and returns the discord.Embed.
func (b *FriendsEmbed) Build() discord.Embed {
	embed := discord.NewEmbedBuilder().
		SetTitle(fmt.Sprintf("User Friends (Page %d/%d)", b.current+1, b.total)).
		SetDescription(fmt.Sprintf("```%s (%d)```", b.user.Name, b.user.ID)).
		SetImage("attachment://" + b.fileName).
		SetColor(0x312D2B)

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

	return embed.Build()
}
