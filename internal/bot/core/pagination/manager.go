package pagination

import (
	"context"
	"strconv"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/rotector/rotector/internal/bot/constants"
	"github.com/rotector/rotector/internal/bot/core/session"
	"github.com/rotector/rotector/internal/bot/interfaces"
	"github.com/rotector/rotector/internal/bot/utils"
	"go.uber.org/zap"
)

// Page holds the structure for a single interactive page, containing handlers for different
// types of Discord interactions (select menus, buttons, modals) and a message builder function.
// The handlers are optional - a page may only use some interaction types.
type Page struct {
	Name    string
	Message func(s *session.Session) *discord.MessageUpdateBuilder

	// SelectHandlerFunc processes select menu interactions by taking the selected option
	// and custom ID to determine what action to take
	SelectHandlerFunc func(
		event *events.ComponentInteractionCreate,
		s *session.Session,
		customID string,
		option string,
	)
	// ButtonHandlerFunc processes button clicks by using the button's custom ID
	// to determine what action to take
	ButtonHandlerFunc func(
		event *events.ComponentInteractionCreate,
		s *session.Session,
		customID string,
	)
	// ModalHandlerFunc processes form submissions from Discord modals
	// by reading the submitted values
	ModalHandlerFunc func(
		event *events.ModalSubmitInteractionCreate,
		s *session.Session,
	)
	// BackHandlerFunc is called when navigating away from this page
	BackHandlerFunc func()
}

// Manager maintains a map of pages indexed by their names and handles
// the routing of Discord interactions to the appropriate page handlers.
type Manager struct {
	sessionManager *session.Manager
	pages          map[string]*Page
	logger         *zap.Logger
}

// NewManager initializes a new Manager with an empty pages map
// and the provided logger for debugging interaction handling.
func NewManager(sessionManager *session.Manager, logger *zap.Logger) *Manager {
	return &Manager{
		sessionManager: sessionManager,
		pages:          make(map[string]*Page),
		logger:         logger,
	}
}

// AddPage stores a page in the manager's pages map using the page's name as the key.
func (m *Manager) AddPage(page *Page) {
	m.pages[page.Name] = page
}

// GetPage retrieves a page from the manager's pages map using the provided name.
func (m *Manager) GetPage(name string) *Page {
	return m.pages[name]
}

// HandleInteraction routes Discord interactions to the appropriate handler function
// based on the interaction type (select menu, button, or modal) and the current page.
// If no handler is found for an interaction, an error is logged.
func (m *Manager) HandleInteraction(event interfaces.CommonEvent, s *session.Session) {
	currentPage := s.GetString(constants.SessionKeyCurrentPage)
	page := m.GetPage(currentPage)

	switch e := event.(type) {
	case *events.ComponentInteractionCreate:
		switch data := e.Data.(type) {
		case discord.StringSelectMenuInteractionData:
			if page.SelectHandlerFunc != nil {
				page.SelectHandlerFunc(e, s, data.CustomID(), data.Values[0])
				m.logger.Debug("Select interaction", zap.String("customID", data.CustomID()), zap.String("option", data.Values[0]))
			} else {
				m.logger.Error("No select handler found for customID", zap.String("customID", data.CustomID()))
			}
		case discord.ButtonInteractionData:
			if page.ButtonHandlerFunc != nil {
				page.ButtonHandlerFunc(e, s, data.CustomID())
				m.logger.Debug("Button interaction", zap.String("customID", data.CustomID()))
			} else {
				m.logger.Error("No button handler found for customID", zap.String("customID", data.CustomID()))
			}
		}
	case *events.ModalSubmitInteractionCreate:
		if page.ModalHandlerFunc != nil {
			page.ModalHandlerFunc(e, s)
			m.logger.Debug("Modal submit interaction", zap.String("customID", e.Data.CustomID))
		} else {
			m.logger.Error("No modal handler found for customID", zap.String("customID", e.Data.CustomID))
		}
	}
}

// NavigateTo updates the Discord message with new content and components for the target page.
// It stores the page history in the session, allowing for nested navigation.
func (m *Manager) NavigateTo(event interfaces.CommonEvent, s *session.Session, page *Page, content string) {
	// Update the message with the new content and components
	messageUpdate := page.Message(s).
		SetContent(utils.GetTimestampedSubtext(content)).
		RetainAttachments().
		Build()

	message, err := event.Client().Rest().UpdateInteractionResponse(event.ApplicationID(), event.Token(), messageUpdate)
	if err != nil {
		m.logger.Error("Failed to update interaction response", zap.Error(err))
	}

	// Update the page history in the session
	m.UpdatePage(s, page)

	// Set the message ID in the session
	s.Set(constants.SessionKeyMessageID, strconv.FormatUint(uint64(message.ID), 10))

	m.logger.Debug("Updated message",
		zap.String("page", page.Name),
		zap.Uint64("message_id", uint64(message.ID)))
}

// UpdatePage updates the session with a new page.
func (m *Manager) UpdatePage(s *session.Session, newPage *Page) {
	currentPage := s.GetString(constants.SessionKeyCurrentPage)
	if currentPage != "" && currentPage != newPage.Name {
		// Get existing page history
		var previousPages []string
		s.GetInterface(constants.SessionKeyPreviousPages, &previousPages)

		// Check if new page exists in history
		for i, page := range previousPages {
			if page == newPage.Name {
				// Found the page in history, revert back to its state
				previousPages = previousPages[:i]
				s.Set(constants.SessionKeyPreviousPages, previousPages)
				s.Set(constants.SessionKeyCurrentPage, newPage.Name)
				return
			}
		}

		// Page not in history, append current page
		previousPages = append(previousPages, currentPage)
		s.Set(constants.SessionKeyPreviousPages, previousPages)
	}

	s.Set(constants.SessionKeyCurrentPage, newPage.Name)
}

// NavigateBack navigates back to the previous page in the history.
func (m *Manager) NavigateBack(event interfaces.CommonEvent, s *session.Session, content string) {
	var previousPages []string
	s.GetInterface(constants.SessionKeyPreviousPages, &previousPages)

	if len(previousPages) > 0 {
		// Get the last page from history
		lastIdx := len(previousPages) - 1
		previousPage := previousPages[lastIdx]

		// Navigate to the previous page
		page := m.GetPage(previousPage)
		m.NavigateTo(event, s, page, content)
	}
}

// Refresh reloads the current page in the session.
func (m *Manager) Refresh(event interfaces.CommonEvent, s *session.Session, content string) {
	currentPage := s.GetString(constants.SessionKeyCurrentPage)
	page := m.GetPage(currentPage)
	m.NavigateTo(event, s, page, content)
}

// RespondWithError clears all message components and embeds, replacing them with
// a timestamped error message. This is used when an unrecoverable error occurs
// during interaction handling.
func (m *Manager) RespondWithError(event interfaces.CommonEvent, message string) {
	messageUpdate := discord.NewMessageUpdateBuilder().
		SetContent(utils.GetTimestampedSubtext("Fatal error: " + message)).
		ClearEmbeds().
		ClearFiles().
		ClearContainerComponents().
		RetainAttachments().
		Build()

	_, _ = event.Client().Rest().UpdateInteractionResponse(event.ApplicationID(), event.Token(), messageUpdate)
	m.sessionManager.CloseSession(context.Background(), uint64(event.User().ID))
}
