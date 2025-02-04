package group

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

// MembersBuilder creates the visual layout for viewing a group's flagged members.
type MembersBuilder struct {
	group       *types.ReviewGroup
	pageMembers []uint64
	members     map[uint64]*types.ReviewUser
	presences   map[uint64]*apiTypes.UserPresenceResponse
	start       int
	page        int
	total       int
	imageBuffer *bytes.Buffer
	isStreaming bool
	privacyMode bool
}

// NewMembersBuilder creates a new members builder.
func NewMembersBuilder(s *session.Session) *MembersBuilder {
	return &MembersBuilder{
		group:       session.GroupTarget.Get(s),
		pageMembers: session.GroupPageMembers.Get(s),
		members:     session.GroupMembers.Get(s),
		presences:   session.UserPresences.Get(s),
		start:       session.PaginationOffset.Get(s),
		page:        session.PaginationPage.Get(s),
		total:       session.PaginationTotalItems.Get(s),
		imageBuffer: session.ImageBuffer.Get(s),
		isStreaming: session.PaginationIsStreaming.Get(s),
		privacyMode: session.UserReviewMode.Get(s) == enum.ReviewModeTraining || session.UserStreamerMode.Get(s),
	}
}

// Build creates a Discord message with a grid of member avatars and information.
func (b *MembersBuilder) Build() *discord.MessageUpdateBuilder {
	totalPages := (b.total + constants.MembersPerPage - 1) / constants.MembersPerPage

	// Create file attachment for the member avatars grid
	fileName := fmt.Sprintf("members_%d_%d.png", b.group.ID, b.page)
	file := discord.NewFile(fileName, "", b.imageBuffer)

	// Build base embed with group info
	embed := discord.NewEmbedBuilder().
		SetTitle(fmt.Sprintf("Group Members (Page %d/%d)", b.page+1, totalPages)).
		SetDescription(fmt.Sprintf(
			"```%s (%s)```",
			utils.CensorString(b.group.Name, b.privacyMode),
			utils.CensorString(strconv.FormatUint(b.group.ID, 10), b.privacyMode),
		)).
		SetImage("attachment://" + fileName).
		SetColor(utils.GetMessageEmbedColor(b.privacyMode))

	// Add fields for each member
	for i, memberID := range b.pageMembers {
		fieldName := b.getMemberFieldName(i, memberID)
		fieldValue := b.getMemberFieldValue(memberID)
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
				discord.NewSecondaryButton("⏮️", string(session.ViewerFirstPage)).WithDisabled(b.page == 0),
				discord.NewSecondaryButton("◀️", string(session.ViewerPrevPage)).WithDisabled(b.page == 0),
				discord.NewSecondaryButton("▶️", string(session.ViewerNextPage)).WithDisabled(b.page == totalPages-1),
				discord.NewSecondaryButton("⏭️", string(session.ViewerLastPage)).WithDisabled(b.page == totalPages-1),
			),
		}...)
	}

	return builder
}

// getMemberFieldName creates the field name for a member entry.
func (b *MembersBuilder) getMemberFieldName(index int, memberID uint64) string {
	fieldName := fmt.Sprintf("Member %d", b.start+index+1)

	// Add presence indicator
	if presence, ok := b.presences[memberID]; ok {
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

	// Add status indicator based on member status
	if member, ok := b.members[memberID]; ok {
		switch member.Status {
		case enum.UserTypeConfirmed:
			fieldName += " ⚠️"
		case enum.UserTypeFlagged:
			fieldName += " ⏳"
		case enum.UserTypeCleared:
			fieldName += " ✅"
		case enum.UserTypeUnflagged:
		}

		// Add banned status if applicable
		if member.IsBanned {
			fieldName += " 🔨"
		}
	}

	return fieldName
}

// getMemberFieldValue creates the field value for a member entry.
func (b *MembersBuilder) getMemberFieldValue(memberID uint64) string {
	var info strings.Builder
	member := b.members[memberID]

	// Add member name (with link in standard mode)
	memberName := member.Name
	if member.Status == enum.UserTypeUnflagged {
		memberName = "Unflagged"
	}

	name := utils.CensorString(memberName, b.privacyMode)
	if b.privacyMode {
		info.WriteString(name)
	} else {
		info.WriteString(fmt.Sprintf(
			"[%s](https://www.roblox.com/users/%d/profile)",
			name,
			member.ID,
		))
	}

	// Add presence details if available
	if presence, ok := b.presences[memberID]; ok {
		if presence.UserPresenceType != apiTypes.Offline {
			info.WriteString("\n" + presence.LastLocation)
		} else {
			info.WriteString(fmt.Sprintf("\nLast Online: <t:%d:R>", presence.LastOnline.Unix()))
		}
	}

	// Add reason and confidence if available
	if len(member.Reasons) > 0 {
		reasonTypes := member.Reasons.Types()
		info.WriteString(fmt.Sprintf("\n(%.2f) [%s]", member.Confidence, strings.Join(reasonTypes, ", ")))
	}

	return info.String()
}
