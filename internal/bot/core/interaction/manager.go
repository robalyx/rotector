package interaction

import (
	"context"

	"github.com/disgoorg/disgo/discord"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/bot/utils"
	"go.uber.org/zap"
)

// Page holds the structure for a single interactive page, containing handlers for different
// types of Discord interactions (select menus, buttons, modals) and a message builder function.
// The handlers are optional - a page may only use some interaction types.
type Page struct {
	Name    string
	Message func(s *session.Session) *discord.MessageUpdateBuilder

	// ShowHandlerFunc is called when the page is shown.
	ShowHandlerFunc func(
		ctx *Context,
		s *session.Session,
	)

	// ResetHandlerFunc is called when the page is opened for the first time.
	ResetHandlerFunc func(
		s *session.Session,
	)
	// CleanupHandlerFunc is called when the page is closed.
	CleanupHandlerFunc func(
		s *session.Session,
	)

	// SelectHandlerFunc processes select menu interactions.
	SelectHandlerFunc func(
		ctx *Context,
		s *session.Session,
		customID,
		option string,
	)
	// ButtonHandlerFunc processes button clicks.
	ButtonHandlerFunc func(
		ctx *Context,
		s *session.Session,
		customID string,
	)
	// ModalHandlerFunc processes form submissions.
	ModalHandlerFunc func(
		ctx *Context,
		s *session.Session,
	)
}

// Manager maintains a map of pages indexed by their names and handles
// the routing of Discord interactions to the appropriate page handlers.
type Manager struct {
	sessionManager *session.Manager
	pages          map[string]*Page
	logger         *zap.Logger
}

// NewManager initializes a new pagination manager.
func NewManager(sessionManager *session.Manager, logger *zap.Logger) *Manager {
	return &Manager{
		sessionManager: sessionManager,
		pages:          make(map[string]*Page),
		logger:         logger.Named("pagination"),
	}
}

// AddPages stores pages for the manager using their names as keys.
func (m *Manager) AddPages(pages []*Page) {
	for _, page := range pages {
		m.pages[page.Name] = page
	}
}

// GetPage retrieves a page from the manager's pages using the provided name.
func (m *Manager) GetPage(name string) *Page {
	return m.pages[name]
}

// HandleInteraction routes Discord interactions to the appropriate handler function
// based on the interaction type (select menu, button, or modal) and the current page.
// If no handler is found for an interaction, an error is logged.
func (m *Manager) HandleInteraction(event CommonEvent, s *session.Session) {
	currentPage := session.CurrentPage.Get(s)
	page := m.GetPage(currentPage)

	ctx := New(context.Background(), event, s, m)

	switch e := event.(type) {
	case *ComponentEvent:
		switch data := e.Data.(type) {
		case discord.StringSelectMenuInteractionData:
			if page.SelectHandlerFunc != nil {
				m.logger.Debug("Select interaction", zap.String("customID", data.CustomID()), zap.String("option", data.Values[0]))
				page.SelectHandlerFunc(ctx, s, data.CustomID(), data.Values[0])
			} else {
				m.logger.Error("No select handler found for customID", zap.String("customID", data.CustomID()))
			}
		case discord.ButtonInteractionData:
			if page.ButtonHandlerFunc != nil {
				m.logger.Debug("Button interaction", zap.String("customID", data.CustomID()))
				page.ButtonHandlerFunc(ctx, s, data.CustomID())
			} else {
				m.logger.Error("No button handler found for customID", zap.String("customID", data.CustomID()))
			}
		}
	case *ModalSubmitEvent:
		if page.ModalHandlerFunc != nil {
			m.logger.Debug("Modal submit interaction", zap.String("customID", e.Data.CustomID))
			page.ModalHandlerFunc(ctx, s)
		} else {
			m.logger.Error("No modal handler found for customID", zap.String("customID", e.Data.CustomID))
		}
	}
}

// Show updates the Discord message with new content and components for the target page.
func (m *Manager) Show(event CommonEvent, s *session.Session, pageName, content string) {
	page := m.GetPage(pageName)
	if page == nil {
		m.logger.Error("Page not found", zap.String("pageName", pageName))
		return
	}

	// Handle the page show event
	responded := false
	if page.ShowHandlerFunc != nil {
		ctx := New(context.Background(), event, s, m)
		page.ShowHandlerFunc(ctx, s)
		responded = ctx.responded
	}

	// Display the page to the user if it wasn't handled by the handler
	if !responded {
		m.Display(event, s, page, content)
	}
}

// Display updates the page in the session and displays it to the user.
func (m *Manager) Display(
	event CommonEvent, s *session.Session, page *Page, content string, files ...*discord.File,
) {
	// Update the message with the new content and components
	messageUpdate := page.Message(s).
		SetContent(utils.GetTimestampedSubtext(content)).
		RetainAttachments().
		AddFiles(files...).
		Build()

	message, err := event.Client().Rest().UpdateInteractionResponse(event.ApplicationID(), event.Token(), messageUpdate)
	if err != nil {
		m.logger.Error("Failed to update interaction response", zap.Error(err))
	}

	// Update the page history in the session
	m.UpdatePage(s, page)

	m.logger.Debug("Updated message",
		zap.String("page", page.Name),
		zap.Uint64("message_id", uint64(message.ID)))
}

// UpdatePage updates the session with a new page.
func (m *Manager) UpdatePage(s *session.Session, newPage *Page) {
	currentPage := session.CurrentPage.Get(s)
	if currentPage != "" && currentPage != newPage.Name {
		// Get existing page history
		previousPages := session.PreviousPages.Get(s)

		// Check if new page exists in history
		for i, page := range previousPages {
			if page == newPage.Name {
				// Found the page in history, revert back to its state
				previousPages = previousPages[:i]
				session.PreviousPages.Set(s, previousPages)
				session.CurrentPage.Set(s, newPage.Name)
				return
			}
		}

		// Page not in history, append current page
		previousPages = append(previousPages, currentPage)
		session.PreviousPages.Set(s, previousPages)

		// Reset the page if it has a reset handler
		if newPage.ResetHandlerFunc != nil {
			newPage.ResetHandlerFunc(s)
		}
	}

	session.CurrentPage.Set(s, newPage.Name)
}

// RespondWithError updates the interaction response with an error message.
func (m *Manager) RespondWithError(event CommonEvent, message string) {
	ctx := New(context.Background(), event, nil, m)
	ctx.Error(message)
}

// RespondWithClear updates the interaction response with a message.
// This also clears message embeds, files, and container components.
func (m *Manager) RespondWithClear(event CommonEvent, message string) {
	ctx := New(context.Background(), event, nil, m)
	ctx.Clear(message)
}
