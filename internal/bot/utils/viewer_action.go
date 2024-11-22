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
func (h *ViewerAction) ParsePageAction(s *session.Session, action ViewerAction, maxPage int) (int, bool) {
	switch action {
	case ViewerFirstPage:
		s.Set(constants.SessionKeyPaginationPage, 0)
		return 0, true

	case ViewerPrevPage:
		prevPage := s.GetInt(constants.SessionKeyPaginationPage) - 1
		if prevPage < 0 {
			prevPage = 0
		}
		s.Set(constants.SessionKeyPaginationPage, prevPage)
		return prevPage, true

	case ViewerNextPage:
		nextPage := s.GetInt(constants.SessionKeyPaginationPage) + 1
		if nextPage > maxPage {
			nextPage = maxPage
		}
		s.Set(constants.SessionKeyPaginationPage, nextPage)
		return nextPage, true

	case ViewerLastPage:
		s.Set(constants.SessionKeyPaginationPage, maxPage)
		return maxPage, true

	default:
		return 0, false
	} //exhaustive:ignore
}
