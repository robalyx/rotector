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
	"github.com/robalyx/rotector/internal/bot/views/review/shared"
	"github.com/robalyx/rotector/internal/database/types"
	"github.com/robalyx/rotector/internal/database/types/enum"
)

// FriendsBuilder creates the visual layout for viewing a user's friends.
type FriendsBuilder struct {
	user           *types.ReviewUser
	friends        []*apiTypes.ExtendedFriend
	presences      map[uint64]*apiTypes.UserPresenceResponse
	flaggedFriends map[uint64]*types.ReviewUser
	start          int
	page           int
	totalItems     int
	totalPages     int
	imageBuffer    *bytes.Buffer
	isStreaming    bool
	isReviewer     bool
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
		totalItems:     session.PaginationTotalItems.Get(s),
		totalPages:     session.PaginationTotalPages.Get(s),
		imageBuffer:    session.ImageBuffer.Get(s),
		isStreaming:    session.PaginationIsStreaming.Get(s),
		isReviewer:     s.BotSettings().IsReviewer(session.UserID.Get(s)),
		trainingMode:   trainingMode,
		privacyMode:    trainingMode || session.UserStreamerMode.Get(s),
	}
}

// Build creates a Discord message with a grid of friend avatars and information.
func (b *FriendsBuilder) Build() *discord.MessageUpdateBuilder {
	// Create file attachment for the friend avatars grid
	fileName := fmt.Sprintf("friends_%d_%d.png", b.user.ID, b.page)

	// Build content
	var content strings.Builder
	content.WriteString("## User Friends\n")
	content.WriteString(fmt.Sprintf("```%s (%s)```",
		utils.CensorString(b.user.Name, b.privacyMode),
		utils.CensorString(strconv.FormatUint(b.user.ID, 10), b.privacyMode),
	))

	// Add friends list
	for i, friend := range b.friends {
		// Add row indicator at the start of each row
		if i%constants.FriendsGridColumns == 0 {
			content.WriteString(fmt.Sprintf("\n\n**Row %d:**", (i/constants.FriendsGridColumns)+1))
		}

		// Add friend name with status indicators
		content.WriteString(fmt.Sprintf("\n**%d.** %s", b.start+i+1, b.getFriendFieldName(friend)))

		// Add friend details
		details := b.getFriendFieldValue(friend)
		if details != "" {
			content.WriteString(" • " + details)
		}
	}

	// Add page info at the bottom
	content.WriteString(fmt.Sprintf("\n\n-# Page %d/%d", b.page+1, b.totalPages+1))

	// Build container with all components
	container := discord.NewContainer(
		discord.NewTextDisplay(content.String()),
		discord.NewMediaGallery(discord.MediaGalleryItem{
			Media: discord.UnfurledMediaItem{
				URL: "attachment://" + fileName,
			},
		}),
	).WithAccentColor(utils.GetContainerColor(b.privacyMode))

	// Add friend reason section if exists
	if reason, ok := b.user.Reasons[enum.UserReasonTypeFriend]; ok {
		var reasonContent strings.Builder
		reasonContent.WriteString(shared.BuildSingleReasonDisplay(
			b.privacyMode,
			enum.UserReasonTypeFriend,
			reason,
			200,
			strconv.FormatUint(b.user.ID, 10),
			b.user.Name,
			b.user.DisplayName))

		container = container.AddComponents(
			discord.NewLargeSeparator(),
			discord.NewTextDisplay(reasonContent.String()),
			discord.NewLargeSeparator(),
		)
	}

	// Add edit button if reviewer and not in training mode
	if b.isReviewer && !b.trainingMode && !b.isStreaming {
		container = container.AddComponents(
			discord.NewActionRow(
				discord.NewSecondaryButton("Edit Reason", constants.EditReasonButtonCustomID),
			),
		)
	}

	// Add pagination buttons if not streaming
	if !b.isStreaming {
		container = container.AddComponents(
			discord.NewActionRow(
				discord.NewSecondaryButton("⏮️", string(session.ViewerFirstPage)).WithDisabled(b.page == 0),
				discord.NewSecondaryButton("◀️", string(session.ViewerPrevPage)).WithDisabled(b.page == 0),
				discord.NewSecondaryButton("▶️", string(session.ViewerNextPage)).WithDisabled(b.page == b.totalPages),
				discord.NewSecondaryButton("⏭️", string(session.ViewerLastPage)).WithDisabled(b.page == b.totalPages),
			),
		)
	}

	// Build message with container and back button
	builder := discord.NewMessageUpdateBuilder().
		SetFiles(discord.NewFile(fileName, "", b.imageBuffer)).
		AddComponents(container)

	// Add back button if not streaming
	if !b.isStreaming {
		builder.AddComponents(
			discord.NewActionRow(
				discord.NewSecondaryButton("◀️ Back", constants.BackButtonCustomID),
			),
		)
	}

	return builder
}

// getFriendFieldName creates the field name for a friend entry.
func (b *FriendsBuilder) getFriendFieldName(friend *apiTypes.ExtendedFriend) string {
	var indicators []string

	// Add presence indicator
	if presence, ok := b.presences[friend.ID]; ok {
		switch presence.UserPresenceType {
		case apiTypes.Website:
			indicators = append(indicators, "🌐")
		case apiTypes.InGame:
			indicators = append(indicators, "🎮")
		case apiTypes.InStudio:
			indicators = append(indicators, "🔨")
		case apiTypes.Offline:
			indicators = append(indicators, "💤")
		}
	}

	// Add status indicator based on friend status
	if reviewUser, ok := b.flaggedFriends[friend.ID]; ok {
		switch reviewUser.Status {
		case enum.UserTypeConfirmed:
			indicators = append(indicators, "⚠️")
		case enum.UserTypeFlagged:
			indicators = append(indicators, "⏳")
		case enum.UserTypeCleared:
			indicators = append(indicators, "✅")
		}

		// Add banned status if applicable
		if reviewUser.IsBanned {
			indicators = append(indicators, "🔨")
		}
	}

	// Add friend name (with link in standard mode)
	name := utils.CensorString(friend.Name, b.privacyMode)
	if !b.trainingMode {
		name = fmt.Sprintf("[%s](https://www.roblox.com/users/%d/profile)", name, friend.ID)
	}

	if len(indicators) > 0 {
		return fmt.Sprintf("%s %s", name, strings.Join(indicators, " "))
	}

	return name
}

// getFriendFieldValue creates the field value for a friend entry.
func (b *FriendsBuilder) getFriendFieldValue(friend *apiTypes.ExtendedFriend) string {
	var info []string

	// Add presence details if available
	if presence, ok := b.presences[friend.ID]; ok {
		if presence.UserPresenceType != apiTypes.Offline {
			info = append(info, presence.LastLocation)
		} else if presence.LastOnline != nil {
			info = append(info, fmt.Sprintf("Last Online: <t:%d:R>", presence.LastOnline.Unix()))
		}
	}

	// Add confidence and reason if available
	if flaggedFriend, ok := b.flaggedFriends[friend.ID]; ok && len(flaggedFriend.Reasons) > 0 {
		reasonTypes := flaggedFriend.Reasons.Types()
		info = append(info, fmt.Sprintf("(%.2f) [%s]", flaggedFriend.Confidence, strings.Join(reasonTypes, ", ")))
	}

	return strings.Join(info, " • ")
}
