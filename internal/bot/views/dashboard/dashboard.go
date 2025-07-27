package dashboard

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"

	"github.com/disgoorg/disgo/discord"
	"github.com/redis/rueidis"
	"github.com/robalyx/rotector/assets"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/bot/utils"
	"github.com/robalyx/rotector/internal/database/types"
	"github.com/robalyx/rotector/internal/database/types/enum"
	"github.com/robalyx/rotector/internal/worker/stats"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// Builder creates the visual layout for the main dashboard.
type Builder struct {
	userID              uint64
	userCounts          *types.UserCounts
	groupCounts         *types.GroupCounts
	userStatsBuffer     *bytes.Buffer
	groupStatsBuffer    *bytes.Buffer
	activeUsers         []uint64
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
	// Create components
	var components []discord.LayoutComponent

	// Show all components if not in maintenance mode or if user is admin
	if !b.showMaintenance {
		// Add stats components
		components = append(components,
			b.buildUserGraphContainer(),
			b.buildGroupGraphContainer(),
		)

		// Add announcement if exists
		if b.announcementType != enum.AnnouncementTypeNone && b.announcementMessage != "" {
			components = append(components,
				b.buildAnnouncementContainer(),
			)
		}

		// Add welcome container with action menu at the bottom
		components = append(components, b.buildWelcomeContainer())
	} else {
		// Only show maintenance announcement and refresh button for non-admins during maintenance
		components = append(components,
			b.buildAnnouncementContainer(),
			discord.NewActionRow(
				discord.NewSecondaryButton("🔄 Refresh", constants.RefreshButtonCustomID),
			),
		)
	}

	// Create message builder
	builder := discord.NewMessageUpdateBuilder().
		AddComponents(components...).
		AddFile("banner.png", "", bytes.NewReader(assets.Banner))

	// Attach both chart files if available
	if b.userStatsBuffer != nil && !b.showMaintenance {
		builder.AddFile("user_stats_chart.png", "", b.userStatsBuffer)
	}

	if b.groupStatsBuffer != nil && !b.showMaintenance {
		builder.AddFile("group_stats_chart.png", "", b.groupStatsBuffer)
	}

	return builder
}

// buildWelcomeContainer creates the main welcome container.
func (b *Builder) buildWelcomeContainer() discord.LayoutComponent {
	var displays []discord.ContainerSubComponent

	displays = append(displays,
		discord.NewTextDisplay("# Welcome to Rotector 👋"),
		discord.NewMediaGallery(
			discord.MediaGalleryItem{
				Media: discord.UnfurledMediaItem{
					URL: "attachment://banner.png",
				},
			},
		),
		discord.NewLargeSeparator(),
	)

	// Add welcome message if set
	if b.welcomeMessage != "" {
		displays = append(displays, discord.NewTextDisplay(b.welcomeMessage))
	}

	// Add active reviewers if any are online
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

		displays = append(displays, discord.NewTextDisplay("**Active Reviewers**\n"+fieldValue))
	}

	// Add user review section only for reviewers
	if b.isReviewer {
		displays = append(displays,
			discord.NewLargeSeparator(),
			discord.NewSection(
				discord.NewTextDisplay("📝 **Review Users**\nStart reviewing flagged users"),
			).WithAccessory(
				discord.NewPrimaryButton("Start Review", constants.StartUserReviewButtonCustomID),
			),
		)
	}

	// Add group review section only for admins
	if b.isAdmin {
		displays = append(displays,
			discord.NewSection(
				discord.NewTextDisplay("📝 **Review Groups**\nStart reviewing flagged groups"),
			).WithAccessory(
				discord.NewPrimaryButton("Start Review", constants.StartGroupReviewButtonCustomID),
			),
		)
	}

	// Add action menu components
	options := b.buildActionMenuOptions()
	displays = append(displays,
		discord.NewLargeSeparator(),
		discord.NewActionRow(
			discord.NewStringSelectMenu(constants.ActionSelectMenuCustomID, "Select other actions", options...),
		),
		discord.NewActionRow(
			discord.NewSecondaryButton("🔄 Refresh", constants.RefreshButtonCustomID),
		),
	)

	return discord.NewContainer(displays...).WithAccentColor(constants.DefaultContainerColor)
}

