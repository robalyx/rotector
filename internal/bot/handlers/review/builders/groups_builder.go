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

// GroupsEmbed builds the embed for the groups viewer message.
type GroupsEmbed struct {
	user          *database.FlaggedUser
	groups        []types.UserGroupRoles
	flaggedGroups map[uint64]bool
	start         int
	page          int
	total         int
	file          *bytes.Buffer
	streamerMode  bool
}

// NewGroupsEmbed creates a new GroupsEmbed.
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
		file:          s.GetBuffer(constants.SessionKeyFile),
		streamerMode:  s.GetBool(constants.SessionKeyStreamerMode),
	}
}

// Build constructs and returns the discord.Embed.
func (b *GroupsEmbed) Build() *discord.MessageUpdateBuilder {
	totalPages := (b.total + constants.GroupsPerPage - 1) / constants.GroupsPerPage

	fileName := fmt.Sprintf("groups_%d_%d.png", b.user.ID, b.page)
	file := discord.NewFile(fileName, "", bytes.NewReader(b.file.Bytes()))

	embed := discord.NewEmbedBuilder().
		SetTitle(fmt.Sprintf("User Groups (Page %d/%d)", b.page+1, totalPages)).
		SetDescription(fmt.Sprintf("```%s (%d)```", utils.CensorString(b.user.Name, b.streamerMode), b.user.ID)).
		SetImage("attachment://" + fileName).
		SetColor(utils.GetMessageEmbedColor(b.streamerMode))

	components := []discord.ContainerComponent{
		discord.NewActionRow(
			discord.NewSecondaryButton("◀️", string(constants.BackButtonCustomID)),
			discord.NewSecondaryButton("⏮️", string(utils.ViewerFirstPage)).WithDisabled(b.page == 0),
			discord.NewSecondaryButton("◀️", string(utils.ViewerPrevPage)).WithDisabled(b.page == 0),
			discord.NewSecondaryButton("▶️", string(utils.ViewerNextPage)).WithDisabled(b.page == totalPages-1),
			discord.NewSecondaryButton("⏭️", string(utils.ViewerLastPage)).WithDisabled(b.page == totalPages-1),
		),
	}

	for i, group := range b.groups {
		fieldName := fmt.Sprintf("Group %d", b.start+i+1)
		fieldValue := fmt.Sprintf(
			"[%s](https://www.roblox.com/groups/%d)\n(%s)",
			utils.CensorString(group.Group.Name, b.streamerMode),
			group.Group.ID,
			group.Role.Name,
		)

		// Add flagged status if needed
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
