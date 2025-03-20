package user

import (
	"strings"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	builder "github.com/robalyx/rotector/internal/bot/builder/review/user"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/pagination"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/bot/interfaces"
)

// CaesarMenu handles the display of Caesar cipher translations for a user's description.
type CaesarMenu struct {
	layout *Layout
	page   *pagination.Page
}

// NewCaesarMenu creates a new Caesar cipher menu.
func NewCaesarMenu(layout *Layout) *CaesarMenu {
	m := &CaesarMenu{layout: layout}
	m.page = &pagination.Page{
		Name: constants.UserCaesarPageName,
		Message: func(s *session.Session) *discord.MessageUpdateBuilder {
			return builder.NewCaesarBuilder(s, m.layout.translator).Build()
		},
		ShowHandlerFunc:   m.Show,
		ButtonHandlerFunc: m.handleButton,
	}
	return m
}

// Show displays the Caesar cipher analysis interface.
func (m *CaesarMenu) Show(event interfaces.CommonEvent, s *session.Session, r *pagination.Respond) {
	user := session.UserTarget.Get(s)
	if strings.TrimSpace(user.Description) == "" {
		r.Cancel(event, s, "No description available for analysis.")
		return
	}

	// Calculate page boundaries
	totalPages := (constants.CaesarTotalTranslations + constants.CaesarTranslationsPerPage - 1) /
		constants.CaesarTranslationsPerPage

	page := session.PaginationPage.Get(s)
	if page < 0 || page >= totalPages {
		page = 0 // Reset to first page if out of bounds
	}

	start := page * constants.CaesarTranslationsPerPage

	// Store pagination data in session
	session.PaginationPage.Set(s, page)
	session.PaginationOffset.Set(s, start)
	session.PaginationTotalItems.Set(s, constants.CaesarTotalTranslations)
	session.PaginationTotalPages.Set(s, totalPages)
}

// handleButton processes button interactions.
func (m *CaesarMenu) handleButton(
	event *events.ComponentInteractionCreate, s *session.Session, r *pagination.Respond, customID string,
) {
	action := session.ViewerAction(customID)
	switch action {
	case session.ViewerFirstPage, session.ViewerPrevPage, session.ViewerNextPage, session.ViewerLastPage:
		// Calculate max page
		totalPages := session.PaginationTotalPages.Get(s)
		page := action.ParsePageAction(s, totalPages-1)

		session.PaginationPage.Set(s, page)
		r.Reload(event, s, "")
		return
	}

	switch customID {
	case constants.BackButtonCustomID:
		r.NavigateBack(event, s, "")
	}
}
