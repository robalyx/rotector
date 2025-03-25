package dashboard

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"math/rand"
	"strconv"

	"github.com/disgoorg/disgo/discord"
	"github.com/redis/rueidis"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/bot/utils"
	"github.com/robalyx/rotector/internal/common/storage/database/types"
	"github.com/robalyx/rotector/internal/common/storage/database/types/enum"
	"github.com/robalyx/rotector/internal/worker/stats"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// Tips for users shown in the welcome embed footer.
var tips = []string{
	"Check out the leaderboard to see our top reviewers",
	"Track your performance through your vote statistics",
	"Use streamer mode to hide sensitive information",
	"You can continue from where you left off across all servers",
	"All foreign text is automatically translated for you",
	"Take your time to make accurate decisions",
}

// getRandomTip returns a random tip from the tips slice.
func getRandomTip() string {
	return "💡 " + tips[rand.Intn(len(tips))]
}

// Builder creates the visual layout for the main dashboard.
type Builder struct {
	userID              uint64
	userCounts          *types.UserCounts
	groupCounts         *types.GroupCounts
	userStatsBuffer     *bytes.Buffer
	groupStatsBuffer    *bytes.Buffer
	activeUsers         []uint64
	voteStats           *types.VoteAccuracy
	announcementType    enum.AnnouncementType
	announcementMessage string
	welcomeMessage      string
	isGuildOwner        bool
	isReviewer          bool
	isAdmin             bool
	showMaintenance     bool
	titleCaser          cases.Caser
}

// NewBuilder creates a new dashboard builder.
func NewBuilder(s *session.Session, redisClient rueidis.Client) *Builder {
	userStatsBuffer, groupStatsBuffer := getChartBuffers(redisClient)
	botSettings := s.BotSettings()
	userID := session.UserID.Get(s)
	announcementType := session.BotAnnouncementType.Get(s)
	isAdmin := botSettings.IsAdmin(userID)
	return &Builder{
		userID:              userID,
		userCounts:          session.StatsUserCounts.Get(s),
		groupCounts:         session.StatsGroupCounts.Get(s),
		userStatsBuffer:     userStatsBuffer,
		groupStatsBuffer:    groupStatsBuffer,
		activeUsers:         session.StatsActiveUsers.Get(s),
		voteStats:           session.StatsVotes.Get(s),
		announcementType:    announcementType,
		announcementMessage: session.BotAnnouncementMessage.Get(s),
		welcomeMessage:      session.BotWelcomeMessage.Get(s),
		isGuildOwner:        session.IsGuildOwner.Get(s),
		isReviewer:          botSettings.IsReviewer(userID),
		isAdmin:             isAdmin,
		showMaintenance:     announcementType == enum.AnnouncementTypeMaintenance && !isAdmin,
		titleCaser:          cases.Title(language.English),
	}
}

