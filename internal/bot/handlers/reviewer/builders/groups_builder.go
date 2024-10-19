package builders

import (
	"fmt"

	"github.com/disgoorg/disgo/discord"
	"github.com/jaxron/roapi.go/pkg/api/types"
	"github.com/rotector/rotector/internal/common/database"
)

// GroupsEmbed builds the embed for the groups viewer message.
type GroupsEmbed struct {
	user          *database.PendingUser
	groups        []types.UserGroupRolesResponse
	flaggedGroups map[uint64]bool
	start         int
	page          int
	total         int
	file          *discord.File
	fileName      string
}

// NewGroupsEmbed creates a new GroupsEmbed.
func NewGroupsEmbed(user *database.PendingUser, groups []types.UserGroupRolesResponse, flaggedGroups map[uint64]bool, start, page, total int, file *discord.File, fileName string) *GroupsEmbed {
	return &GroupsEmbed{
		user:          user,
		groups:        groups,
		flaggedGroups: flaggedGroups,
		start:         start,
		page:          page,
		total:         total,
		file:          file,
		fileName:      fileName,
	}
}

// Build constructs and returns the discord.Embed.
func (b *GroupsEmbed) Build() *discord.MessageUpdateBuilder {
	embed := discord.NewEmbedBuilder().
		SetTitle(fmt.Sprintf("User Groups (Page %d/%d)", b.page+1, b.total)).
		SetDescription(fmt.Sprintf("```%s (%d)```", b.user.Name, b.user.ID)).
		SetImage("attachment://" + b.fileName).
		SetColor(0x312D2B)

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
		fieldValue := fmt.Sprintf("[%s](https://www.roblox.com/groups/%d)\n(%s)", group.Group.Name, group.Group.ID, group.Role.Name)

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
