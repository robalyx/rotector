package builders

import (
	"bytes"
	"fmt"

	"github.com/disgoorg/disgo/discord"
	"github.com/jaxron/roapi.go/pkg/api/types"
	"github.com/rotector/rotector/internal/bot/constants"
	"github.com/rotector/rotector/internal/bot/session"
	"github.com/rotector/rotector/internal/bot/utils"
	"github.com/rotector/rotector/internal/common/database"
)

// FriendsEmbed creates the visual layout for viewing a user's friends.
// It combines friend information with flagged/confirmed status indicators and
// supports pagination through a grid of friend avatars.
type FriendsEmbed struct {
	user           *database.FlaggedUser
	friends        []types.Friend
	flaggedFriends map[uint64]string
	start          int
	page           int
	total          int
	imageBuffer    *bytes.Buffer
	streamerMode   bool
}

// NewFriendsEmbed loads friend data and settings from the session state
// to create a new embed builder.
func NewFriendsEmbed(s *session.Session) *FriendsEmbed {
	var user *database.FlaggedUser
	s.GetInterface(constants.SessionKeyTarget, &user)
	var friends []types.Friend
	s.GetInterface(constants.SessionKeyFriends, &friends)
	var flaggedFriends map[uint64]string
	s.GetInterface(constants.SessionKeyFlaggedFriends, &flaggedFriends)

	return &FriendsEmbed{
		user:           user,
		friends:        friends,
		flaggedFriends: flaggedFriends,
		start:          s.GetInt(constants.SessionKeyStart),
		page:           s.GetInt(constants.SessionKeyPaginationPage),
		total:          s.GetInt(constants.SessionKeyTotalItems),
		imageBuffer:    s.GetBuffer(constants.SessionKeyImageBuffer),
		streamerMode:   s.GetBool(constants.SessionKeyStreamerMode),
	}
}

// Build creates a Discord message with a grid of friend avatars and information.
// Each friend entry shows:
// - Friend name (with link to profile)
// - Warning indicator if the friend is confirmed (⚠️)
// - Clock indicator if the friend is flagged (⏳)
// Navigation buttons are disabled when at the start/end of the list.
func (b *FriendsEmbed) Build() *discord.MessageUpdateBuilder {
	totalPages := (b.total + constants.FriendsPerPage - 1) / constants.FriendsPerPage

	// Create file attachment for the friend avatars grid
	fileName := fmt.Sprintf("friends_%d_%d.png", b.user.ID, b.page)
	file := discord.NewFile(fileName, "", b.imageBuffer)

	// Build embed with user info and avatars
	embed := discord.NewEmbedBuilder().
		SetTitle(fmt.Sprintf("User Friends (Page %d/%d)", b.page+1, totalPages)).
		SetDescription(fmt.Sprintf("```%s (%d)```", utils.CensorString(b.user.Name, b.streamerMode), b.user.ID)).
		SetImage("attachment://" + fileName).
		SetColor(utils.GetMessageEmbedColor(b.streamerMode))

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

	// Add fields for each friend on the current page
	for i, friend := range b.friends {
		fieldName := fmt.Sprintf("Friend %d", b.start+i+1)
		fieldValue := fmt.Sprintf("[%s](https://www.roblox.com/users/%d/profile)",
			utils.CensorString(friend.Name, b.streamerMode), friend.ID)

		// Add status indicators for flagged/confirmed friends
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
		SetFiles(file)
}
