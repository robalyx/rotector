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
		user:           s.GetFlaggedUser(constants.SessionKeyTarget),
		friends:        s.Get(constants.SessionKeyFriends).([]types.Friend),
		flaggedFriends: s.Get(constants.SessionKeyFlaggedFriends).(map[uint64]string),
		start:          s.GetInt(constants.SessionKeyStart),
		page:           s.GetInt(constants.SessionKeyPaginationPage),
		total:          s.GetInt(constants.SessionKeyTotalItems),
		file:           s.Get(constants.SessionKeyFile).(*discord.File),
		fileName:       s.GetString(constants.SessionKeyFileName),
		streamerMode:   s.GetBool(constants.SessionKeyStreamerMode),
	}
}

// Build constructs and returns the discord.Embed.
func (b *FriendsEmbed) Build() *discord.MessageUpdateBuilder {
	totalPages := (b.total + constants.FriendsPerPage - 1) / constants.FriendsPerPage

	embed := discord.NewEmbedBuilder().
		SetTitle(fmt.Sprintf("User Friends (Page %d/%d)", b.page+1, totalPages)).
		SetDescription(fmt.Sprintf("```%s (%d)```", utils.CensorString(b.user.Name, b.streamerMode), b.user.ID)).
		SetImage("attachment://" + b.fileName).
		SetColor(utils.GetMessageEmbedColor(b.streamerMode))

	components := []discord.ContainerComponent{
		discord.NewActionRow(
			discord.NewSecondaryButton("◀️", string(constants.BackButtonCustomID)),
			discord.NewSecondaryButton("⏮️", string(utils.ViewerFirstPage)).WithDisabled(b.page == 0),
			discord.NewSecondaryButton("◀️", string(utils.ViewerPrevPage)).WithDisabled(b.page == 0),
			discord.NewSecondaryButton("▶️", string(utils.ViewerNextPage)).WithDisabled(b.page == totalPages-1),
			discord.NewSecondaryButton("⏭️", string(utils.ViewerLastPage)).WithDisabled(b.page == totalPages-1),
		),
	}

	for i, friend := range b.friends {
		fieldName := fmt.Sprintf("Friend %d", b.start+i+1)
		fieldValue := fmt.Sprintf("[%s](https://www.roblox.com/users/%d/profile)", utils.CensorString(friend.Name, b.streamerMode), friend.ID)

		// Add confirmed or flagged status if needed
		if flagged, ok := b.flaggedFriends[friend.ID]; ok {
			if flagged == database.UserTypeConfirmed {
				fieldName += " ⚠️"
			} else if flagged == database.UserTypeFlagged {
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
