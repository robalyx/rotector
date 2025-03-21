package reviewer

import (
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/snowflake/v2"
	builder "github.com/robalyx/rotector/internal/bot/builder/reviewer"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/interaction"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/common/storage/database/types"
	"github.com/robalyx/rotector/internal/common/storage/database/types/enum"
	"go.uber.org/zap"
)

// Menu handles the display and interaction logic for viewing reviewer stats.
type Menu struct {
	layout *Layout
	page   *interaction.Page
}

// NewMenu creates a new reviewer stats menu.
func NewMenu(l *Layout) *Menu {
	m := &Menu{layout: l}
	m.page = &interaction.Page{
		Name: constants.ReviewerStatsPageName,
		Message: func(s *session.Session) *discord.MessageUpdateBuilder {
			return builder.NewBuilder(s).Build()
		},
		ShowHandlerFunc:   m.Show,
		ButtonHandlerFunc: m.handleButton,
		SelectHandlerFunc: m.handleSelectMenu,
	}
	return m
}

// Show prepares and displays the reviewer stats interface.
func (m *Menu) Show(ctx *interaction.Context, s *session.Session) {
	cursor := session.ReviewerStatsCursor.Get(s)
	period := session.UserReviewerStatsPeriod.Get(s)

	// Get refresh info for the selected period
	lastRefresh, nextRefresh, err := m.layout.db.Service().View().GetReviewerStatsRefreshInfo(
		ctx.Context(),
		period,
	)
	if err != nil {
		m.layout.logger.Error("Failed to get refresh info", zap.Error(err))
		ctx.Error("Failed to retrieve reviewer statistics. Please try again.")
		return
	}

	// Fetch reviewer stats from database
	stats, nextCursor, err := m.layout.db.Service().Reviewer().GetReviewerStats(
		ctx.Context(),
		period,
		cursor,
		constants.ReviewerStatsPerPage,
	)
	if err != nil {
		m.layout.logger.Error("Failed to get reviewer stats", zap.Error(err))
		ctx.Error("Failed to retrieve reviewer statistics. Please try again.")
		return
	}

	// Fetch usernames for all reviewers
	usernames := make(map[uint64]string)
	for reviewerID := range stats {
		user, err := m.layout.client.Rest().GetUser(snowflake.ID(reviewerID))
		if err != nil {
			usernames[reviewerID] = "Unknown"
			continue
		}
		usernames[reviewerID] = user.Username
	}

	// Store results in session
	session.ReviewerStats.Set(s, stats)
	session.ReviewerUsernames.Set(s, usernames)
	session.ReviewerStatsNextCursor.Set(s, nextCursor)
	session.PaginationHasNextPage.Set(s, nextCursor != nil)
	session.PaginationHasPrevPage.Set(s, cursor != nil)
	session.ReviewerStatsLastRefresh.Set(s, lastRefresh)
	session.ReviewerStatsNextRefresh.Set(s, nextRefresh)
}

// handleSelectMenu processes select menu interactions.
func (m *Menu) handleSelectMenu(ctx *interaction.Context, s *session.Session, customID, option string) {
	if customID != constants.ReviewerStatsPeriodSelectMenuCustomID {
		return
	}

	// Parse option to reviewer stats period
	period, err := enum.ReviewerStatsPeriodString(option)
	if err != nil {
		m.layout.logger.Error("Failed to parse reviewer stats period", zap.Error(err))
		ctx.Error("Failed to save time period preference. Please try again.")
		return
	}

	// Update user's reviewer stats period preference
	session.UserReviewerStatsPeriod.Set(s, period)

	// Reset page and show updated stats
	ResetStats(s)
	ctx.Reload("")
}

// handleButton processes button interactions.
func (m *Menu) handleButton(ctx *interaction.Context, s *session.Session, customID string) {
	switch customID {
	case constants.BackButtonCustomID:
		ctx.NavigateBack("")
	case constants.RefreshButtonCustomID:
		ResetStats(s)
		ctx.Reload("")
	case string(session.ViewerFirstPage),
		string(session.ViewerPrevPage),
		string(session.ViewerNextPage),
		string(session.ViewerLastPage):
		m.handlePagination(ctx, s, session.ViewerAction(customID))
	}
}

// handlePagination processes page navigation.
func (m *Menu) handlePagination(ctx *interaction.Context, s *session.Session, action session.ViewerAction) {
	switch action {
	case session.ViewerNextPage:
		cursor := session.ReviewerStatsCursor.Get(s)
		nextCursor := session.ReviewerStatsNextCursor.Get(s)
		prevCursors := session.ReviewerStatsPrevCursors.Get(s)

		if session.PaginationHasNextPage.Get(s) {
			session.ReviewerStatsCursor.Set(s, nextCursor)
			session.ReviewerStatsPrevCursors.Set(s, append(prevCursors, cursor))
			ctx.Reload("")
		}
	case session.ViewerPrevPage:
		prevCursors := session.ReviewerStatsPrevCursors.Get(s)

		if len(prevCursors) > 0 {
			lastIdx := len(prevCursors) - 1
			session.ReviewerStatsPrevCursors.Set(s, prevCursors[:lastIdx])
			session.ReviewerStatsCursor.Set(s, prevCursors[lastIdx])
			ctx.Reload("")
		}
	case session.ViewerFirstPage:
		session.ReviewerStatsCursor.Set(s, nil)
		session.ReviewerStatsPrevCursors.Set(s, []*types.ReviewerStatsCursor{})
		ctx.Reload("")
	case session.ViewerLastPage:
		return
	}
}
