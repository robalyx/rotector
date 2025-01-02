package dashboard

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/snowflake/v2"
	"github.com/redis/rueidis"
	"github.com/rotector/rotector/internal/bot/constants"
	"github.com/rotector/rotector/internal/bot/core/session"
	"github.com/rotector/rotector/internal/bot/utils"
	"github.com/rotector/rotector/internal/common/storage/database/types"
	"github.com/rotector/rotector/internal/worker/core"
	"github.com/rotector/rotector/internal/worker/stats"
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
	botSettings      *types.BotSetting
	userID           uint64
	userCounts       *types.UserCounts
	groupCounts      *types.GroupCounts
	userStatsBuffer  *bytes.Buffer
	groupStatsBuffer *bytes.Buffer
	activeUsers      []snowflake.ID
	workerStatuses   []core.Status
	titleCaser       cases.Caser
}

// NewBuilder creates a new dashboard builder.
func NewBuilder(s *session.Session, redisClient rueidis.Client) *Builder {
	var botSettings *types.BotSetting
	s.GetInterface(constants.SessionKeyBotSettings, &botSettings)
	var userCounts *types.UserCounts
	s.GetInterface(constants.SessionKeyUserCounts, &userCounts)
	var groupCounts *types.GroupCounts
	s.GetInterface(constants.SessionKeyGroupCounts, &groupCounts)
	var activeUsers []snowflake.ID
	s.GetInterface(constants.SessionKeyActiveUsers, &activeUsers)
	var workerStatuses []core.Status
	s.GetInterface(constants.SessionKeyWorkerStatuses, &workerStatuses)

	// Get chart buffers from Redis
	userStatsBuffer, groupStatsBuffer := getChartBuffers(redisClient)

	return &Builder{
		botSettings:      botSettings,
		userID:           s.UserID(),
		userCounts:       userCounts,
		groupCounts:      groupCounts,
		userStatsBuffer:  userStatsBuffer,
		groupStatsBuffer: groupStatsBuffer,
		activeUsers:      activeUsers,
		workerStatuses:   workerStatuses,
		titleCaser:       cases.Title(language.English),
	}
}

// getChartBuffers retrieves the cached chart buffers from Redis.
func getChartBuffers(client rueidis.Client) (*bytes.Buffer, *bytes.Buffer) {
	var userStatsChart, groupStatsChart *bytes.Buffer

	// Get user stats chart
	if result := client.Do(context.Background(), client.B().Get().Key(stats.UserStatsChartKey).Build()); result.Error() == nil {
		if data, err := result.AsBytes(); err == nil {
			if decoded, err := base64.StdEncoding.DecodeString(string(data)); err == nil {
				userStatsChart = bytes.NewBuffer(decoded)
			}
		}
	}

	// Get group stats chart
	if result := client.Do(context.Background(), client.B().Get().Key(stats.GroupStatsChartKey).Build()); result.Error() == nil {
		if data, err := result.AsBytes(); err == nil {
			if decoded, err := base64.StdEncoding.DecodeString(string(data)); err == nil {
				groupStatsChart = bytes.NewBuffer(decoded)
			}
		}
	}

	return userStatsChart, groupStatsChart
}

// Build creates a Discord message showing statistics and worker status.
func (b *Builder) Build() *discord.MessageUpdateBuilder {
	// Create base options
	options := []discord.StringSelectMenuOption{
		discord.NewStringSelectMenuOption("Review Users", constants.StartUserReviewButtonCustomID).
			WithEmoji(discord.ComponentEmoji{Name: "📝"}).
			WithDescription("Start reviewing flagged users"),
		discord.NewStringSelectMenuOption("Review Groups", constants.StartGroupReviewButtonCustomID).
			WithEmoji(discord.ComponentEmoji{Name: "📝"}).
			WithDescription("Start reviewing flagged groups"),
	}

	// Add reviewer-only options
	if b.botSettings.IsReviewer(b.userID) {
		options = append(options,
			discord.NewStringSelectMenuOption("Review Specific User", constants.ReviewUserButtonCustomID).
				WithEmoji(discord.ComponentEmoji{Name: "🎯"}).
				WithDescription("Review a specific user by ID or UUID"),
			discord.NewStringSelectMenuOption("Review Specific Group", constants.ReviewGroupButtonCustomID).
				WithEmoji(discord.ComponentEmoji{Name: "🎯"}).
				WithDescription("Review a specific group by ID or UUID"),
			discord.NewStringSelectMenuOption("AI Chat Assistant", constants.ChatAssistantButtonCustomID).
				WithEmoji(discord.ComponentEmoji{Name: "🤖"}).
				WithDescription("Chat with AI about moderation topics"),
			discord.NewStringSelectMenuOption("Activity Log Browser", constants.ActivityBrowserButtonCustomID).
				WithEmoji(discord.ComponentEmoji{Name: "📜"}).
				WithDescription("Search and filter activity logs"),
			discord.NewStringSelectMenuOption("User Queue Manager", constants.QueueManagerButtonCustomID).
				WithEmoji(discord.ComponentEmoji{Name: "📋"}).
				WithDescription("Manage user recheck queue priorities"),
		)
	}

	// Add other default options
	options = append(options,
		discord.NewStringSelectMenuOption("Lookup User", constants.LookupUserButtonCustomID).
			WithEmoji(discord.ComponentEmoji{Name: "🔍"}).
			WithDescription("Look up the details of a specific user by ID or UUID"),
		discord.NewStringSelectMenuOption("Lookup Group", constants.LookupGroupButtonCustomID).
			WithEmoji(discord.ComponentEmoji{Name: "🔍"}).
			WithDescription("Look up the details of a specific group by ID or UUID"),
		discord.NewStringSelectMenuOption("View Appeals", constants.AppealMenuButtonCustomID).
			WithEmoji(discord.ComponentEmoji{Name: "⚖️"}).
			WithDescription("View pending appeals"),
		discord.NewStringSelectMenuOption("User Settings", constants.UserSettingsButtonCustomID).
			WithEmoji(discord.ComponentEmoji{Name: "👤"}).
			WithDescription("Configure your personal settings"),
	)

	// Add admin tools option only for admins
	if b.botSettings.IsAdmin(b.userID) {
		options = append(options,
			discord.NewStringSelectMenuOption("Admin Tools", constants.AdminMenuButtonCustomID).
				WithEmoji(discord.ComponentEmoji{Name: "⚡"}).
				WithDescription("Access administrative tools"),
		)
	}

	// Create embeds
	embeds := []discord.Embed{
		b.buildWelcomeEmbed(),
		b.buildUserGraphEmbed(),
		b.buildGroupGraphEmbed(),
		b.buildWorkerStatusEmbed(),
	}

	// Add announcement embed if type is not none
	if b.botSettings.Announcement.Type != types.AnnouncementTypeNone &&
		b.botSettings.Announcement.Message != "" {
		embeds = append(embeds, b.buildAnnouncementEmbed())
	}

	// Create message builder
	builder := discord.NewMessageUpdateBuilder().
		SetEmbeds(embeds...).
		AddContainerComponents(
			discord.NewActionRow(
				discord.NewStringSelectMenu(constants.ActionSelectMenuCustomID, "Select an action", options...),
			),
			discord.NewActionRow(
				discord.NewSecondaryButton("🔄 Refresh", string(constants.RefreshButtonCustomID)),
			),
		)

	// Attach both chart files if available
	if b.userStatsBuffer != nil {
		builder.AddFile("user_stats_chart.png", "image/png", b.userStatsBuffer)
	}
	if b.groupStatsBuffer != nil {
		builder.AddFile("group_stats_chart.png", "image/png", b.groupStatsBuffer)
	}

	return builder
}

