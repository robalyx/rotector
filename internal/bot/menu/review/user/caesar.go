package user

import (
	"strings"

	"github.com/disgoorg/disgo/discord"
	builder "github.com/robalyx/rotector/internal/bot/builder/review/user"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/interaction"
	"github.com/robalyx/rotector/internal/bot/core/session"
)

// CaesarMenu handles the display of Caesar cipher translations for a user's description.
type CaesarMenu struct {
	layout *Layout
	page   *interaction.Page
}

// NewCaesarMenu creates a new Caesar cipher menu.
func NewCaesarMenu(layout *Layout) *CaesarMenu {
	m := &CaesarMenu{layout: layout}
	m.page = &interaction.Page{
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
func (m *CaesarMenu) Show(ctx *interaction.Context, s *session.Session) {
	user := session.UserTarget.Get(s)
	if strings.TrimSpace(user.Description) == "" {
		ctx.Cancel("No description available for analysis.")
		return
	}

	// Calculate page boundaries
	page := session.PaginationPage.Get(s)
	totalPages := max((constants.CaesarTotalTranslations-1)/constants.CaesarTranslationsPerPage, 0)

	// Store pagination data in session
	session.PaginationOffset.Set(s, page*constants.CaesarTranslationsPerPage)
	session.PaginationTotalItems.Set(s, constants.CaesarTotalTranslations)
	session.PaginationTotalPages.Set(s, totalPages)
}

// handleButton processes button interactions.
func (m *CaesarMenu) handleButton(ctx *interaction.Context, s *session.Session, customID string) {
	action := session.ViewerAction(customID)
	switch action {
	case session.ViewerFirstPage, session.ViewerPrevPage, session.ViewerNextPage, session.ViewerLastPage:
		totalPages := session.PaginationTotalPages.Get(s)
		page := action.ParsePageAction(s, totalPages)

		session.PaginationPage.Set(s, page)
		ctx.Reload("")
		return
	}

	switch customID {
	case constants.BackButtonCustomID:
		ctx.NavigateBack("")
	}
}
