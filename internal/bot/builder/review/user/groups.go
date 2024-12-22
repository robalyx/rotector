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

// GroupsBuilder creates the visual layout for viewing a user's groups.
// It combines group information with flagged status indicators and
// supports pagination through a grid of group thumbnails.
type GroupsBuilder struct {
	settings      *types.UserSetting
	user          *types.ReviewUser
	groups        []*apiTypes.UserGroupRoles
	groupTypes    map[uint64]types.GroupType
	flaggedGroups map[uint64]*types.Group
	start         int
	page          int
	total         int
	imageBuffer   *bytes.Buffer
	isStreaming   bool
}

// NewGroupsBuilder creates a new groups builder.
func NewGroupsBuilder(s *session.Session) *GroupsBuilder {
	var settings *types.UserSetting
	s.GetInterface(constants.SessionKeyUserSettings, &settings)
	var user *types.ReviewUser
	s.GetInterface(constants.SessionKeyTarget, &user)
	var groups []*apiTypes.UserGroupRoles
	s.GetInterface(constants.SessionKeyGroups, &groups)
	var groupTypes map[uint64]types.GroupType
	s.GetInterface(constants.SessionKeyGroupTypes, &groupTypes)
	var flaggedGroups map[uint64]*types.Group
	s.GetInterface(constants.SessionKeyFlaggedGroups, &flaggedGroups)

	return &GroupsBuilder{
		settings:      settings,
		user:          user,
		groups:        groups,
		groupTypes:    groupTypes,
		flaggedGroups: flaggedGroups,
		start:         s.GetInt(constants.SessionKeyStart),
		page:          s.GetInt(constants.SessionKeyPaginationPage),
		total:         s.GetInt(constants.SessionKeyTotalItems),
		imageBuffer:   s.GetBuffer(constants.SessionKeyImageBuffer),
		isStreaming:   s.GetBool(constants.SessionKeyIsStreaming),
	}
}

// Build creates a Discord message with a grid of group thumbnails and information.
func (b *GroupsBuilder) Build() *discord.MessageUpdateBuilder {
	totalPages := (b.total + constants.GroupsPerPage - 1) / constants.GroupsPerPage
	censor := b.settings.StreamerMode || b.settings.ReviewMode == types.TrainingReviewMode

	// Create file attachment for the group thumbnails grid
	fileName := fmt.Sprintf("groups_%d_%d.png", b.user.ID, b.page)
	file := discord.NewFile(fileName, "", b.imageBuffer)

	// Build base embed with user info
	embed := discord.NewEmbedBuilder().
		SetTitle(fmt.Sprintf("User Groups (Page %d/%d)", b.page+1, totalPages)).
		SetDescription(fmt.Sprintf(
			"```%s (%s)```",
			utils.CensorString(b.user.Name, censor),
			utils.CensorString(strconv.FormatUint(b.user.ID, 10), censor),
		)).
		SetImage("attachment://" + fileName).
		SetColor(utils.GetMessageEmbedColor(b.settings.StreamerMode))

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
				discord.NewSecondaryButton("⏮️", string(utils.ViewerFirstPage)).WithDisabled(b.page == 0),
				discord.NewSecondaryButton("◀️", string(utils.ViewerPrevPage)).WithDisabled(b.page == 0),
				discord.NewSecondaryButton("▶️", string(utils.ViewerNextPage)).WithDisabled(b.page == totalPages-1),
				discord.NewSecondaryButton("⏭️", string(utils.ViewerLastPage)).WithDisabled(b.page == totalPages-1),
			),
		}...)
	}

	return builder
}

// getGroupFieldName creates the field name for a group entry.
func (b *GroupsBuilder) getGroupFieldName(index int, group *apiTypes.UserGroupRoles) string {
	fieldName := fmt.Sprintf("Group %d", b.start+index+1)

	// Add status indicator based on group type
	if groupType, ok := b.groupTypes[group.Group.ID]; ok {
		switch groupType {
		case types.GroupTypeConfirmed:
			fieldName += " ⚠️"
		case types.GroupTypeFlagged:
			fieldName += " ⏳"
		case types.GroupTypeCleared:
			fieldName += " ✅"
		case types.GroupTypeLocked:
			fieldName += " 🔒"
		case types.GroupTypeUnflagged:
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
	if b.settings.ReviewMode == types.TrainingReviewMode {
		info.WriteString(utils.CensorString(group.Group.Name, true) + "\n")
	} else {
		info.WriteString(fmt.Sprintf(
			"[%s](https://www.roblox.com/groups/%d)\n",
			utils.CensorString(group.Group.Name, b.settings.StreamerMode),
			group.Group.ID,
		))
	}

	// Add member count and user's role
	info.WriteString(fmt.Sprintf("👥 `%s` • 👤 `%s`\n",
		utils.FormatNumber(group.Group.MemberCount),
		group.Role.Name,
	))

	// Add owner info (with link in standard mode)
	if b.settings.ReviewMode == types.TrainingReviewMode {
		info.WriteString(fmt.Sprintf("👑 Owner: %s\n",
			utils.CensorString(group.Group.Owner.Username, true)))
	} else {
		info.WriteString(fmt.Sprintf("👑 Owner: [%s](https://www.roblox.com/users/%d/profile)\n",
			utils.CensorString(group.Group.Owner.Username, b.settings.StreamerMode),
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
		if flaggedGroup.Reason != "" {
			info.WriteString(fmt.Sprintf("```%s```", flaggedGroup.Reason))
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
