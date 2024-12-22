package user

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"

	"github.com/disgoorg/disgo/discord"
	apiTypes "github.com/jaxron/roapi.go/pkg/api/types"
	"github.com/rotector/rotector/internal/bot/constants"
	"github.com/rotector/rotector/internal/bot/core/session"
	"github.com/rotector/rotector/internal/bot/utils"
	"github.com/rotector/rotector/internal/common/storage/database/types"
)

// FriendsBuilder creates the visual layout for viewing a user's friends.
type FriendsBuilder struct {
	settings       *types.UserSetting
	user           *types.ReviewUser
	friends        []types.ExtendedFriend
	presences      map[uint64]apiTypes.UserPresenceResponse
	flaggedFriends map[uint64]*types.User
	friendTypes    map[uint64]types.UserType
	start          int
	page           int
	total          int
	imageBuffer    *bytes.Buffer
	isStreaming    bool
}

// NewFriendsBuilder creates a new friends builder.
func NewFriendsBuilder(s *session.Session) *FriendsBuilder {
	var settings *types.UserSetting
	s.GetInterface(constants.SessionKeyUserSettings, &settings)
	var user *types.ReviewUser
	s.GetInterface(constants.SessionKeyTarget, &user)
	var friends []types.ExtendedFriend
	s.GetInterface(constants.SessionKeyFriends, &friends)
	var presences map[uint64]apiTypes.UserPresenceResponse
	s.GetInterface(constants.SessionKeyPresences, &presences)
	var flaggedFriends map[uint64]*types.User
	s.GetInterface(constants.SessionKeyFlaggedFriends, &flaggedFriends)
	var friendTypes map[uint64]types.UserType
	s.GetInterface(constants.SessionKeyFriendTypes, &friendTypes)

	return &FriendsBuilder{
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
		isStreaming:    s.GetBool(constants.SessionKeyIsStreaming),
	}
}

// Build creates a Discord message with a grid of friend avatars and information.
func (b *FriendsBuilder) Build() *discord.MessageUpdateBuilder {
	totalPages := (b.total + constants.FriendsPerPage - 1) / constants.FriendsPerPage
	censor := b.settings.StreamerMode || b.settings.ReviewMode == types.TrainingReviewMode

	// Create file attachment for the friend avatars grid
	fileName := fmt.Sprintf("friends_%d_%d.png", b.user.ID, b.page)
	file := discord.NewFile(fileName, "", b.imageBuffer)

	// Build base embed with user info
	embed := discord.NewEmbedBuilder().
		SetTitle(fmt.Sprintf("User Friends (Page %d/%d)", b.page+1, totalPages)).
		SetDescription(fmt.Sprintf(
			"```%s (%s)```",
			utils.CensorString(b.user.Name, censor),
			utils.CensorString(strconv.FormatUint(b.user.ID, 10), censor),
		)).
		SetImage("attachment://" + fileName).
		SetColor(utils.GetMessageEmbedColor(b.settings.StreamerMode))

	// Add fields for each friend
	for i, friend := range b.friends {
		fieldName := b.getFriendFieldName(i, friend)
		fieldValue := b.getFriendFieldValue(friend)
		embed.AddField(fieldName, fieldValue, true)
	}

	builder := discord.NewMessageUpdateBuilder().
		SetEmbeds(embed.Build()).
		SetFiles(file)

	// Only add navigation components if not streaming
	if !b.isStreaming {
		builder.AddContainerComponents([]discord.ContainerComponent{
			discord.NewActionRow(
				discord.NewSecondaryButton("◀️", string(constants.BackButtonCustomID)),
				discord.NewSecondaryButton("⏮️", string(utils.ViewerFirstPage)).WithDisabled(b.page == 0),
				discord.NewSecondaryButton("◀️", string(utils.ViewerPrevPage)).WithDisabled(b.page == 0),
				discord.NewSecondaryButton("▶️", string(utils.ViewerNextPage)).WithDisabled(b.page == totalPages-1),
				discord.NewSecondaryButton("⏭️", string(utils.ViewerLastPage)).WithDisabled(b.page == totalPages-1),
			),
		}...)
	}

	return builder
}

// getFriendFieldName creates the field name for a friend entry.
func (b *FriendsBuilder) getFriendFieldName(index int, friend types.ExtendedFriend) string {
	fieldName := fmt.Sprintf("Friend %d", b.start+index+1)

	// Add presence indicator
	if presence, ok := b.presences[friend.ID]; ok {
		switch presence.UserPresenceType {
		case apiTypes.Website:
			fieldName += " 🌐"
		case apiTypes.InGame:
			fieldName += " 🎮"
		case apiTypes.InStudio:
			fieldName += " 🔨"
		case apiTypes.Offline:
			fieldName += " 💤"
		}
	}

	// Add status indicator based on friend type
	if friendType, ok := b.friendTypes[friend.ID]; ok {
		switch friendType {
		case types.UserTypeConfirmed:
			fieldName += " ⚠️"
		case types.UserTypeFlagged:
			fieldName += " ⏳"
		case types.UserTypeCleared:
			fieldName += " ✅"
		case types.UserTypeBanned:
			fieldName += " 🔨"
		case types.UserTypeUnflagged:
		}
	}

	return fieldName
}

// getFriendFieldValue creates the field value for a friend entry.
func (b *FriendsBuilder) getFriendFieldValue(friend types.ExtendedFriend) string {
	var info strings.Builder

	// Add friend name (with link in standard mode)
	if b.settings.ReviewMode == types.TrainingReviewMode {
		info.WriteString(utils.CensorString(friend.Name, true))
	} else {
		info.WriteString(fmt.Sprintf(
			"[%s](https://www.roblox.com/users/%d/profile)",
			utils.CensorString(friend.Name, b.settings.StreamerMode),
			friend.ID,
		))
	}

	// Add presence details if available
	if presence, ok := b.presences[friend.ID]; ok {
		if presence.UserPresenceType != apiTypes.Offline {
			info.WriteString("\n" + presence.LastLocation)
		} else {
			info.WriteString(fmt.Sprintf("\nLast Online: <t:%d:R>", presence.LastOnline.Unix()))
		}
	}

	// Add flagged info and confidence if available
	if flaggedFriend, ok := b.flaggedFriends[friend.ID]; ok {
		if flaggedFriend.Confidence > 0 {
			info.WriteString(fmt.Sprintf(" (%.2f)", flaggedFriend.Confidence))
		}
		if flaggedFriend.Reason != "" {
			info.WriteString(fmt.Sprintf("\n```%s```", flaggedFriend.Reason))
		}
	}

	return info.String()
}
