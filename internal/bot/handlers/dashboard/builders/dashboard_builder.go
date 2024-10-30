package builders

import (
	"strconv"

	"github.com/disgoorg/disgo/discord"
	"github.com/rotector/rotector/internal/bot/constants"
)

// DashboardBuilder is the builder for the dashboard.
type DashboardBuilder struct {
	flaggedCount   int
	confirmedCount int
}

// NewDashboardBuilder creates a new DashboardBuilder.
func NewDashboardBuilder(flaggedCount, confirmedCount int) *DashboardBuilder {
	return &DashboardBuilder{
		flaggedCount:   flaggedCount,
		confirmedCount: confirmedCount,
	}
}

// Build builds the dashboard.
func (b *DashboardBuilder) Build() *discord.MessageUpdateBuilder {
	embed := discord.NewEmbedBuilder().
		AddField("Flagged Users", strconv.Itoa(b.flaggedCount), true).
		AddField("Confirmed Users", strconv.Itoa(b.confirmedCount), true).
		SetColor(constants.DefaultEmbedColor).
		Build()

	components := []discord.ContainerComponent{
		discord.NewActionRow(
			discord.NewStringSelectMenu(constants.ActionSelectMenuCustomID, "Select an action",
				discord.NewStringSelectMenuOption("Review Flagged Users", constants.StartReviewCustomID).WithEmoji(discord.ComponentEmoji{Name: "🔍"}),
				discord.NewStringSelectMenuOption("Log Query Browser", constants.LogQueryBrowserCustomID).WithEmoji(discord.ComponentEmoji{Name: "📜"}),
				discord.NewStringSelectMenuOption("Queue Manager", constants.QueueManagerCustomID).WithEmoji(discord.ComponentEmoji{Name: "📋"}),
				discord.NewStringSelectMenuOption("User Settings", constants.UserSettingsCustomID).WithEmoji(discord.ComponentEmoji{Name: "👤"}),
				discord.NewStringSelectMenuOption("Guild Settings", constants.GuildSettingsCustomID).WithEmoji(discord.ComponentEmoji{Name: "⚙️"}),
			),
		),
		discord.NewActionRow(
			discord.NewSecondaryButton("🔄", string(constants.RefreshButtonCustomID)),
		),
	}

	return discord.NewMessageUpdateBuilder().
		SetEmbeds(embed).
		AddContainerComponents(components...)
}
