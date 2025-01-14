package leaderboard

import (
	"fmt"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/bot/utils"
	"github.com/robalyx/rotector/internal/common/storage/database/types"
	"github.com/robalyx/rotector/internal/common/storage/database/types/enum"
)

// Builder creates the visual layout for viewing the voting leaderboard.
type Builder struct {
	settings    *types.UserSetting
	stats       []types.VoteAccuracy
	usernames   map[uint64]string
	hasNextPage bool
	hasPrevPage bool
	lastRefresh time.Time
	nextRefresh time.Time
}

// NewBuilder creates a new leaderboard builder.
func NewBuilder(s *session.Session) *Builder {
	var settings *types.UserSetting
	s.GetInterface(constants.SessionKeyUserSettings, &settings)
	var stats []types.VoteAccuracy
	s.GetInterface(constants.SessionKeyLeaderboardStats, &stats)
	var usernames map[uint64]string
	s.GetInterface(constants.SessionKeyLeaderboardUsernames, &usernames)
	var lastRefresh time.Time
	s.GetInterface(constants.SessionKeyLeaderboardLastRefresh, &lastRefresh)
	var nextRefresh time.Time
	s.GetInterface(constants.SessionKeyLeaderboardNextRefresh, &nextRefresh)

	return &Builder{
		settings:    settings,
		stats:       stats,
		usernames:   usernames,
		hasNextPage: s.GetBool(constants.SessionKeyHasNextPage),
		hasPrevPage: s.GetBool(constants.SessionKeyHasPrevPage),
		lastRefresh: lastRefresh,
		nextRefresh: nextRefresh,
	}
}

// Build creates a Discord message showing the leaderboard entries.
func (b *Builder) Build() *discord.MessageUpdateBuilder {
	embed := discord.NewEmbedBuilder().
		SetTitle("🏆 Voting Leaderboard").
		SetDescription(fmt.Sprintf("Top voters for %s period", b.settings.LeaderboardPeriod.String())).
		SetColor(utils.GetMessageEmbedColor(b.settings.StreamerMode))

	if !b.lastRefresh.IsZero() {
		embed.SetFooter(fmt.Sprintf(
			"Last updated %s • Next update %s",
			utils.FormatTimeAgo(b.lastRefresh),
			utils.FormatTimeUntil(b.nextRefresh),
		), "")
	}

	if len(b.stats) > 0 {
		for _, stat := range b.stats {
			username := b.usernames[stat.DiscordUserID]
			if username == "" {
				username = fmt.Sprintf("Unknown (%d)", stat.DiscordUserID)
			}

			// Get rank display with medal if applicable
			rankDisplay := getRankDisplay(stat.Rank)

			// Calculate percentage
			accuracyPercent := stat.Accuracy * 100

			// Format field content with better spacing and alignment
			embed.AddField(
				fmt.Sprintf("%s %s", rankDisplay, username),
				fmt.Sprintf("```\nCorrect Votes: %d\nTotal Votes: %d\nAccuracy: %.1f%%\n```",
					stat.CorrectVotes,
					stat.TotalVotes,
					accuracyPercent),
				false,
			)
		}
	} else {
		embed.AddField("No Results", "No entries found for this time period", false)
	}

	// Create components
	components := b.buildComponents()

	return discord.NewMessageUpdateBuilder().
		SetEmbeds(embed.Build()).
		AddContainerComponents(components...)
}

// buildComponents creates all interactive components for the leaderboard viewer.
func (b *Builder) buildComponents() []discord.ContainerComponent {
	return []discord.ContainerComponent{
		// Time period selection menu
		discord.NewActionRow(
			discord.NewStringSelectMenu(constants.LeaderboardPeriodSelectMenuCustomID, "Select Time Period",
				b.buildPeriodOptions()...),
		),
		// Refresh button
		discord.NewActionRow(
			discord.NewSecondaryButton("🔄 Refresh", constants.RefreshButtonCustomID),
		),
		// Navigation buttons
		discord.NewActionRow(
			discord.NewSecondaryButton("◀️", constants.BackButtonCustomID),
			discord.NewSecondaryButton("⏮️", string(utils.ViewerFirstPage)).WithDisabled(!b.hasPrevPage),
			discord.NewSecondaryButton("◀️", string(utils.ViewerPrevPage)).WithDisabled(!b.hasPrevPage),
			discord.NewSecondaryButton("▶️", string(utils.ViewerNextPage)).WithDisabled(!b.hasNextPage),
			discord.NewSecondaryButton("⏭️", string(utils.ViewerLastPage)).WithDisabled(true),
		),
	}
}

// buildPeriodOptions creates the options for the time period selection menu.
func (b *Builder) buildPeriodOptions() []discord.StringSelectMenuOption {
	return []discord.StringSelectMenuOption{
		discord.NewStringSelectMenuOption("Daily", enum.LeaderboardPeriodDaily.String()).
			WithDefault(b.settings.LeaderboardPeriod == enum.LeaderboardPeriodDaily),
		discord.NewStringSelectMenuOption("Weekly", enum.LeaderboardPeriodWeekly.String()).
			WithDefault(b.settings.LeaderboardPeriod == enum.LeaderboardPeriodWeekly),
		discord.NewStringSelectMenuOption("Bi-Weekly", enum.LeaderboardPeriodBiWeekly.String()).
			WithDefault(b.settings.LeaderboardPeriod == enum.LeaderboardPeriodBiWeekly),
		discord.NewStringSelectMenuOption("Monthly", enum.LeaderboardPeriodMonthly.String()).
			WithDefault(b.settings.LeaderboardPeriod == enum.LeaderboardPeriodMonthly),
		discord.NewStringSelectMenuOption("Bi-Annually", enum.LeaderboardPeriodBiAnnually.String()).
			WithDefault(b.settings.LeaderboardPeriod == enum.LeaderboardPeriodBiAnnually),
		discord.NewStringSelectMenuOption("Annually", enum.LeaderboardPeriodAnnually.String()).
			WithDefault(b.settings.LeaderboardPeriod == enum.LeaderboardPeriodAnnually),
		discord.NewStringSelectMenuOption("All Time", enum.LeaderboardPeriodAllTime.String()).
			WithDefault(b.settings.LeaderboardPeriod == enum.LeaderboardPeriodAllTime),
	}
}

// getRankDisplay returns a formatted rank with medal emoji for top 3.
func getRankDisplay(rank int) string {
	switch rank {
	case 1:
		return "🥇"
	case 2:
		return "🥈"
	case 3:
		return "🥉"
	default:
		return fmt.Sprintf("#%d", rank)
	}
}
