package user

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"

	"github.com/disgoorg/disgo/discord"
	apiTypes "github.com/jaxron/roapi.go/pkg/api/types"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/bot/utils"
	"github.com/robalyx/rotector/internal/common/storage/database/types"
	"github.com/robalyx/rotector/internal/common/storage/database/types/enum"
)

// FriendsBuilder creates the visual layout for viewing a user's friends.
type FriendsBuilder struct {
	user           *types.ReviewUser
	friends        []*apiTypes.ExtendedFriend
	presences      map[uint64]*apiTypes.UserPresenceResponse
	flaggedFriends map[uint64]*types.ReviewUser
	start          int
	page           int
	total          int
	imageBuffer    *bytes.Buffer
	isStreaming    bool
	trainingMode   bool
	privacyMode    bool
}

// NewFriendsBuilder creates a new friends builder.
func NewFriendsBuilder(s *session.Session) *FriendsBuilder {
	trainingMode := session.UserReviewMode.Get(s) == enum.ReviewModeTraining
	return &FriendsBuilder{
		user:           session.UserTarget.Get(s),
		friends:        session.UserFriends.Get(s),
		presences:      session.UserPresences.Get(s),
		flaggedFriends: session.UserFlaggedFriends.Get(s),
		start:          session.PaginationOffset.Get(s),
		page:           session.PaginationPage.Get(s),
		total:          session.PaginationTotalItems.Get(s),
		imageBuffer:    session.ImageBuffer.Get(s),
		isStreaming:    session.PaginationIsStreaming.Get(s),
		trainingMode:   trainingMode,
		privacyMode:    trainingMode || session.UserStreamerMode.Get(s),
	}
}

// Build creates a Discord message with a grid of friend avatars and information.
func (b *FriendsBuilder) Build() *discord.MessageUpdateBuilder {
	totalPages := (b.total + constants.FriendsPerPage - 1) / constants.FriendsPerPage

	// Create file attachment for the friend avatars grid
	fileName := fmt.Sprintf("friends_%d_%d.png", b.user.ID, b.page)
	file := discord.NewFile(fileName, "", b.imageBuffer)

	// Build base embed with user info
	embed := discord.NewEmbedBuilder().
		SetTitle(fmt.Sprintf("User Friends (Page %d/%d)", b.page+1, totalPages)).
		SetDescription(fmt.Sprintf(
			"```%s (%s)```",
			utils.CensorString(b.user.Name, b.privacyMode),
			utils.CensorString(strconv.FormatUint(b.user.ID, 10), b.privacyMode),
		)).
		SetImage("attachment://" + fileName).
		SetColor(utils.GetMessageEmbedColor(b.privacyMode))

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
				discord.NewSecondaryButton("◀️", constants.BackButtonCustomID),
				discord.NewSecondaryButton("⏮️", string(session.ViewerFirstPage)).WithDisabled(b.page == 0),
				discord.NewSecondaryButton("◀️", string(session.ViewerPrevPage)).WithDisabled(b.page == 0),
				discord.NewSecondaryButton("▶️", string(session.ViewerNextPage)).WithDisabled(b.page == totalPages-1),
				discord.NewSecondaryButton("⏭️", string(session.ViewerLastPage)).WithDisabled(b.page == totalPages-1),
			),
		}...)
	}

	return builder
}

// getFriendFieldName creates the field name for a friend entry.
func (b *FriendsBuilder) getFriendFieldName(index int, friend *apiTypes.ExtendedFriend) string {
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

	// Add status indicator based on friend status
	if reviewUser, ok := b.flaggedFriends[friend.ID]; ok {
		switch reviewUser.Status {
		case enum.UserTypeConfirmed:
			fieldName += " ⚠️"
		case enum.UserTypeFlagged:
			fieldName += " ⏳"
		case enum.UserTypeCleared:
			fieldName += " ✅"
		case enum.UserTypeUnflagged:
		}

		// Add banned status if applicable
		if reviewUser.IsBanned {
			fieldName += " 🔨"
		}
	}

	return fieldName
}

// getFriendFieldValue creates the field value for a friend entry.
func (b *FriendsBuilder) getFriendFieldValue(friend *apiTypes.ExtendedFriend) string {
	var info strings.Builder

	// Add friend name (with link in standard mode)
	name := utils.CensorString(friend.Name, b.privacyMode)
	if b.trainingMode {
		info.WriteString(name)
	} else {
		info.WriteString(fmt.Sprintf(
			"[%s](https://www.roblox.com/users/%d/profile)",
			name,
			friend.ID,
		))
	}

	// Add presence details if available
	if presence, ok := b.presences[friend.ID]; ok {
		if presence.UserPresenceType != apiTypes.Offline {
			info.WriteString("\n" + presence.LastLocation)
		} else if presence.LastOnline != nil {
			info.WriteString(fmt.Sprintf("\nLast Online: <t:%d:R>", presence.LastOnline.Unix()))
		}
	}

	// Add confidence and reason if available
	if flaggedFriend, ok := b.flaggedFriends[friend.ID]; ok && len(flaggedFriend.Reasons) > 0 {
		reasonTypes := flaggedFriend.Reasons.Types()
		info.WriteString(fmt.Sprintf("\n(%.2f) [%s]", flaggedFriend.Confidence, strings.Join(reasonTypes, ", ")))
	}

	return info.String()
}
