package pagination

import (
	"context"

	"github.com/disgoorg/disgo/discord"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/bot/interfaces"
	"github.com/robalyx/rotector/internal/bot/utils"
)

// Respond is a helper struct that handles responding to Discord interactions.
type Respond struct {
	sessionManager    *session.Manager
	paginationManager *Manager
	responded         bool
}

// NewRespond creates a new Respond instance.
func NewRespond(sessionManager *session.Manager, paginationManager *Manager) *Respond {
	return &Respond{
		sessionManager:    sessionManager,
		paginationManager: paginationManager,
		responded:         false,
	}
}

// Error updates the interaction response with an error message.
func (r *Respond) Error(event interfaces.CommonEvent, message string) {
	messageUpdate := discord.NewMessageUpdateBuilder().
		SetContent(utils.GetTimestampedSubtext("Fatal error: " + message)).
		ClearEmbeds().
		ClearFiles().
		ClearContainerComponents().
		RetainAttachments().
		Build()

	_, _ = event.Client().Rest().UpdateInteractionResponse(event.ApplicationID(), event.Token(), messageUpdate)
	r.sessionManager.CloseSession(context.Background(), uint64(event.User().ID))
	r.responded = true
}

// Clear updates the interaction response with a clear message.
func (r *Respond) Clear(event interfaces.CommonEvent, content string) {
	messageUpdate := discord.NewMessageUpdateBuilder().
		SetContent(content).
		ClearEmbeds().
		ClearFiles().
		ClearContainerComponents().
		RetainAttachments().
		Build()

	_, _ = event.Client().Rest().UpdateInteractionResponse(event.ApplicationID(), event.Token(), messageUpdate)
	r.responded = true
}

// NavigateBack navigates back to the previous page in the history.
func (r *Respond) NavigateBack(event interfaces.CommonEvent, s *session.Session, content string) {
	previousPages := session.PreviousPages.Get(s)

	if len(previousPages) > 0 {
		// Get the last page from history
		lastIdx := len(previousPages) - 1
		previousPage := previousPages[lastIdx]

		// Call the cleanup handler for the current page
		currentPage := r.paginationManager.GetPage(session.CurrentPage.Get(s))
		if currentPage.CleanupHandlerFunc != nil {
			currentPage.CleanupHandlerFunc(s)
		}

		// Navigate to the previous page
		r.paginationManager.Show(event, s, previousPage, content)
	} else {
		r.Cancel(event, s, content)
	}
	r.responded = true
}

// Cancel stops the loading of the new page in the session and refreshes with a new message.
func (r *Respond) Cancel(event interfaces.CommonEvent, s *session.Session, message string) {
	page := r.paginationManager.GetPage(session.CurrentPage.Get(s))
	r.paginationManager.Display(event, s, page, message)
	r.responded = true
}

// Reload reloads the page being processed with a new message.
func (r *Respond) Reload(event interfaces.CommonEvent, s *session.Session, content string) {
	pageName := session.CurrentPage.Get(s)
	r.paginationManager.Show(event, s, pageName, content)
	r.responded = true
}

// Show updates the Discord message with new content and components for the target page.
func (r *Respond) Show(event interfaces.CommonEvent, s *session.Session, pageName, content string) {
	r.paginationManager.Show(event, s, pageName, content)
	r.responded = true
}

// UpdatePage updates the session with a new page.
// This does not refresh or reload the page, it just updates the page in the session.
func (r *Respond) UpdatePage(s *session.Session, pageName string) {
	page := r.paginationManager.GetPage(pageName)
	r.paginationManager.UpdatePage(s, page)
	r.responded = true
}
