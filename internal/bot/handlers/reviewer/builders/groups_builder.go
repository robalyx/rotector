package builders

import (
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
	file          *discord.File
	fileName      string
	streamerMode  bool
}

// NewGroupsEmbed creates a new GroupsEmbed.
func NewGroupsEmbed(s *session.Session) *GroupsEmbed {
	return &GroupsEmbed{
		user:          s.GetFlaggedUser(constants.KeyTarget),
		groups:        s.Get(constants.SessionKeyGroups).([]types.UserGroupRoles),
		flaggedGroups: s.Get(constants.SessionKeyFlaggedGroups).(map[uint64]bool),
		start:         s.GetInt(constants.SessionKeyStart),
		page:          s.GetInt(constants.SessionKeyPage),
		total:         s.GetInt(constants.SessionKeyTotal),
		file:          s.Get(constants.SessionKeyFile).(*discord.File),
		fileName:      s.GetString(constants.SessionKeyFileName),
		streamerMode:  s.GetBool(constants.SessionKeyStreamerMode),
	}
}

// Build constructs and returns the discord.Embed.
func (b *GroupsEmbed) Build() *discord.MessageUpdateBuilder {
	embed := discord.NewEmbedBuilder().
		SetTitle(fmt.Sprintf("User Groups (Page %d/%d)", b.page+1, b.total)).
		SetDescription(fmt.Sprintf("```%s (%d)```", utils.CensorString(b.user.Name, b.streamerMode), b.user.ID)).
		SetImage("attachment://" + b.fileName).
		SetColor(utils.GetMessageEmbedColor(b.streamerMode))

	components := []discord.ContainerComponent{
		discord.NewActionRow(
			discord.NewSecondaryButton("◀️", string(ViewerBackToReview)),
			discord.NewSecondaryButton("⏮️", string(ViewerFirstPage)).WithDisabled(b.page == 0),
			discord.NewSecondaryButton("◀️", string(ViewerPrevPage)).WithDisabled(b.page == 0),
			discord.NewSecondaryButton("▶️", string(ViewerNextPage)).WithDisabled(b.page == b.total-1),
			discord.NewSecondaryButton("⏭️", string(ViewerLastPage)).WithDisabled(b.page == b.total-1),
		),
	}

	for i, group := range b.groups {
		fieldName := fmt.Sprintf("Group %d", b.start+i+1)
		fieldValue := fmt.Sprintf("[%s](https://www.roblox.com/groups/%d)\n(%s)", utils.CensorString(group.Group.Name, b.streamerMode), group.Group.ID, group.Role.Name)

		// Add flagged status if needed
		if b.flaggedGroups[group.Group.ID] {
			fieldName += " ⚠️"
		}

		embed.AddField(fieldName, fieldValue, true)
	}

	return discord.NewMessageUpdateBuilder().
		SetEmbeds(embed.Build()).
		AddContainerComponents(components...).
		SetFiles(b.file)
}
