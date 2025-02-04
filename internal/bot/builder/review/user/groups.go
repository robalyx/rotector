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

// GroupsBuilder creates the visual layout for viewing a user's groups.
type GroupsBuilder struct {
	user          *types.ReviewUser
	groups        []*apiTypes.UserGroupRoles
	flaggedGroups map[uint64]*types.ReviewGroup
	start         int
	page          int
	total         int
	imageBuffer   *bytes.Buffer
	isStreaming   bool
	privacyMode   bool
}

// NewGroupsBuilder creates a new groups builder.
func NewGroupsBuilder(s *session.Session) *GroupsBuilder {
	return &GroupsBuilder{
		user:          session.UserTarget.Get(s),
		groups:        session.UserGroups.Get(s),
		flaggedGroups: session.UserFlaggedGroups.Get(s),
		start:         session.PaginationOffset.Get(s),
		page:          session.PaginationPage.Get(s),
		total:         session.PaginationTotalItems.Get(s),
		imageBuffer:   session.ImageBuffer.Get(s),
		isStreaming:   session.PaginationIsStreaming.Get(s),
		privacyMode:   session.UserReviewMode.Get(s) == enum.ReviewModeTraining || session.UserStreamerMode.Get(s),
	}
}

// Build creates a Discord message with a grid of group thumbnails and information.
func (b *GroupsBuilder) Build() *discord.MessageUpdateBuilder {
	totalPages := (b.total + constants.GroupsPerPage - 1) / constants.GroupsPerPage

	// Create file attachment for the group thumbnails grid
	fileName := fmt.Sprintf("groups_%d_%d.png", b.user.ID, b.page)
	file := discord.NewFile(fileName, "", b.imageBuffer)

	// Build base embed with user info
	embed := discord.NewEmbedBuilder().
		SetTitle(fmt.Sprintf("User Groups (Page %d/%d)", b.page+1, totalPages)).
		SetDescription(fmt.Sprintf(
			"```%s (%s)```",
			utils.CensorString(b.user.Name, b.privacyMode),
			utils.CensorString(strconv.FormatUint(b.user.ID, 10), b.privacyMode),
		)).
		SetImage("attachment://" + fileName).
		SetColor(utils.GetMessageEmbedColor(b.privacyMode))

	// Add fields for each group
	for i, group := range b.groups {
		fieldName := b.getGroupFieldName(i, group)
		fieldValue := b.getGroupFieldValue(group)
		embed.AddField(fieldName, fieldValue, false)
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

// getGroupFieldName creates the field name for a group entry.
func (b *GroupsBuilder) getGroupFieldName(index int, group *apiTypes.UserGroupRoles) string {
	fieldName := fmt.Sprintf("Group %d", b.start+index+1)

	// Add status indicator based on group status
	if reviewGroup, ok := b.flaggedGroups[group.Group.ID]; ok {
		switch reviewGroup.Status {
		case enum.GroupTypeConfirmed:
			fieldName += " ⚠️"
		case enum.GroupTypeFlagged:
			fieldName += " ⏳"
		case enum.GroupTypeCleared:
			fieldName += " ✅"
		case enum.GroupTypeUnflagged:
		}

		// Add locked status if applicable
		if reviewGroup.IsLocked {
			fieldName += " 🔒"
		}
	}

	// Add verification badge if group is verified
	if group.Group.HasVerifiedBadge {
		fieldName += " ✓"
	}

	return fieldName
}

// getGroupFieldValue creates the field value for a group entry.
func (b *GroupsBuilder) getGroupFieldValue(group *apiTypes.UserGroupRoles) string {
	var info strings.Builder

	// Add group name (with link in standard mode)
	name := utils.CensorString(group.Group.Name, b.privacyMode)
	if b.privacyMode {
		info.WriteString(name + "\n")
	} else {
		info.WriteString(fmt.Sprintf(
			"[%s](https://www.roblox.com/groups/%d)\n",
			name,
			group.Group.ID,
		))
	}

	// Add member count and user's role
	info.WriteString(fmt.Sprintf("👥 `%s` • 👤 `%s`\n",
		utils.FormatNumber(group.Group.MemberCount),
		group.Role.Name,
	))

	// Add owner info (with link in standard mode)
	username := utils.CensorString(group.Group.Owner.Username, b.privacyMode)
	if b.privacyMode {
		info.WriteString(fmt.Sprintf("👑 Owner: %s\n", username))
	} else {
		info.WriteString(fmt.Sprintf("👑 Owner: [%s](https://www.roblox.com/users/%d/profile)\n",
			username,
			group.Group.Owner.UserID))
	}

	// Add status indicators (locked, private)
	var status []string
	if group.Group.IsLocked != nil && *group.Group.IsLocked {
		status = append(status, "🔒 Locked")
	}
	if !group.Group.PublicEntryAllowed {
		status = append(status, "🚫 Private")
	}
	if len(status) > 0 {
		info.WriteString(strings.Join(status, " • ") + "\n")
	}

	// Add flagged info and confidence if available
	if flaggedGroup, ok := b.flaggedGroups[group.Group.ID]; ok {
		if flaggedGroup.Confidence > 0 {
			info.WriteString(fmt.Sprintf("🔮 Confidence: `%.2f`\n", flaggedGroup.Confidence))
		}
		if len(flaggedGroup.Reasons) > 0 {
			for reasonType, reason := range flaggedGroup.Reasons {
				info.WriteString(fmt.Sprintf("```[%s] %s```\n", reasonType, reason.Message))
			}
		}
	}

	// Add truncated description if available
	if group.Group.Description != "" {
		desc := utils.NormalizeString(group.Group.Description)
		if len(desc) > 500 {
			desc = desc[:497] + "..."
		}
		info.WriteString(fmt.Sprintf("```%s```", desc))
	}

	return info.String()
}
