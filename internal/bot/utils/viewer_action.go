package utils

import (
	"github.com/rotector/rotector/internal/bot/constants"
	"github.com/rotector/rotector/internal/bot/core/session"
)

// ViewerAction represents the type of page navigation action.
type ViewerAction string

// Navigation actions for moving between pages in a paginated view.
const (
	// ViewerFirstPage moves to the first available page.
	ViewerFirstPage ViewerAction = "first_page"
	// ViewerPrevPage moves to the previous page if available.
	ViewerPrevPage ViewerAction = "prev_page"
	// ViewerNextPage moves to the next page if available.
	ViewerNextPage ViewerAction = "next_page"
	// ViewerLastPage moves to the last available page.
	ViewerLastPage ViewerAction = "last_page"
)

// ParsePageAction updates the session's pagination page based on the requested action.
// Returns the new page number and true if the action was valid, or 0 and false if invalid.
// The maxPage parameter prevents navigation beyond the available pages.
func (h *ViewerAction) ParsePageAction(s *session.Session, action ViewerAction, maxPage int) int {
	var page int

	switch action {
	case ViewerFirstPage:
		s.Set(constants.SessionKeyPaginationPage, 0)
		page = 0
	case ViewerPrevPage:
		page = s.GetInt(constants.SessionKeyPaginationPage) - 1
		if page < 0 {
			page = 0
		}
		s.Set(constants.SessionKeyPaginationPage, page)
	case ViewerNextPage:
		page = s.GetInt(constants.SessionKeyPaginationPage) + 1
		if page > maxPage {
			page = maxPage
		}
		s.Set(constants.SessionKeyPaginationPage, page)
	case ViewerLastPage:
		s.Set(constants.SessionKeyPaginationPage, maxPage)
		page = maxPage
	}

	return page
}