// getChartBuffers retrieves the cached chart buffers from Redis.
func getChartBuffers(client rueidis.Client) (*bytes.Buffer, *bytes.Buffer) {
	var userStatsChart, groupStatsChart *bytes.Buffer

	// Get user stats chart
	if result := client.Do(
		context.Background(),
		client.B().Get().Key(stats.UserStatsChartKey).Build(),
	); result.Error() == nil {
		if data, err := result.AsBytes(); err == nil {
			if decoded, err := base64.StdEncoding.DecodeString(string(data)); err == nil {
				userStatsChart = bytes.NewBuffer(decoded)
			}
		}
	}

	// Get group stats chart
	if result := client.Do(
		context.Background(),
		client.B().Get().Key(stats.GroupStatsChartKey).Build(),
	); result.Error() == nil {
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
	// Create embeds
	var embeds []discord.Embed

	// Show all embeds if not in maintenance mode or if user is admin
	if !b.showMaintenance {
		embeds = []discord.Embed{
			b.buildWelcomeEmbed(),
		}

		// Only show vote stats for non-reviewers
		if !b.isReviewer {
			embeds = append(embeds, b.buildVoteStatsEmbed())
		}

		// Add remaining embeds
		embeds = append(embeds,
			b.buildUserGraphEmbed(),
			b.buildGroupGraphEmbed(),
		)

		// Add announcement if exists
		if b.announcementType != enum.AnnouncementTypeNone && b.announcementMessage != "" {
			embeds = append(embeds, b.buildAnnouncementEmbed())
		}
	} else {
		// Only show maintenance announcement for non-admins during maintenance
		embeds = []discord.Embed{b.buildAnnouncementEmbed()}
	}

	// Create message builder
	builder := discord.NewMessageUpdateBuilder().
		SetEmbeds(embeds...).
		AddContainerComponents(b.buildComponents()...)

	// Attach both chart files if available
	if b.userStatsBuffer != nil && !b.showMaintenance {
		builder.AddFile("user_stats_chart.png", "image/png", b.userStatsBuffer)
	}
	if b.groupStatsBuffer != nil && !b.showMaintenance {
		builder.AddFile("group_stats_chart.png", "image/png", b.groupStatsBuffer)
	}

	return builder
}

// buildComponents creates all interactive components for the dashboard.
func (b *Builder) buildComponents() []discord.ContainerComponent {
	components := []discord.ContainerComponent{}

	// Create base options
	options := []discord.StringSelectMenuOption{
		discord.NewStringSelectMenuOption("Review Users", constants.StartUserReviewButtonCustomID).
			WithEmoji(discord.ComponentEmoji{Name: "📝"}).
			WithDescription("Start reviewing flagged users"),
	}

	// Add group review option only for reviewers
	if b.isReviewer {
		options = append(options,
			discord.NewStringSelectMenuOption("Review Groups", constants.StartGroupReviewButtonCustomID).
				WithEmoji(discord.ComponentEmoji{Name: "📝"}).
				WithDescription("Start reviewing flagged groups"),
		)
	}

	// Add remaining base options
	options = append(options,
		discord.NewStringSelectMenuOption("Lookup Roblox User", constants.LookupRobloxUserButtonCustomID).
			WithEmoji(discord.ComponentEmoji{Name: "🔍"}).
			WithDescription("Look up specific Roblox user by ID or UUID"),
		discord.NewStringSelectMenuOption("Lookup Roblox Group", constants.LookupRobloxGroupButtonCustomID).
			WithEmoji(discord.ComponentEmoji{Name: "🔍"}).
			WithDescription("Look up specific Roblox group by ID or UUID"),
		discord.NewStringSelectMenuOption("Lookup Discord User", constants.LookupDiscordUserButtonCustomID).
			WithEmoji(discord.ComponentEmoji{Name: "🔍"}).
			WithDescription("Look up Discord user and their flagged servers"),
		discord.NewStringSelectMenuOption("View Leaderboard", constants.LeaderboardMenuButtonCustomID).
			WithEmoji(discord.ComponentEmoji{Name: "🏆"}).
			WithDescription("View voting leaderboard"),
	)

	// Add reviewer-only options
	if b.isReviewer {
		options = append(options,
			discord.NewStringSelectMenuOption("AI Chat Assistant", constants.ChatAssistantButtonCustomID).
				WithEmoji(discord.ComponentEmoji{Name: "🤖"}).
				WithDescription("Chat with AI about moderation topics"),
			discord.NewStringSelectMenuOption("Activity Log Browser", constants.ActivityBrowserButtonCustomID).
				WithEmoji(discord.ComponentEmoji{Name: "📜"}).
				WithDescription("Search and filter activity logs"),
			discord.NewStringSelectMenuOption("User Queue Manager", constants.QueueManagerButtonCustomID).
				WithEmoji(discord.ComponentEmoji{Name: "📋"}).
				WithDescription("Manage user recheck queue priorities"),
			discord.NewStringSelectMenuOption("Worker Status", constants.WorkerStatusButtonCustomID).
				WithEmoji(discord.ComponentEmoji{Name: "🔧"}).
				WithDescription("View worker status and health"),
			discord.NewStringSelectMenuOption("Reviewer Stats", constants.ReviewerStatsButtonCustomID).
				WithEmoji(discord.ComponentEmoji{Name: "📊"}).
				WithDescription("View reviewer activity statistics"),
		)
	}

	// Add last default options
	options = append(options,
		discord.NewStringSelectMenuOption("View Appeals", constants.AppealMenuButtonCustomID).
			WithEmoji(discord.ComponentEmoji{Name: "⚖️"}).
			WithDescription("View appeals and data deletion requests"),
		discord.NewStringSelectMenuOption("User Settings", constants.UserSettingsButtonCustomID).
			WithEmoji(discord.ComponentEmoji{Name: "👤"}).
			WithDescription("Configure your personal settings"),
	)

	// Add guild owner option if user is a guild owner or admin
	if b.isGuildOwner || b.isAdmin {
		options = append(options,
			discord.NewStringSelectMenuOption("Guild Owner Tools", constants.GuildOwnerMenuButtonCustomID).
				WithEmoji(discord.ComponentEmoji{Name: "🛡️"}).
				WithDescription("Access guild owner tools"),
		)
	}

	// Add admin tools option only for admins
	if b.isAdmin {
		options = append(options,
			discord.NewStringSelectMenuOption("Admin Tools", constants.AdminMenuButtonCustomID).
				WithEmoji(discord.ComponentEmoji{Name: "⚡"}).
				WithDescription("Access administrative tools"),
		)
	}

	// Add components based on maintenance mode and admin status
	if !b.showMaintenance {
		// Show all components for admins or when not in maintenance
		components = append(components,
			discord.NewActionRow(
				discord.NewStringSelectMenu(constants.ActionSelectMenuCustomID, "Select an action", options...),
			),
			discord.NewActionRow(
				discord.NewSecondaryButton("🔄 Refresh", constants.RefreshButtonCustomID),
			),
		)
	} else {
		// Only show refresh button during maintenance for non-admins
		components = append(components,
			discord.NewActionRow(
				discord.NewSecondaryButton("🔄 Refresh", constants.RefreshButtonCustomID),
			),
		)
	}

	return components
}

// buildWelcomeEmbed creates the main welcome embed.
func (b *Builder) buildWelcomeEmbed() discord.Embed {
	embed := discord.NewEmbedBuilder().
		SetTitle("Welcome to Rotector 👋").
		SetColor(constants.DefaultEmbedColor)

	// Add welcome message if set
	if b.welcomeMessage != "" {
		embed.SetDescription(b.welcomeMessage)
	}

	// Add active reviewers field if any are online
	if len(b.activeUsers) > 0 {
		// Collect reviewer IDs
		displayIDs := make([]uint64, 0, 10)
		for _, userID := range b.activeUsers {
			if b.isReviewer {
				displayIDs = append(displayIDs, userID)
			}
		}

		// Format IDs and add count of additional users if any
		fieldValue := utils.FormatIDs(displayIDs)
		if len(displayIDs) > 10 {
			fieldValue += fmt.Sprintf("\n...and %d more", len(displayIDs)-10)
		}

		embed.AddField("Active Reviewers", fieldValue, false)
	}

	// Add random tip to footer
	embed.SetFooter(getRandomTip(), "")

	return embed.Build()
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

	switch b.announcementType {
	case enum.AnnouncementTypeInfo:
		color = 0x3498DB // Blue
		title = "📢 Announcement"
	case enum.AnnouncementTypeWarning:
		color = 0xF1C40F // Yellow
		title = "⚠️ Warning"
	case enum.AnnouncementTypeSuccess:
		color = 0x2ECC71 // Green
		title = "✅ Notice"
	case enum.AnnouncementTypeError:
		color = 0xE74C3C // Red
		title = "🚫 Alert"
	case enum.AnnouncementTypeMaintenance:
		color = 0x95A5A6 // Gray
		title = "🔧 System Maintenance"
	case enum.AnnouncementTypeNone:
	}

	return discord.NewEmbedBuilder().
		SetTitle(title).
		SetDescription(b.announcementMessage).
		SetColor(color).
		Build()
}

// buildVoteStatsEmbed creates the vote statistics embed.
func (b *Builder) buildVoteStatsEmbed() discord.Embed {
	embed := discord.NewEmbedBuilder().
		SetTitle("Your Vote Statistics").
		AddField("Correct Votes", strconv.FormatInt(b.voteStats.CorrectVotes, 10), true).
		AddField("Total Votes", strconv.FormatInt(b.voteStats.TotalVotes, 10), true).
		SetColor(constants.DefaultEmbedColor)

	// Calculate and add accuracy field
	accuracyStr := "0%"
	if b.voteStats.TotalVotes > 0 {
		accuracyStr = fmt.Sprintf("%.1f%%", b.voteStats.Accuracy*100)
	}
	embed.AddField("Accuracy", accuracyStr, true)

	// Add rank field if available
	if b.voteStats.Rank > 0 {
		embed.AddField("Leaderboard Rank", fmt.Sprintf("#%d", b.voteStats.Rank), true)
	} else {
		embed.AddField("Leaderboard Rank", "Unranked", true)
	}

	return embed.Build()
}
