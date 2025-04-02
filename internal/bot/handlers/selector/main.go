package selector

import (
	"strconv"

	"github.com/disgoorg/disgo/discord"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/interaction"
	"github.com/robalyx/rotector/internal/bot/core/session"
	view "github.com/robalyx/rotector/internal/bot/views/selector"
)

// Menu handles selector operations and their interactions.
type Menu struct {
	layout *Layout
	page   *interaction.Page
}

// NewMenu creates a new selector menu.
func NewMenu(layout *Layout) *Menu {
	m := &Menu{layout: layout}
	m.page = &interaction.Page{
		Name: constants.SessionSelectorPageName,
		Message: func(s *session.Session) *discord.MessageUpdateBuilder {
			return view.NewBuilder(s).Build()
		},
		ShowHandlerFunc:   m.Show,
		SelectHandlerFunc: m.handleSelectMenu,
	}
	return m
}

// Show prepares and displays the session selector interface.
func (m *Menu) Show(ctx *interaction.Context, s *session.Session) {
	// Get existing sessions for the user
	existingSessions, err := m.layout.sessionManager.GetUserSessions(ctx.Context(), uint64(ctx.Event().User().ID), false)
	if err != nil {
		ctx.Error("Failed to check existing sessions. Please try again.")
		return
	}

	// Store session information in the current session
	session.ExistingSessions.Set(s, existingSessions)
}

// handleSelectMenu processes select menu interactions.
func (m *Menu) handleSelectMenu(ctx *interaction.Context, s *session.Session, customID, option string) {
	if customID != constants.SelectorSelectMenuCustomID {
		return
	}

	if option == constants.SelectorNewButtonCustomID {
		// Continue with current session
		ctx.Show(constants.DashboardPageName, "Starting new session.")
		return
	}

	// Parse selected message ID
	selectedMessageID, err := strconv.ParseUint(option, 10, 64)
	if err != nil {
		ctx.Error("Failed to parse selected message ID.")
		return
	}

	// Get the selected session
	selectedSession, _, err := m.layout.sessionManager.GetOrCreateSession(
		ctx.Context(),
		ctx.Event().User().ID,
		selectedMessageID,
		session.IsGuildOwner.Get(s),
	)
	if err != nil {
		ctx.Error("Failed to load selected session.")
		return
	}

	// Get the data from the selected session
	selectedData := selectedSession.GetData()
	pageName := session.CurrentPage.Get(selectedSession)

	// Close the selected session
	m.layout.sessionManager.CloseSession(
		ctx.Context(), selectedSession, uint64(ctx.Event().User().ID), uint64(ctx.Event().Message().ID),
	)

	// Get the new message ID from the current interaction
	newMessageID := uint64(ctx.Event().Message().ID)

	// Update the current session with the data from the old session
	s.UpdateData(selectedData, newMessageID)

	// Show the page from the selected session
	ctx.Show(pageName, "Resumed existing session.")
}
