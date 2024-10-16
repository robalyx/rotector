package reviewer

import (
	"fmt"

	"github.com/disgoorg/disgo/discord"
	"github.com/jaxron/roapi.go/pkg/api/types"
	"github.com/rotector/rotector/internal/common/database"
)

// FriendsEmbedBuilder builds the embed for the friends viewer message.
type FriendsEmbedBuilder struct {
	user     *database.PendingUser
	friends  []types.UserResponse
	start    int
	current  int
	total    int
	fileName string
}

// NewFriendsEmbedBuilder creates a new FriendsEmbedBuilder.
func NewFriendsEmbedBuilder(user *database.PendingUser, friends []types.UserResponse, start, current, total int, fileName string) *FriendsEmbedBuilder {
	return &FriendsEmbedBuilder{
		user:     user,
		friends:  friends,
		start:    start,
		current:  current,
		total:    total,
		fileName: fileName,
	}
}

// Build constructs and returns the discord.Embed.
func (b *FriendsEmbedBuilder) Build() discord.Embed {
	embed := discord.NewEmbedBuilder().
		SetTitle(fmt.Sprintf("User Friends (Page %d/%d)", b.current+1, b.total)).
		SetDescription(fmt.Sprintf("```%s (%d)```", b.user.Name, b.user.ID)).
		SetImage("attachment://" + b.fileName).
		SetColor(0x312D2B)

	for i, friend := range b.friends {
		embed.AddField(
			fmt.Sprintf("Friend %d", b.start+i+1),
			fmt.Sprintf("[%s](https://www.roblox.com/users/%d/profile)", friend.Name, friend.ID),
			true,
		)
	}

	return embed.Build()
}
