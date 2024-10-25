package pagination

import (
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/rotector/rotector/internal/bot/constants"
	"github.com/rotector/rotector/internal/bot/interfaces"
	"github.com/rotector/rotector/internal/bot/session"
	"github.com/rotector/rotector/internal/bot/utils"
	"go.uber.org/zap"
)

// Page represents a single page in the pagination system.
type Page struct {
	Name    string
	Message func(s *session.Session) *discord.MessageUpdateBuilder

	SelectHandlerFunc func(
		event *events.ComponentInteractionCreate,
		s *session.Session,
		customID string,
		option string,
	)
	ButtonHandlerFunc func(
		event *events.ComponentInteractionCreate,
		s *session.Session,
		customID string,
	)
	ModalHandlerFunc func(
		event *events.ModalSubmitInteractionCreate,
		s *session.Session,
	)
	BackHandlerFunc func()
}

// Manager handles the pagination system.
type Manager struct {
	pages  map[string]*Page
	logger *zap.Logger
}

// NewManager creates a new Manager.
func NewManager(logger *zap.Logger) *Manager {
	return &Manager{
		pages:  make(map[string]*Page),
		logger: logger,
	}
}

// AddPage adds a new page to the Manager.
func (m *Manager) AddPage(page *Page) {
	m.pages[page.Name] = page
}

// GetPage retrieves a page by its name.
func (m *Manager) GetPage(name string) *Page {
	return m.pages[name]
}

// HandleInteraction processes interactions and updates the session.
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
				m.logger.Warn("No select handler found for customID", zap.String("customID", data.CustomID()))
			}
		case discord.ButtonInteractionData:
			if page.ButtonHandlerFunc != nil {
				page.ButtonHandlerFunc(e, s, data.CustomID())
				m.logger.Debug("Button interaction", zap.String("customID", data.CustomID()))
			} else {
				m.logger.Warn("No button handler found for customID", zap.String("customID", data.CustomID()))
			}
		}
	case *events.ModalSubmitInteractionCreate:
		if page.ModalHandlerFunc != nil {
			page.ModalHandlerFunc(e, s)
			m.logger.Debug("Modal submit interaction", zap.String("customID", e.Data.CustomID))
		} else {
			m.logger.Warn("No modal handler found for customID", zap.String("customID", e.Data.CustomID))
		}
	}
}

// UpdateMessage updates the message with the current page content.
func (m *Manager) UpdateMessage(event interfaces.CommonEvent, s *session.Session, page *Page, content string) {
	messageUpdate := page.Message(s).
		SetContent(utils.GetTimestampedSubtext(content)).
		RetainAttachments().
		Build()

	message, err := event.Client().Rest().UpdateInteractionResponse(event.ApplicationID(), event.Token(), messageUpdate)
	if err != nil {
		m.logger.Error("Failed to update interaction response", zap.Error(err))
	}

	s.Set(constants.SessionKeyMessageID, message.ID.String())
}

// NavigateTo navigates to a specific page.
func (m *Manager) NavigateTo(pageName string, s *session.Session) {
	s.Set(constants.SessionKeyCurrentPage, pageName)
	m.logger.Debug("Navigating to page", zap.String("page", pageName))
}
