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
	user          *types.FlaggedUser
	groups        []apiTypes.UserGroupRoles
	flaggedGroups map[uint64]bool
	start         int
	page          int
	total         int
	imageBuffer   *bytes.Buffer
}

// NewGroupsBuilder creates a new groups builder.
func NewGroupsBuilder(s *session.Session) *GroupsBuilder {
	var settings *types.UserSetting
	s.GetInterface(constants.SessionKeyUserSettings, &settings)
	var user *types.FlaggedUser
	s.GetInterface(constants.SessionKeyTarget, &user)
	var groups []apiTypes.UserGroupRoles
	s.GetInterface(constants.SessionKeyGroups, &groups)
	var flaggedGroups map[uint64]bool
	s.GetInterface(constants.SessionKeyFlaggedGroups, &flaggedGroups)

	return &GroupsBuilder{
		settings:      settings,
		user:          user,
		groups:        groups,
		flaggedGroups: flaggedGroups,
		start:         s.GetInt(constants.SessionKeyStart),
		page:          s.GetInt(constants.SessionKeyPaginationPage),
		total:         s.GetInt(constants.SessionKeyTotalItems),
		imageBuffer:   s.GetBuffer(constants.SessionKeyImageBuffer),
	}
}

// Build creates a Discord message with a grid of group thumbnails and information.
// Each group entry shows:
// - Group name (with link to group page)
// - Indicators for group, verification, privacy, flagged status
// - Description (if available)
// - Owner information
// - User's role in the group
// Navigation buttons are disabled when at the start/end of the list.
func (b *GroupsBuilder) Build() *discord.MessageUpdateBuilder {
	totalPages := (b.total + constants.GroupsPerPage - 1) / constants.GroupsPerPage

	// Create file attachment for the group thumbnails grid
	fileName := fmt.Sprintf("groups_%d_%d.png", b.user.ID, b.page)
	file := discord.NewFile(fileName, "", b.imageBuffer)

	// Build embed with user info and thumbnails
	censor := b.settings.StreamerMode || b.settings.ReviewMode == types.TrainingReviewMode
	embed := discord.NewEmbedBuilder().
		SetTitle(fmt.Sprintf("User Groups (Page %d/%d)", b.page+1, totalPages)).
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

	// Add fields for each group on the current page
	for i, group := range b.groups {
		fieldName := fmt.Sprintf("Group %d", b.start+i+1)

		// Add warning indicator for flagged groups
		if b.flaggedGroups[group.Group.ID] {
			fieldName += " ⚠️"
		}

		// Add verification badge if group is verified
		if group.Group.HasVerifiedBadge {
			fieldName += " ✓"
		}

		// Format group information based on mode
		var info string
		if b.settings.ReviewMode == types.TrainingReviewMode {
			info = utils.CensorString(group.Group.Name, true) + "\n"
		} else {
			info = fmt.Sprintf(
				"[%s](https://www.roblox.com/groups/%d)\n",
				utils.CensorString(group.Group.Name, b.settings.StreamerMode),
				group.Group.ID,
			)
		}

		// Add member count and role
		info += fmt.Sprintf("👥 `%s` • 👤 `%s`\n",
			utils.FormatNumber(group.Group.MemberCount),
			group.Role.Name,
		)

		// Add owner information based on mode
		if b.settings.ReviewMode == types.TrainingReviewMode {
			info += fmt.Sprintf("👑 Owner: %s\n",
				utils.CensorString(group.Group.Owner.Username, true))
		} else {
			info += fmt.Sprintf("👑 Owner: [%s](https://www.roblox.com/users/%d/profile)\n",
				utils.CensorString(group.Group.Owner.Username, b.settings.StreamerMode),
				group.Group.Owner.UserID)
		}

		// Add group status indicators
		var status []string
		if group.Group.IsLocked != nil && *group.Group.IsLocked {
			status = append(status, "🔒 Locked")
		}
		if !group.Group.PublicEntryAllowed {
			status = append(status, "🚫 Private")
		}
		if len(status) > 0 {
			info += strings.Join(status, " • ") + "\n"
		}

		// Add description if available
		if group.Group.Description != "" {
			desc := utils.NormalizeString(group.Group.Description)
			if len(desc) > 500 {
				desc = desc[:497] + "..."
			}
			info += fmt.Sprintf("```%s```", desc)
		}

		embed.AddField(fieldName, info, false)
	}

	return discord.NewMessageUpdateBuilder().
		SetEmbeds(embed.Build()).
		AddContainerComponents(components...).
		SetFiles(file)
}
