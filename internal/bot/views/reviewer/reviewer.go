package reviewer

import (
	"fmt"
	"strings"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/bot/utils"
	"github.com/robalyx/rotector/internal/database/types"
	"github.com/robalyx/rotector/internal/database/types/enum"
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
	builder := discord.NewMessageUpdateBuilder()

	// Create main container components
	var components []discord.ContainerSubComponent

	// Add header section
	var headerContent strings.Builder
	headerContent.WriteString(fmt.Sprintf("## Reviewer Statistics\nActivity statistics for `%s` period", b.period.String()))

	if !b.lastRefresh.IsZero() {
		headerContent.WriteString(fmt.Sprintf("\n\n-# Last updated %s • Next update %s",
			utils.FormatTimeAgo(b.lastRefresh),
			utils.FormatTimeUntil(b.nextRefresh),
		))
	}

	components = append(components, discord.NewTextDisplay(headerContent.String()))

	// Add stats section
	if len(b.stats) > 0 {
		components = append(components, discord.NewLargeSeparator())

		var statsContent strings.Builder
		statsContent.WriteString("## Activity Overview\n\n")

		for reviewerID, username := range b.usernames {
			if stat, ok := b.stats[reviewerID]; ok {
				statsContent.WriteString(fmt.Sprintf("\n### %s\n", username))
				statsContent.WriteString(utils.FormatString(fmt.Sprintf(
					"Users Viewed: %d\nUsers Confirmed: %d\nUsers Cleared: %d",
					stat.UsersViewed,
					stat.UsersConfirmed,
					stat.UsersCleared,
				)))
			}
		}

		components = append(components, discord.NewTextDisplay(statsContent.String()))
	} else {
		components = append(components, discord.NewLargeSeparator())
		components = append(components, discord.NewTextDisplay("## No Results\nNo reviewer statistics available"))
	}

	// Add navigation buttons
	components = append(components, discord.NewActionRow(
		discord.NewSecondaryButton("⏮️", string(session.ViewerFirstPage)).WithDisabled(!b.hasPrevPage),
		discord.NewSecondaryButton("◀️", string(session.ViewerPrevPage)).WithDisabled(!b.hasPrevPage),
		discord.NewSecondaryButton("▶️", string(session.ViewerNextPage)).WithDisabled(!b.hasNextPage),
		discord.NewSecondaryButton("⏭️", string(session.ViewerLastPage)).WithDisabled(true),
	))

	// Create main container
	mainContainer := discord.NewContainer(components...).
		WithAccentColor(constants.DefaultContainerColor)

	// Add all components
	builder.AddComponents(
		mainContainer,
		discord.NewActionRow(
			discord.NewStringSelectMenu(constants.ReviewerStatsPeriodSelectMenuCustomID, "Select Time Period",
				b.buildPeriodOptions()...),
		),
		discord.NewActionRow(
			discord.NewSecondaryButton("◀️ Back", constants.BackButtonCustomID),
			discord.NewSecondaryButton("🔄 Refresh", constants.RefreshButtonCustomID),
		),
	)

	return builder
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
