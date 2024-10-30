package builders

import (
	"bytes"
	"strconv"

	"github.com/disgoorg/disgo/discord"
	"github.com/rotector/rotector/internal/bot/constants"
)

// DashboardBuilder is the builder for the dashboard.
type DashboardBuilder struct {
	confirmedCount int
	flaggedCount   int
	clearedCount   int
	statsChart     *bytes.Buffer
}

// NewDashboardBuilder creates a new DashboardBuilder.
func NewDashboardBuilder(confirmedCount, flaggedCount, clearedCount int, statsChart *bytes.Buffer) *DashboardBuilder {
	return &DashboardBuilder{
		confirmedCount: confirmedCount,
		flaggedCount:   flaggedCount,
		clearedCount:   clearedCount,
		statsChart:     statsChart,
	}
}

// Build builds the dashboard.
func (b *DashboardBuilder) Build() *discord.MessageUpdateBuilder {
	embed := discord.NewEmbedBuilder().
		AddField("Confirmed Users", strconv.Itoa(b.confirmedCount), true).
		AddField("Flagged Users", strconv.Itoa(b.flaggedCount), true).
		AddField("Cleared Users", strconv.Itoa(b.clearedCount), true).
		SetColor(constants.DefaultEmbedColor)

	// Add stats chart if available
	if b.statsChart != nil {
		embed.SetImage("attachment://stats_chart.png")
	}

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

	builder := discord.NewMessageUpdateBuilder().
		SetEmbeds(embed.Build()).
		AddContainerComponents(components...)

	// Attach stats chart if available
	if b.statsChart != nil {
		builder.SetFiles(discord.NewFile("stats_chart.png", "", b.statsChart))
	}

	return builder
}
