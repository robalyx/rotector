package builders

import (
	"bytes"
	"fmt"
	"strconv"

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
	settings       *database.UserSetting
	user           *database.FlaggedUser
	friends        []database.ExtendedFriend
	presences      map[uint64]types.UserPresenceResponse
	flaggedFriends map[uint64]*database.User
	friendTypes    map[uint64]string
	start          int
	page           int
	total          int
	imageBuffer    *bytes.Buffer
}

// NewFriendsEmbed loads friend data and settings from the session state
// to create a new embed builder.
func NewFriendsEmbed(s *session.Session) *FriendsEmbed {
	var settings *database.UserSetting
	s.GetInterface(constants.SessionKeyUserSettings, &settings)
	var user *database.FlaggedUser
	s.GetInterface(constants.SessionKeyTarget, &user)
	var friends []database.ExtendedFriend
	s.GetInterface(constants.SessionKeyFriends, &friends)
	var presences map[uint64]types.UserPresenceResponse
	s.GetInterface(constants.SessionKeyPresences, &presences)
	var flaggedFriends map[uint64]*database.User
	s.GetInterface(constants.SessionKeyFlaggedFriends, &flaggedFriends)
	var friendTypes map[uint64]string
	s.GetInterface(constants.SessionKeyFriendTypes, &friendTypes)

	return &FriendsEmbed{
		settings:       settings,
		user:           user,
		friends:        friends,
		presences:      presences,
		flaggedFriends: flaggedFriends,
		friendTypes:    friendTypes,
		start:          s.GetInt(constants.SessionKeyStart),
		page:           s.GetInt(constants.SessionKeyPaginationPage),
		total:          s.GetInt(constants.SessionKeyTotalItems),
		imageBuffer:    s.GetBuffer(constants.SessionKeyImageBuffer),
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
	censor := b.settings.StreamerMode || b.settings.ReviewMode == database.TrainingReviewMode
	embed := discord.NewEmbedBuilder().
		SetTitle(fmt.Sprintf("User Friends (Page %d/%d)", b.page+1, totalPages)).
		SetDescription(fmt.Sprintf(
			"```%s (%s)```",
			utils.CensorString(b.user.Name, censor),
			utils.CensorString(strconv.FormatUint(b.user.ID, 10), censor),
		)).
		SetImage("attachment://" + fileName).
		SetColor(utils.GetMessageEmbedColor(b.settings.StreamerMode))

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

		// Add presence indicator emoji
		if presence, ok := b.presences[friend.ID]; ok {
			switch presence.UserPresenceType {
			case types.Website:
				fieldName += " 🌐" // Website
			case types.InGame:
				fieldName += " 🎮" // In Game
			case types.InStudio:
				fieldName += " 🔨" // In Studio
			case types.Offline:
				fieldName += " 💤" // Offline
			}
		}

		// Add status indicators (⚠️ or ⏳)
		if friendType, ok := b.friendTypes[friend.ID]; ok {
			switch friendType {
			case database.UserTypeConfirmed:
				fieldName += " ⚠️"
			case database.UserTypeFlagged:
				fieldName += " ⏳"
			}
		}

		// Format friend information based on mode
		var fieldValue string
		if b.settings.ReviewMode == database.TrainingReviewMode {
			fieldValue = utils.CensorString(friend.Name, true)
		} else {
			fieldValue = fmt.Sprintf(
				"[%s](https://www.roblox.com/users/%d/profile)",
				utils.CensorString(friend.Name, b.settings.StreamerMode),
				friend.ID,
			)
		}

		// Add presence details if available
		if presence, ok := b.presences[friend.ID]; ok {
			if presence.UserPresenceType != types.Offline {
				fieldValue += "\n" + presence.LastLocation
			} else {
				fieldValue += fmt.Sprintf("\nLast Online: <t:%d:R>", presence.LastOnline.Unix())
			}
		}

		// Add flagged friend info if available
		if flaggedFriend, ok := b.flaggedFriends[friend.ID]; ok {
			if flaggedFriend.Confidence > 0 {
				fieldValue += fmt.Sprintf(" (%.2f)", flaggedFriend.Confidence)
			}
			fieldValue += fmt.Sprintf("\n```%s```", flaggedFriend.Reason)
		}

		embed.AddField(fieldName, fieldValue, true)
	}

	return discord.NewMessageUpdateBuilder().
		SetEmbeds(embed.Build()).
		AddContainerComponents(components...).
		SetFiles(file)
}
