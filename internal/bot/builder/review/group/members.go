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
)

// MembersBuilder creates the visual layout for viewing a group's flagged members.
type MembersBuilder struct {
	settings    *types.UserSetting
	group       *types.ReviewGroup
	pageMembers []uint64
	members     map[uint64]*types.ReviewUser
	presences   map[uint64]*apiTypes.UserPresenceResponse
	start       int
	page        int
	total       int
	imageBuffer *bytes.Buffer
	isStreaming bool
}

// NewMembersBuilder creates a new members builder.
func NewMembersBuilder(s *session.Session) *MembersBuilder {
	var settings *types.UserSetting
	s.GetInterface(constants.SessionKeyUserSettings, &settings)
	var group *types.ReviewGroup
	s.GetInterface(constants.SessionKeyGroupTarget, &group)
	var pageMembers []uint64
	s.GetInterface(constants.SessionKeyGroupPageMembers, &pageMembers)
	var members map[uint64]*types.ReviewUser
	s.GetInterface(constants.SessionKeyGroupMembers, &members)
	var presences map[uint64]*apiTypes.UserPresenceResponse
	s.GetInterface(constants.SessionKeyPresences, &presences)

	return &MembersBuilder{
		settings:    settings,
		group:       group,
		pageMembers: pageMembers,
		members:     members,
		presences:   presences,
		start:       s.GetInt(constants.SessionKeyStart),
		page:        s.GetInt(constants.SessionKeyPaginationPage),
		total:       s.GetInt(constants.SessionKeyTotalItems),
		imageBuffer: s.GetBuffer(constants.SessionKeyImageBuffer),
		isStreaming: s.GetBool(constants.SessionKeyIsStreaming),
	}
}

// Build creates a Discord message with a grid of member avatars and information.
func (b *MembersBuilder) Build() *discord.MessageUpdateBuilder {
	totalPages := (b.total + constants.MembersPerPage - 1) / constants.MembersPerPage
	censor := b.settings.StreamerMode || b.settings.ReviewMode == types.TrainingReviewMode

	// Create file attachment for the member avatars grid
	fileName := fmt.Sprintf("members_%d_%d.png", b.group.ID, b.page)
	file := discord.NewFile(fileName, "", b.imageBuffer)

	// Build base embed with group info
	embed := discord.NewEmbedBuilder().
		SetTitle(fmt.Sprintf("Group Members (Page %d/%d)", b.page+1, totalPages)).
		SetDescription(fmt.Sprintf(
			"```%s (%s)```",
			utils.CensorString(b.group.Name, censor),
			utils.CensorString(strconv.FormatUint(b.group.ID, 10), censor),
		)).
		SetImage("attachment://" + fileName).
		SetColor(utils.GetMessageEmbedColor(b.settings.StreamerMode))

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
				discord.NewSecondaryButton("⏮️", string(utils.ViewerFirstPage)).WithDisabled(b.page == 0),
				discord.NewSecondaryButton("◀️", string(utils.ViewerPrevPage)).WithDisabled(b.page == 0),
				discord.NewSecondaryButton("▶️", string(utils.ViewerNextPage)).WithDisabled(b.page == totalPages-1),
				discord.NewSecondaryButton("⏭️", string(utils.ViewerLastPage)).WithDisabled(b.page == totalPages-1),
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

// getMemberFieldValue creates the field value for a member entry.
func (b *MembersBuilder) getMemberFieldValue(memberID uint64) string {
	var info strings.Builder
	member := b.members[memberID]

	// Add member name (with link in standard mode)
	memberName := member.Name
	if member.Status == types.UserTypeUnflagged {
		memberName = "Unflagged"
	}

	if b.settings.ReviewMode == types.TrainingReviewMode {
		info.WriteString(utils.CensorString(memberName, true))
	} else {
		info.WriteString(fmt.Sprintf(
			"[%s](https://www.roblox.com/users/%d/profile)",
			utils.CensorString(memberName, b.settings.StreamerMode),
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
	if member.Confidence > 0 {
		info.WriteString(fmt.Sprintf(" (%.2f)", member.Confidence))
	}

	if member.Reason != "" {
		censored := utils.CensorStringsInText(
			member.Reason,
			b.settings.StreamerMode,
			strconv.FormatUint(b.group.ID, 10),
			b.group.Name,
			strconv.FormatUint(member.ID, 10),
			member.Name,
			member.DisplayName,
		)
		info.WriteString(fmt.Sprintf("\n```%s```", censored))
	}

	return info.String()
}
