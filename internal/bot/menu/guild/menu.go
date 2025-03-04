package guild

import (
	"context"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	builder "github.com/robalyx/rotector/internal/bot/builder/guild"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/pagination"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/bot/interfaces"
	"go.uber.org/zap"
)

// Menu handles the guild owner menu operations.
type Menu struct {
	layout *Layout
	page   *pagination.Page
}

// NewMenu creates a new guild owner menu.
func NewMenu(layout *Layout) *Menu {
	m := &Menu{layout: layout}
	m.page = &pagination.Page{
		Name: constants.GuildOwnerPageName,
		Message: func(s *session.Session) *discord.MessageUpdateBuilder {
			return builder.NewMenuBuilder(s).Build()
		},
		ShowHandlerFunc:   m.Show,
		SelectHandlerFunc: m.handleSelectMenu,
		ButtonHandlerFunc: m.handleButton,
	}
	return m
}

// Show prepares and displays the guild owner interface.
func (m *Menu) Show(_ interfaces.CommonEvent, s *session.Session, _ *pagination.Respond) {
	ctx := context.Background()

	// Fetch unique guild count
	uniqueGuilds, err := m.layout.db.Models().Sync().GetUniqueGuildCount(ctx)
	if err != nil {
		m.layout.logger.Error("Failed to get unique guild count", zap.Error(err))
		uniqueGuilds = 0 // Default to 0 if there's an error
	}

	// Fetch unique user count
	uniqueUsers, err := m.layout.db.Models().Sync().GetUniqueUserCount(ctx)
	if err != nil {
		m.layout.logger.Error("Failed to get unique user count", zap.Error(err))
		uniqueUsers = 0 // Default to 0 if there's an error
	}

	// Fetch inappropriate user count
	inappropriateUsers, err := m.layout.db.Models().Message().GetUniqueInappropriateUserCount(ctx)
	if err != nil {
		m.layout.logger.Error("Failed to get inappropriate user count", zap.Error(err))
		inappropriateUsers = 0 // Default to 0 if there's an error
	}

	// Store the statistics in session keys
	session.GuildStatsUniqueGuilds.Set(s, uniqueGuilds)
	session.GuildStatsUniqueUsers.Set(s, uniqueUsers)
	session.GuildStatsInappropriateUsers.Set(s, inappropriateUsers)
}

// handleSelectMenu processes select menu interactions.
func (m *Menu) handleSelectMenu(
	event *events.ComponentInteractionCreate, s *session.Session, r *pagination.Respond, customID, option string,
) {
	if customID != constants.ActionSelectMenuCustomID {
		return
	}

	switch option {
	case constants.StartGuildScanButtonCustomID:
		// Set scan type to condo-based
		session.GuildScanType.Set(s, constants.GuildScanTypeCondo)

		// Reset scan page
		session.PaginationPage.Set(s, 0)
		session.PaginationTotalItems.Set(s, 0)

		r.Show(event, s, constants.GuildScanPageName, "")

	case constants.StartMessageScanButtonCustomID:
		// Set scan type to message-based
		session.GuildScanType.Set(s, constants.GuildScanTypeMessages)

		// Reset scan page
		session.PaginationPage.Set(s, 0)
		session.PaginationTotalItems.Set(s, 0)

		r.Show(event, s, constants.GuildScanPageName, "")

	case constants.ViewGuildBanLogsButtonCustomID:
		// Reset logs page
		session.LogCursor.Delete(s)
		session.LogNextCursor.Delete(s)
		session.LogPrevCursors.Delete(s)

		r.Show(event, s, constants.GuildLogsPageName, "")
	}
}

// handleButton processes button interactions.
func (m *Menu) handleButton(
	event *events.ComponentInteractionCreate, s *session.Session, r *pagination.Respond, customID string,
) {
	switch customID {
	case constants.BackButtonCustomID:
		r.NavigateBack(event, s, "")
	}
}