// buildActionMenuOptions creates the options for the action menu.
func (b *Builder) buildActionMenuOptions() []discord.StringSelectMenuOption {
	// Create base options
	options := []discord.StringSelectMenuOption{
		discord.NewStringSelectMenuOption("Lookup Roblox User", constants.LookupRobloxUserButtonCustomID).
			WithEmoji(discord.ComponentEmoji{Name: "🔍"}).
			WithDescription("Look up specific Roblox user by ID or UUID"),
	}

	// Add Roblox group lookup option only for admins
	if b.isAdmin {
		options = append(options,
			discord.NewStringSelectMenuOption("Lookup Roblox Group", constants.LookupRobloxGroupButtonCustomID).
				WithEmoji(discord.ComponentEmoji{Name: "🔍"}).
				WithDescription("Look up specific Roblox group by ID or UUID"),
		)
	}

	// Continue with remaining base options
	options = append(options,
		discord.NewStringSelectMenuOption("Lookup Discord User", constants.LookupDiscordUserButtonCustomID).
			WithEmoji(discord.ComponentEmoji{Name: "🔍"}).
			WithDescription("Look up Discord user and their flagged servers"),
	)

	// Add reviewer-only options
	if b.isReviewer {
		options = append(options,
			discord.NewStringSelectMenuOption("Queue Management", constants.QueueManagementButtonCustomID).
				WithEmoji(discord.ComponentEmoji{Name: "📥"}).
				WithDescription("Add users to the processing queue"),
			discord.NewStringSelectMenuOption("AI Chat Assistant", constants.ChatAssistantButtonCustomID).
				WithEmoji(discord.ComponentEmoji{Name: "🤖"}).
				WithDescription("Chat with AI about moderation topics"),
			discord.NewStringSelectMenuOption("Activity Log Browser", constants.ActivityBrowserButtonCustomID).
				WithEmoji(discord.ComponentEmoji{Name: "📜"}).
				WithDescription("Search and filter activity logs"),
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

	return options
}

// buildUserGraphContainer creates the container containing user statistics graph and current counts.
func (b *Builder) buildUserGraphContainer() discord.LayoutComponent {
	displays := []discord.ContainerSubComponent{
		discord.NewTextDisplay("# User Statistics"),
		discord.NewTextDisplayf("**Confirmed Users:** `%d`\n**Flagged Users:** `%d`\n**Cleared Users:** `%d`\n**Banned Users:** `%d`",
			b.userCounts.Confirmed,
			b.userCounts.Flagged,
			b.userCounts.Cleared,
			b.userCounts.Banned,
		),
	}

	if b.userStatsBuffer != nil {
		displays = append(displays, discord.NewMediaGallery(
			discord.MediaGalleryItem{
				Media: discord.UnfurledMediaItem{
					URL: "attachment://user_stats_chart.png",
				},
			},
		))
	}

	return discord.NewContainer(displays...).WithAccentColor(constants.DefaultContainerColor)
}

// buildGroupGraphContainer creates the container containing group statistics graph and current counts.
func (b *Builder) buildGroupGraphContainer() discord.LayoutComponent {
	displays := []discord.ContainerSubComponent{
		discord.NewTextDisplay("# Group Statistics"),
		discord.NewTextDisplayf("**Confirmed Groups:** `%d`\n**Flagged Groups:** `%d`\n**Cleared Groups:** `%d`\n**Locked Groups:** `%d`",
			b.groupCounts.Confirmed,
			b.groupCounts.Flagged,
			b.groupCounts.Cleared,
			b.groupCounts.Locked,
		),
	}

	if b.groupStatsBuffer != nil {
		displays = append(displays, discord.NewMediaGallery(
			discord.MediaGalleryItem{
				Media: discord.UnfurledMediaItem{
					URL: "attachment://group_stats_chart.png",
				},
			},
		))
	}

	return discord.NewContainer(displays...).WithAccentColor(constants.DefaultContainerColor)
}

// buildAnnouncementContainer creates the announcement container.
func (b *Builder) buildAnnouncementContainer() discord.LayoutComponent {
	var (
		color int
		title string
	)

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

	return discord.NewContainer(
		discord.NewTextDisplay("# "+title),
		discord.NewTextDisplay(b.announcementMessage),
	).WithAccentColor(color)
}
