package reviewer

import (
	"fmt"
	"strings"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/bot/utils"
	"github.com/robalyx/rotector/internal/common/storage/database/types"
	"github.com/robalyx/rotector/internal/common/storage/database/types/enum"
)

// Builder creates the visual layout for viewing reviewer statistics.
type Builder struct {
	stats       map[uint64]*types.ReviewerStats
	usernames   map[uint64]string
	hasNextPage bool
	hasPrevPage bool
	lastRefresh time.Time
	nextRefresh time.Time
	period      enum.ReviewerStatsPeriod
}

// NewBuilder creates a new reviewer stats builder.
func NewBuilder(s *session.Session) *Builder {
	return &Builder{
		stats:       session.ReviewerStats.Get(s),
		usernames:   session.ReviewerUsernames.Get(s),
		hasNextPage: session.PaginationHasNextPage.Get(s),
		hasPrevPage: session.PaginationHasPrevPage.Get(s),
		lastRefresh: session.ReviewerStatsLastRefresh.Get(s),
		nextRefresh: session.ReviewerStatsNextRefresh.Get(s),
		period:      session.UserReviewerStatsPeriod.Get(s),
	}
}

// Build creates a Discord message showing the reviewer statistics.
func (b *Builder) Build() *discord.MessageUpdateBuilder {
	embed := discord.NewEmbedBuilder().
		SetTitle("Reviewer Statistics").
		SetDescription(fmt.Sprintf("Activity statistics for `%s` period", b.period.String())).
		SetColor(constants.DefaultEmbedColor)

	if !b.lastRefresh.IsZero() {
		embed.SetFooter(fmt.Sprintf(
			"Last updated %s • Next update %s",
			utils.FormatTimeAgo(b.lastRefresh),
			utils.FormatTimeUntil(b.nextRefresh),
		), "")
	}

	if len(b.stats) > 0 {
		for reviewerID, username := range b.usernames {
			if stat, ok := b.stats[reviewerID]; ok {
				var statsBuilder strings.Builder
				statsBuilder.WriteString("```\n")
				statsBuilder.WriteString(fmt.Sprintf("Users Viewed: %d\n", stat.UsersViewed))
				statsBuilder.WriteString(fmt.Sprintf("Users Confirmed: %d\n", stat.UsersConfirmed))
				statsBuilder.WriteString(fmt.Sprintf("Users Cleared: %d\n", stat.UsersCleared))
				statsBuilder.WriteString("```")

				embed.AddField(username, statsBuilder.String(), false)
			}
		}
	} else {
		embed.AddField("No Results", "No reviewer statistics available", false)
	}

	return discord.NewMessageUpdateBuilder().
		SetEmbeds(embed.Build()).
		AddContainerComponents(b.buildComponents()...)
}

// buildComponents creates all interactive components for the reviewer stats viewer.
func (b *Builder) buildComponents() []discord.ContainerComponent {
	return []discord.ContainerComponent{
		// Time period selection menu
		discord.NewActionRow(
			discord.NewStringSelectMenu(constants.ReviewerStatsPeriodSelectMenuCustomID, "Select Time Period",
				b.buildPeriodOptions()...),
		),
		// Refresh button
		discord.NewActionRow(
			discord.NewSecondaryButton("🔄 Refresh", constants.RefreshButtonCustomID),
		),
		// Navigation buttons
		discord.NewActionRow(
			discord.NewSecondaryButton("◀️", constants.BackButtonCustomID),
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
		discord.NewStringSelectMenuOption("Daily", enum.ReviewerStatsPeriodDaily.String()).
			WithDefault(b.period == enum.ReviewerStatsPeriodDaily),
		discord.NewStringSelectMenuOption("Weekly", enum.ReviewerStatsPeriodWeekly.String()).
			WithDefault(b.period == enum.ReviewerStatsPeriodWeekly),
		discord.NewStringSelectMenuOption("Monthly", enum.ReviewerStatsPeriodMonthly.String()).
			WithDefault(b.period == enum.ReviewerStatsPeriodMonthly),
	}
}
