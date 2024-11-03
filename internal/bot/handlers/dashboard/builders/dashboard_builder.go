package builders

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/snowflake/v2"
	"github.com/rotector/rotector/internal/bot/constants"
)

// DashboardBuilder creates the visual layout for the main dashboard.
type DashboardBuilder struct {
	confirmedCount int
	flaggedCount   int
	clearedCount   int
	imageBuffer    *bytes.Buffer
	activeUsers    []snowflake.ID
}

// NewDashboardBuilder loads current statistics and active user information
// to create a new builder.
func NewDashboardBuilder(confirmedCount, flaggedCount, clearedCount int, imageBuffer *bytes.Buffer, activeUsers []snowflake.ID) *DashboardBuilder {
	return &DashboardBuilder{
		confirmedCount: confirmedCount,
		flaggedCount:   flaggedCount,
		clearedCount:   clearedCount,
		imageBuffer:    imageBuffer,
		activeUsers:    activeUsers,
	}
}

// Build creates a Discord message showing:
// - Current user counts (confirmed, flagged, cleared)
// - List of active reviewers with mentions
// - Statistics chart (if available)
// - Navigation menu to different sections.
func (b *DashboardBuilder) Build() *discord.MessageUpdateBuilder {
	// Create embed with user statistics
	embed := discord.NewEmbedBuilder().
		SetTitle("Welcome to Rotector 👋").
		AddField("Confirmed Users", strconv.Itoa(b.confirmedCount), true).
		AddField("Flagged Users", strconv.Itoa(b.flaggedCount), true).
		AddField("Cleared Users", strconv.Itoa(b.clearedCount), true).
		SetColor(constants.DefaultEmbedColor)

	// Add active reviewers field if any are online
	if len(b.activeUsers) > 0 {
		activeUserMentions := make([]string, len(b.activeUsers))
		for i, userID := range b.activeUsers {
			activeUserMentions[i] = fmt.Sprintf("<@%d>", userID)
		}
		embed.AddField("Active Reviewers", strings.Join(activeUserMentions, ", "), false)
	}

	// Add statistics chart if available
	if b.imageBuffer != nil {
		embed.SetImage("attachment://stats_chart.png")
	}

	// Add navigation menu and refresh button
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

	// Create message builder and attach components
	builder := discord.NewMessageUpdateBuilder().
		SetEmbeds(embed.Build()).
		AddContainerComponents(components...)

	// Attach statistics chart if available
	if b.imageBuffer != nil {
		builder.SetFiles(discord.NewFile("stats_chart.png", "", b.imageBuffer))
	}

	return builder
}
