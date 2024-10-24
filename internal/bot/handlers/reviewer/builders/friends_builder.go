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

// FriendsEmbed builds the embed for the friends viewer message.
type FriendsEmbed struct {
	user           *database.FlaggedUser
	friends        []types.Friend
	flaggedFriends map[uint64]string
	start          int
	page           int
	total          int
	file           *discord.File
	fileName       string
	streamerMode   bool
}

// NewFriendsEmbed creates a new FriendsEmbed.
func NewFriendsEmbed(s *session.Session) *FriendsEmbed {
	return &FriendsEmbed{
		user:           s.GetFlaggedUser(constants.KeyTarget),
		friends:        s.Get(constants.SessionKeyFriends).([]types.Friend),
		flaggedFriends: s.Get(constants.SessionKeyFlaggedFriends).(map[uint64]string),
		start:          s.GetInt(constants.SessionKeyStart),
		page:           s.GetInt(constants.SessionKeyPage),
		total:          s.GetInt(constants.SessionKeyTotal),
		file:           s.Get(constants.SessionKeyFile).(*discord.File),
		fileName:       s.GetString(constants.SessionKeyFileName),
		streamerMode:   s.GetBool(constants.SessionKeyStreamerMode),
	}
}

// Build constructs and returns the discord.Embed.
func (b *FriendsEmbed) Build() *discord.MessageUpdateBuilder {
	embed := discord.NewEmbedBuilder().
		SetTitle(fmt.Sprintf("User Friends (Page %d/%d)", b.page+1, b.total)).
		SetDescription(fmt.Sprintf("```%s (%d)```", utils.CensorString(b.user.Name, b.streamerMode), b.user.ID)).
		SetImage("attachment://" + b.fileName).
		SetColor(utils.GetMessageEmbedColor(b.streamerMode))

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
		fieldValue := fmt.Sprintf("[%s](https://www.roblox.com/users/%d/profile)", utils.CensorString(friend.Name, b.streamerMode), friend.ID)

		// Add confirmed or flagged status if needed
		if flagged, ok := b.flaggedFriends[friend.ID]; ok {
			if flagged == "confirmed" {
				fieldName += " ⚠️"
			} else if flagged == "flagged" {
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
