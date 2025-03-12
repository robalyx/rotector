package pagination

import (
	"context"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/bot/interfaces"
	"github.com/robalyx/rotector/internal/bot/utils"
	"go.uber.org/zap"
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

	r.paginationManager.logger.Debug("Fatal error",
		zap.Uint64("user_id", uint64(event.User().ID)),
		zap.String("message", message),
	)
}

// Clear updates the interaction response with a message and clears the embeds, files, and components.
func (r *Respond) Clear(event interfaces.CommonEvent, content string) {
	messageUpdate := discord.NewMessageUpdateBuilder().
		SetContent(utils.GetTimestampedSubtext(content)).
		ClearEmbeds().
		ClearFiles().
		ClearContainerComponents().
		RetainAttachments().
		Build()

	_, _ = event.Client().Rest().UpdateInteractionResponse(event.ApplicationID(), event.Token(), messageUpdate)
	r.responded = true
}

// ClearComponents updates the interaction response with a message and clears the components.
func (r *Respond) ClearComponents(event interfaces.CommonEvent, content string) {
	messageUpdate := discord.NewMessageUpdateBuilder().
		SetContent(utils.GetTimestampedSubtext(content)).
		ClearContainerComponents().
		Build()

	_, _ = event.Client().Rest().UpdateInteractionResponse(event.ApplicationID(), event.Token(), messageUpdate)
	r.responded = true
}

// RespondWithFiles updates the interaction response with a message and file attachments.
func (r *Respond) RespondWithFiles(
	event interfaces.CommonEvent, s *session.Session, content string, files ...*discord.File,
) {
	page := r.paginationManager.GetPage(session.CurrentPage.Get(s))
	r.paginationManager.Display(event, s, page, content, files...)
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
		s.Touch(context.Background())
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
	s.Touch(context.Background())
	r.responded = true
}

// Show updates the Discord message with new content and components for the target page.
func (r *Respond) Show(event interfaces.CommonEvent, s *session.Session, pageName, content string) {
	r.paginationManager.Show(event, s, pageName, content)
	s.Touch(context.Background())
	r.responded = true
}

// UpdatePage updates the session with a new page.
// This does not refresh or reload the page, it just updates the page in the session.
func (r *Respond) UpdatePage(s *session.Session, pageName string) {
	page := r.paginationManager.GetPage(pageName)
	r.paginationManager.UpdatePage(s, page)
	r.responded = true
}

// Modal updates the interaction response with a modal.
func (r *Respond) Modal(event *events.ComponentInteractionCreate, s *session.Session, modal *discord.ModalCreateBuilder) {
	if err := event.Modal(modal.Build()); err != nil {
		r.paginationManager.logger.Error("Failed to create modal",
			zap.Error(err),
			zap.String("custom_id", modal.CustomID),
			zap.String("title", modal.Title),
		)
		r.Error(event, "Failed to open the modal. Please try again.")
		return
	}
	s.Touch(context.Background())
	r.responded = true

	// WORKAROUND:
	// This fixes a problem with Discord's select menu behavior. When a modal is opened,
	// the selected option in the dropdown remains selected even if the user exits the modal.
	// This prevents the user from selecting the same option again since Discord doesn't
	// automatically reset the selection. By updating the message after opening a modal,
	// we force the select menu to reset to its default state.
	page := r.paginationManager.GetPage(session.CurrentPage.Get(s))
	_, err := event.Client().Rest().UpdateInteractionResponse(
		event.ApplicationID(),
		event.Token(),
		page.Message(s).Build(),
	)
	if err != nil {
		r.Error(event,
			"Failed to implement workaround for Discord's select menu behavior. Please report this issue.",
		)
	}
}
