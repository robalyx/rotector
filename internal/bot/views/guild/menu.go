package guild

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/disgoorg/disgo/discord"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/session"
)

// MenuBuilder creates the visual layout for the guild owner tools menu.
type MenuBuilder struct {
	guildName          string
	uniqueGuilds       int
	uniqueUsers        int
	inappropriateUsers int
}

// NewMenuBuilder creates a new menu builder.
func NewMenuBuilder(s *session.Session) *MenuBuilder {
	return &MenuBuilder{
		guildName:          session.GuildStatsName.Get(s),
		uniqueGuilds:       session.GuildStatsUniqueGuilds.Get(s),
		uniqueUsers:        session.GuildStatsUniqueUsers.Get(s),
		inappropriateUsers: session.GuildStatsInappropriateUsers.Get(s),
	}
}

// Build creates a Discord message with the guild owner tools.
func (b *MenuBuilder) Build() *discord.MessageUpdateBuilder {
	// Create main info container
	var mainContent strings.Builder
	mainContent.WriteString("## Guild Owner Tools\n")
	mainContent.WriteString("These tools help you maintain a safe server environment by managing ")
	mainContent.WriteString("Discord users in your server who may be participating in ERP (erotic roleplay) ")
	mainContent.WriteString("across multiple Discord servers.\n")

	if b.guildName != "" {
		mainContent.WriteString("### Current Guild\n" + b.guildName)
	}

	mainContainer := discord.NewContainer(
		discord.NewTextDisplay(mainContent.String()),
	).WithAccentColor(constants.DefaultContainerColor)

	// Create stats container
	var statsContent strings.Builder
	statsContent.WriteString("## Sync Statistics\n")
	statsContent.WriteString(fmt.Sprintf("**Tracked Discord Servers:** `%s`\n", strconv.Itoa(b.uniqueGuilds)))
	statsContent.WriteString(fmt.Sprintf("**Tracked Discord Users:** `%s`\n", strconv.Itoa(b.uniqueUsers)))
	statsContent.WriteString(fmt.Sprintf("**Inappropriate Discord Users:** `%s`", strconv.Itoa(b.inappropriateUsers)))

	statsContainer := discord.NewContainer(
		discord.NewTextDisplay(statsContent.String()),
	).WithAccentColor(constants.DefaultContainerColor)

	// Create action menu options
	options := []discord.StringSelectMenuOption{
		discord.NewStringSelectMenuOption("Ban Users in Condo Servers", constants.StartGuildScanButtonCustomID).
			WithEmoji(discord.ComponentEmoji{Name: "🔍"}).
			WithDescription("Scan and remove users who are members of flagged servers"),
		discord.NewStringSelectMenuOption("Ban Users with Inappropriate Messages", constants.StartMessageScanButtonCustomID).
			WithEmoji(discord.ComponentEmoji{Name: "💬"}).
			WithDescription("Scan and remove users who sent inappropriate messages (Recommended)"),
		discord.NewStringSelectMenuOption("View Ban Logs", constants.ViewGuildBanLogsButtonCustomID).
			WithEmoji(discord.ComponentEmoji{Name: "📜"}).
			WithDescription("View history of ban operations"),
	}

	// Create interactive components
	components := []discord.LayoutComponent{
		discord.NewActionRow(
			discord.NewStringSelectMenu(constants.ActionSelectMenuCustomID, "Select an action", options...),
		),
		discord.NewActionRow(
			discord.NewSecondaryButton("◀️ Back", constants.BackButtonCustomID),
		),
	}

	return discord.NewMessageUpdateBuilder().
		AddComponents(mainContainer, statsContainer).
		AddComponents(components...)
}