// buildWelcomeEmbed creates the main welcome embed.
func (b *Builder) buildWelcomeEmbed() discord.Embed {
	embed := discord.NewEmbedBuilder().
		SetTitle("Welcome to Rotector 👋").
		SetColor(constants.DefaultEmbedColor)

	// Add welcome message if set
	if b.botSettings.WelcomeMessage != "" {
		embed.SetDescription(b.botSettings.WelcomeMessage)
	}

	// Add active reviewers field if any are online
	if len(b.activeUsers) > 0 {
		// Collect reviewer IDs
		displayIDs := make([]uint64, 0, 10)
		for _, userID := range b.activeUsers {
			if b.botSettings.IsReviewer(uint64(userID)) {
				displayIDs = append(displayIDs, uint64(userID))
			}
		}

		// Format IDs and add count of additional users if any
		fieldValue := utils.FormatIDs(displayIDs)
		if len(displayIDs) > 10 {
			fieldValue += fmt.Sprintf("\n...and %d more", len(displayIDs)-10)
		}

		embed.AddField("Active Reviewers", fieldValue, false)
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

// buildUserGraphEmbed creates the embed containing user statistics graph and current counts.
func (b *Builder) buildUserGraphEmbed() discord.Embed {
	embed := discord.NewEmbedBuilder().
		SetTitle("User Statistics").
		AddField("Confirmed Users", strconv.Itoa(b.userCounts.Confirmed), true).
		AddField("Flagged Users", strconv.Itoa(b.userCounts.Flagged), true).
		AddField("Cleared Users", strconv.Itoa(b.userCounts.Cleared), true).
		AddField("Banned Users", strconv.Itoa(b.userCounts.Banned), true).
		SetColor(constants.DefaultEmbedColor)

	// Attach user statistics chart if available
	if b.userStatsBuffer != nil {
		embed.SetImage("attachment://user_stats_chart.png")
	}

	return embed.Build()
}

// buildGroupGraphEmbed creates the embed containing group statistics graph and current counts.
func (b *Builder) buildGroupGraphEmbed() discord.Embed {
	embed := discord.NewEmbedBuilder().
		SetTitle("Group Statistics").
		AddField("Confirmed Groups", strconv.Itoa(b.groupCounts.Confirmed), true).
		AddField("Flagged Groups", strconv.Itoa(b.groupCounts.Flagged), true).
		AddField("Cleared Groups", strconv.Itoa(b.groupCounts.Cleared), true).
		AddField("Locked Groups", strconv.Itoa(b.groupCounts.Locked), true).
		SetColor(constants.DefaultEmbedColor)

	// Attach group statistics chart if available
	if b.groupStatsBuffer != nil {
		embed.SetImage("attachment://group_stats_chart.png")
	}

	return embed.Build()
}

// buildAnnouncementEmbed creates the announcement embed.
func (b *Builder) buildAnnouncementEmbed() discord.Embed {
	var color int
	var title string

	switch b.botSettings.Announcement.Type {
	case types.AnnouncementTypeInfo:
		color = 0x3498DB // Blue
		title = "📢 Announcement"
	case types.AnnouncementTypeWarning:
		color = 0xF1C40F // Yellow
		title = "⚠️ Warning"
	case types.AnnouncementTypeSuccess:
		color = 0x2ECC71 // Green
		title = "✅ Notice"
	case types.AnnouncementTypeError:
		color = 0xE74C3C // Red
		title = "🚫 Alert"
	case types.AnnouncementTypeNone:
	}

	return discord.NewEmbedBuilder().
		SetTitle(title).
		SetDescription(b.botSettings.Announcement.Message).
		SetColor(color).
		Build()
}
