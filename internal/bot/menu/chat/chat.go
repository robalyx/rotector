package chat

import (
	"context"
	"fmt"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	builder "github.com/rotector/rotector/internal/bot/builder/chat"
	"github.com/rotector/rotector/internal/bot/constants"
	"github.com/rotector/rotector/internal/bot/core/pagination"
	"github.com/rotector/rotector/internal/bot/core/session"
	"github.com/rotector/rotector/internal/bot/interfaces"
	"github.com/rotector/rotector/internal/bot/utils"
	"github.com/rotector/rotector/internal/common/client/ai"
	"github.com/rotector/rotector/internal/common/storage/database/types"
	"go.uber.org/zap"
)

// Menu handles the display and interaction logic for AI chat.
type Menu struct {
	layout *Layout
	page   *pagination.Page
}

// NewMenu creates a new chat menu.
func NewMenu(layout *Layout) *Menu {
	m := &Menu{layout: layout}
	m.page = &pagination.Page{
		Name: "Chat Menu",
		Message: func(s *session.Session) *discord.MessageUpdateBuilder {
			return builder.NewBuilder(s).Build()
		},
		SelectHandlerFunc: m.handleSelectMenu,
		ButtonHandlerFunc: m.handleButton,
		ModalHandlerFunc:  m.handleModal,
	}
	return m
}

// Show prepares and displays the chat interface.
func (m *Menu) Show(event interfaces.CommonEvent, s *session.Session, content string) {
	m.layout.paginationManager.NavigateTo(event, s, m.page, content)
}

// handleButton processes button interactions.
func (m *Menu) handleButton(event *events.ComponentInteractionCreate, s *session.Session, customID string) {
	action := utils.ViewerAction(customID)
	switch action {
	case utils.ViewerFirstPage, utils.ViewerPrevPage, utils.ViewerNextPage, utils.ViewerLastPage:
		var history ai.ChatHistory
		s.GetInterface(constants.SessionKeyChatHistory, &history)

		maxPage := (len(history.Messages)/2 - 1) / constants.ChatMessagesPerPage
		page, ok := action.ParsePageAction(s, action, maxPage)
		if !ok {
			m.layout.paginationManager.RespondWithError(event, "Invalid interaction.")
			return
		}

		s.Set(constants.SessionKeyPaginationPage, page)
		m.Show(event, s, "")

	case constants.ChatSendButtonID:
		modal := discord.NewModalCreateBuilder().
			SetCustomID(constants.ChatInputModalID).
			SetTitle("Chat with AI").
			AddActionRow(
				discord.NewTextInput(constants.ChatInputCustomID, discord.TextInputStyleParagraph, "Message").
					WithRequired(true).
					WithMaxLength(512).
					WithPlaceholder("Type your message here..."),
			).
			Build()

		if err := event.Modal(modal); err != nil {
			m.layout.logger.Error("Failed to create modal", zap.Error(err))
			m.layout.paginationManager.RespondWithError(event, "Failed to open chat input. Please try again.")
		}

	case constants.BackButtonCustomID:
		m.layout.paginationManager.NavigateBack(event, s, "")

	case constants.ChatClearHistoryButtonID:
		// Clear chat history
		s.Set(constants.SessionKeyChatHistory, ai.ChatHistory{Messages: make([]*ai.ChatMessage, 0)})
		s.Set(constants.SessionKeyPaginationPage, 0)
		m.Show(event, s, "Chat history cleared.")

	case constants.ChatClearContextButtonID:
		s.Delete(constants.SessionKeyChatContext)
		m.Show(event, s, "Context cleared.")
	}
}

// handleSelectMenu processes select menu interactions.
func (m *Menu) handleSelectMenu(event *events.ComponentInteractionCreate, s *session.Session, customID string, option string) {
	switch customID {
	case constants.ChatModelSelectID:
		// Get user settings
		var settings *types.UserSetting
		s.GetInterface(constants.SessionKeyUserSettings, &settings)

		// Update user settings with new chat model
		chatModel := types.ChatModel(option)
		settings.ChatModel = chatModel
		if err := m.layout.db.Settings().SaveUserSettings(context.Background(), settings); err != nil {
			m.layout.logger.Error("Failed to save target mode setting", zap.Error(err))
			m.layout.paginationManager.RespondWithError(event, "Failed to switch target mode. Please try again.")
			return
		}

		// Update session and refresh the menu
		s.Set(constants.SessionKeyUserSettings, settings)
		m.Show(event, s, fmt.Sprintf("Switched to %s model", chatModel.FormatDisplay()))
	}
}

// handleModal processes modal submissions for chat input.
func (m *Menu) handleModal(event *events.ModalSubmitInteractionCreate, s *session.Session) {
	switch event.Data.CustomID {
	case constants.ChatInputModalID:
		message := event.Data.Text(constants.ChatInputCustomID)
		if message == "" {
			m.layout.paginationManager.RespondWithError(event, "Message cannot be empty")
			return
		}

		// Prepend context if available
		var msgContext string
		s.GetInterface(constants.SessionKeyChatContext, &msgContext)
		if msgContext != "" {
			message = msgContext + "\n\n" + message
			s.Delete(constants.SessionKeyChatContext)
		}

		// Set streaming state
		s.Set(constants.SessionKeyIsStreaming, true)

		// Show "AI is typing..." message
		m.layout.paginationManager.NavigateTo(event, s, m.page, "AI is typing...")

		// Get user settings and chat history
		var history ai.ChatHistory
		s.GetInterface(constants.SessionKeyChatHistory, &history)
		var userSettings *types.UserSetting
		s.GetInterface(constants.SessionKeyUserSettings, &userSettings)

		// Stream AI response
		ctx := context.Background()
		responseChan := m.layout.chatHandler.StreamResponse(ctx, history.ToGenAIHistory(), string(userSettings.ChatModel), message)

		// Add messages to history
		userMessage := &ai.ChatMessage{
			Role:    "user",
			Content: message,
		}
		aiMessage := &ai.ChatMessage{
			Role:    "model",
			Content: "",
		}
		history.Messages = append(history.Messages, userMessage, aiMessage)
		s.Set(constants.SessionKeyChatHistory, history)

		// Stream AI response
		var lastUpdate time.Time
		var aiResponse string
		for response := range responseChan {
			aiResponse += response

			// Update message at most once per second to avoid rate limits
			if time.Since(lastUpdate) > 1*time.Second {
				// Update the latest AI message's content
				history.Messages[len(history.Messages)-1].Content = aiResponse
				s.Set(constants.SessionKeyChatHistory, history)

				// Update display
				m.layout.paginationManager.NavigateTo(event, s, m.page, "Receiving response...")
				lastUpdate = time.Now()
			}
		}

		// Prevent rate limit
		time.Sleep(1 * time.Second)

		// Do final update to ensure we have the complete response
		history.Messages[len(history.Messages)-1].Content = aiResponse
		s.Set(constants.SessionKeyChatHistory, history)

		// Calculate new page number to show latest messages
		s.Set(constants.SessionKeyPaginationPage, 0)
		s.Set(constants.SessionKeyIsStreaming, false)

		// Show final message
		m.layout.paginationManager.NavigateTo(event, s, m.page, "Response completed.")
	}
}
