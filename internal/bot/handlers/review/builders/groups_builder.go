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

// GroupsEmbed creates the visual layout for viewing a user's groups.
// It combines group information with flagged status indicators and
// supports pagination through a grid of group thumbnails.
type GroupsEmbed struct {
	user          *database.FlaggedUser
	groups        []types.UserGroupRoles
	flaggedGroups map[uint64]bool
	start         int
	page          int
	total         int
	imageBuffer   *bytes.Buffer
	streamerMode  bool
}

// NewGroupsEmbed loads group data and settings from the session state
// to create a new embed builder.
func NewGroupsEmbed(s *session.Session) *GroupsEmbed {
	var user *database.FlaggedUser
	s.GetInterface(constants.SessionKeyTarget, &user)
	var groups []types.UserGroupRoles
	s.GetInterface(constants.SessionKeyGroups, &groups)
	var flaggedGroups map[uint64]bool
	s.GetInterface(constants.SessionKeyFlaggedGroups, &flaggedGroups)

	return &GroupsEmbed{
		user:          user,
		groups:        groups,
		flaggedGroups: flaggedGroups,
		start:         s.GetInt(constants.SessionKeyStart),
		page:          s.GetInt(constants.SessionKeyPaginationPage),
		total:         s.GetInt(constants.SessionKeyTotalItems),
		imageBuffer:   s.GetBuffer(constants.SessionKeyImageBuffer),
		streamerMode:  s.GetBool(constants.SessionKeyStreamerMode),
	}
}

// Build creates a Discord message with a grid of group thumbnails and information.
// Each group entry shows:
// - Group name (with link to group page)
// - User's role in the group
// - Warning indicator if the group is flagged
// Navigation buttons are disabled when at the start/end of the list.
func (b *GroupsEmbed) Build() *discord.MessageUpdateBuilder {
	totalPages := (b.total + constants.GroupsPerPage - 1) / constants.GroupsPerPage

	// Create file attachment for the group thumbnails grid
	fileName := fmt.Sprintf("groups_%d_%d.png", b.user.ID, b.page)
	file := discord.NewFile(fileName, "", b.imageBuffer)

	// Build embed with user info and thumbnails
	embed := discord.NewEmbedBuilder().
		SetTitle(fmt.Sprintf("User Groups (Page %d/%d)", b.page+1, totalPages)).
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

	// Add fields for each group on the current page
	for i, group := range b.groups {
		fieldName := fmt.Sprintf("Group %d", b.start+i+1)
		fieldValue := fmt.Sprintf(
			"[%s](https://www.roblox.com/groups/%d)\n(%s)",
			utils.CensorString(group.Group.Name, b.streamerMode),
			group.Group.ID,
			group.Role.Name,
		)

		// Add warning indicator for flagged groups
		if b.flaggedGroups[group.Group.ID] {
			fieldName += " ⚠️"
		}

		embed.AddField(fieldName, fieldValue, true)
	}

	return discord.NewMessageUpdateBuilder().
		SetEmbeds(embed.Build()).
		AddContainerComponents(components...).
		SetFiles(file)
}
