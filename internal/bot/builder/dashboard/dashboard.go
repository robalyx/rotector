package dashboard

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/snowflake/v2"
	"github.com/rotector/rotector/internal/bot/constants"
	"github.com/rotector/rotector/internal/common/storage/database/models"
	"github.com/rotector/rotector/internal/worker/core"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

const (
	healthyEmoji   = "🟢" // Green circle for healthy workers
	unhealthyEmoji = "🔴" // Red circle for unhealthy workers
	staleEmoji     = "⚫" // Black circle for stale/offline workers
)

// Builder creates the visual layout for the main dashboard.
type Builder struct {
	botSettings    *models.BotSetting
	userID         uint64
	confirmedCount int
	flaggedCount   int
	clearedCount   int
	imageBuffer    *bytes.Buffer
	activeUsers    []snowflake.ID
	workerStatuses []core.Status
	titleCaser     cases.Caser
}

// NewBuilder creates a new dashboard builder.
func NewBuilder(
	botSettings *models.BotSetting,
	userID uint64,
	confirmedCount, flaggedCount, clearedCount int,
	imageBuffer *bytes.Buffer,
	activeUsers []snowflake.ID,
	workerStatuses []core.Status,
) *Builder {
	return &Builder{
		botSettings:    botSettings,
		userID:         userID,
		confirmedCount: confirmedCount,
		flaggedCount:   flaggedCount,
		clearedCount:   clearedCount,
		imageBuffer:    imageBuffer,
		activeUsers:    activeUsers,
		workerStatuses: workerStatuses,
		titleCaser:     cases.Title(language.English),
	}
}

// Build creates a Discord message showing statistics and worker status.
func (b *Builder) Build() *discord.MessageUpdateBuilder {
	// Create base options
	options := []discord.StringSelectMenuOption{
		discord.NewStringSelectMenuOption("Review Users", constants.StartUserReviewCustomID).
			WithEmoji(discord.ComponentEmoji{Name: "🔍"}).
			WithDescription("Start reviewing flagged users"),
		discord.NewStringSelectMenuOption("Review Groups", constants.StartGroupReviewCustomID).
			WithEmoji(discord.ComponentEmoji{Name: "🔍"}).
			WithDescription("Start reviewing flagged groups"),
	}

	// Add activity log and queue manager options only for reviewers
	if b.botSettings.IsReviewer(b.userID) {
		options = append(options,
			discord.NewStringSelectMenuOption("Activity Log Browser", constants.LogActivityBrowserCustomID).
				WithEmoji(discord.ComponentEmoji{Name: "📜"}).
				WithDescription("Search and filter activity logs"),
			discord.NewStringSelectMenuOption("User Queue Manager", constants.QueueManagerCustomID).
				WithEmoji(discord.ComponentEmoji{Name: "📋"}).
				WithDescription("Manage user recheck queue priorities"),
		)
	}

	// Add user settings option
	options = append(options,
		discord.NewStringSelectMenuOption("User Settings", constants.UserSettingsCustomID).
			WithEmoji(discord.ComponentEmoji{Name: "👤"}).
			WithDescription("Configure your personal settings"),
	)

	// Add bot settings option only for admins
	if b.botSettings.IsAdmin(b.userID) {
		options = append(options, discord.NewStringSelectMenuOption("Bot Settings", constants.BotSettingsCustomID).
			WithEmoji(discord.ComponentEmoji{Name: "⚙️"}).
			WithDescription("Configure bot-wide settings"))
	}

	// Create message builder with both embeds
	builder := discord.NewMessageUpdateBuilder().
		SetEmbeds(b.buildStatsEmbed(), b.buildWorkerStatusEmbed()).
		AddContainerComponents(
			discord.NewActionRow(
				discord.NewStringSelectMenu(constants.ActionSelectMenuCustomID, "Select an action", options...),
			),
			discord.NewActionRow(
				discord.NewSecondaryButton("🔄", string(constants.RefreshButtonCustomID)),
			),
		)

	// Attach statistics chart if available
	if b.imageBuffer != nil {
		builder.SetFiles(discord.NewFile("stats_chart.png", "", b.imageBuffer))
	}

	return builder
}

// buildStatsEmbed creates the main statistics embed.
func (b *Builder) buildStatsEmbed() discord.Embed {
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

	return embed.Build()
}

// buildWorkerStatusEmbed creates the worker status monitoring embed.
func (b *Builder) buildWorkerStatusEmbed() discord.Embed {
	embed := discord.NewEmbedBuilder().
		SetTitle("Worker Statuses").
		SetDescription(fmt.Sprintf("%s Online  %s Unhealthy  %s Offline", healthyEmoji, unhealthyEmoji, staleEmoji)).
		SetColor(constants.DefaultEmbedColor)

	// Group workers by type and subtype
	workerGroups := b.groupWorkers()

	// Add fields for each worker type
	for workerType, subtypes := range workerGroups {
		for subType, workers := range subtypes {
			// Format worker statuses
			var statusLines []string
			for _, w := range workers {
				shortID := w.WorkerID[:8]
				emoji := b.getStatusEmoji(w)
				statusLines = append(statusLines, fmt.Sprintf("%s `%s` %s (%d%%)",
					emoji, shortID, w.CurrentTask, w.Progress))
			}

			// Add field for this worker type
			fieldName := fmt.Sprintf("%s %s",
				b.titleCaser.String(workerType),
				b.titleCaser.String(subType),
			)
			fieldValue := "No workers online"
			if len(statusLines) > 0 {
				fieldValue = strings.Join(statusLines, "\n")
			}
			embed.AddField(fieldName, fieldValue, false)
		}
	}

	return embed.Build()
}

// groupWorkers organizes workers by type and subtype.
func (b *Builder) groupWorkers() map[string]map[string][]core.Status {
	groups := make(map[string]map[string][]core.Status)

	for _, status := range b.workerStatuses {
		// Initialize maps
		if _, ok := groups[status.WorkerType]; !ok {
			groups[status.WorkerType] = make(map[string][]core.Status)
		}

		// Add worker to appropriate group
		groups[status.WorkerType][status.SubType] = append(
			groups[status.WorkerType][status.SubType],
			status,
		)
	}

	return groups
}

// getStatusEmoji returns the appropriate emoji for a worker's status.
func (b *Builder) getStatusEmoji(status core.Status) string {
	// Check if worker is stale first (last seen > StaleThreshold)
	if time.Since(status.LastSeen) > core.StaleThreshold {
		return staleEmoji
	}

	// If worker is not stale, show health status
	if !status.IsHealthy {
		return unhealthyEmoji
	}

	return healthyEmoji
}
