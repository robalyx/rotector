package leaderboard

import (
	"fmt"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/bot/utils"
	"github.com/robalyx/rotector/internal/database/types"
	"github.com/robalyx/rotector/internal/database/types/enum"
)

// Builder creates the visual layout for viewing the voting leaderboard.
type Builder struct {
	stats       []*types.VoteAccuracy
	usernames   map[uint64]string
	hasNextPage bool
	hasPrevPage bool
	lastRefresh time.Time
	nextRefresh time.Time
	period      enum.LeaderboardPeriod
	privacyMode bool
}

// NewBuilder creates a new leaderboard builder.
func NewBuilder(s *session.Session) *Builder {
	return &Builder{
		stats:       session.LeaderboardStats.Get(s),
		usernames:   session.LeaderboardUsernames.Get(s),
		hasNextPage: session.PaginationHasNextPage.Get(s),
		hasPrevPage: session.PaginationHasPrevPage.Get(s),
		lastRefresh: session.LeaderboardLastRefresh.Get(s),
		nextRefresh: session.LeaderboardNextRefresh.Get(s),
		period:      session.UserLeaderboardPeriod.Get(s),
		privacyMode: session.UserReviewMode.Get(s) == enum.ReviewModeTraining || session.UserStreamerMode.Get(s),
	}
}

// Build creates a Discord message showing the leaderboard entries.
func (b *Builder) Build() *discord.MessageUpdateBuilder {
	// Create main container
	var mainDisplays []discord.ContainerSubComponent

	// Add header
	mainDisplays = append(mainDisplays,
		discord.NewTextDisplay(fmt.Sprintf("## Voting Leaderboard\nTop voters for `%s` period", b.period.String())),
		discord.NewLargeSeparator(),
	)

	// Add leaderboard entries
	if len(b.stats) > 0 {
		var entriesContent string
		for _, stat := range b.stats {
			username := b.usernames[stat.DiscordUserID]
			if username == "" {
				username = fmt.Sprintf("Unknown (%d)", stat.DiscordUserID)
			}

			// Get rank display with medal if applicable
			rankDisplay := getRankDisplay(stat.Rank)

			// Calculate percentage
			accuracyPercent := stat.Accuracy * 100

			entriesContent += fmt.Sprintf("### %s %s\n```\nCorrect Votes: %d\nTotal Votes: %d\nAccuracy: %.1f%%\n```\n",
				rankDisplay,
				username,
				stat.CorrectVotes,
				stat.TotalVotes,
				accuracyPercent,
			)
		}
		mainDisplays = append(mainDisplays, discord.NewTextDisplay(entriesContent))
	} else {
		mainDisplays = append(mainDisplays, discord.NewTextDisplay("### No Results\nNo entries found for this time period"))
	}

	// Add footer
	if !b.lastRefresh.IsZero() {
		mainDisplays = append(mainDisplays,
			discord.NewLargeSeparator(),
			discord.NewTextDisplay(fmt.Sprintf("-# Last updated %s • Next update %s",
				utils.FormatTimeAgo(b.lastRefresh),
				utils.FormatTimeUntil(b.nextRefresh),
			)),
		)
	}

	mainContainer := discord.NewContainer(mainDisplays...).
		WithAccentColor(utils.GetContainerColor(b.privacyMode))

	// Create components
	components := b.buildInteractiveComponents()

	return discord.NewMessageUpdateBuilder().
		AddComponents(mainContainer).
		AddComponents(components...)
}

// buildInteractiveComponents creates all interactive components for the leaderboard viewer.
func (b *Builder) buildInteractiveComponents() []discord.LayoutComponent {
	return []discord.LayoutComponent{
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
			discord.NewSecondaryButton("◀️ Back", constants.BackButtonCustomID),
			discord.NewSecondaryButton("⏮️", string(session.ViewerFirstPage)).WithDisabled(!b.hasPrevPage),
			discord.NewSecondaryButton("◀️", string(session.ViewerPrevPage)).WithDisabled(!b.hasPrevPage),
			discord.NewSecondaryButton("▶️", string(session.ViewerNextPage)).WithDisabled(!b.hasNextPage),
			discord.NewSecondaryButton("⏭️", string(session.ViewerLastPage)).WithDisabled(true),
		),
	}
}

// buildPeriodOptions creates the options for the time period selection menu.
func (b *Builder) buildPeriodOptions() []discord.StringSelectMenuOption {
	return []discord.StringSelectMenuOption{
		discord.NewStringSelectMenuOption("Daily", enum.LeaderboardPeriodDaily.String()).
			WithDefault(b.period == enum.LeaderboardPeriodDaily),
		discord.NewStringSelectMenuOption("Weekly", enum.LeaderboardPeriodWeekly.String()).
			WithDefault(b.period == enum.LeaderboardPeriodWeekly),
		discord.NewStringSelectMenuOption("Bi-Weekly", enum.LeaderboardPeriodBiWeekly.String()).
			WithDefault(b.period == enum.LeaderboardPeriodBiWeekly),
		discord.NewStringSelectMenuOption("Monthly", enum.LeaderboardPeriodMonthly.String()).
			WithDefault(b.period == enum.LeaderboardPeriodMonthly),
		discord.NewStringSelectMenuOption("Bi-Annually", enum.LeaderboardPeriodBiAnnually.String()).
			WithDefault(b.period == enum.LeaderboardPeriodBiAnnually),
		discord.NewStringSelectMenuOption("Annually", enum.LeaderboardPeriodAnnually.String()).
			WithDefault(b.period == enum.LeaderboardPeriodAnnually),
		discord.NewStringSelectMenuOption("All Time", enum.LeaderboardPeriodAllTime.String()).
			WithDefault(b.period == enum.LeaderboardPeriodAllTime),
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
