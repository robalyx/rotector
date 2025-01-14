package chat

import (
	"context"
	"fmt"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/google/generative-ai-go/genai"
	builder "github.com/robalyx/rotector/internal/bot/builder/chat"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/pagination"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/bot/interfaces"
	"github.com/robalyx/rotector/internal/bot/utils"
	"github.com/robalyx/rotector/internal/common/client/ai"
	"github.com/robalyx/rotector/internal/common/storage/database/types"
	"github.com/robalyx/rotector/internal/common/storage/database/types/enum"
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
		page := action.ParsePageAction(s, action, maxPage)

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

		// Parse option to chat model
		chatModel, err := enum.ChatModelString(option)
		if err != nil {
			m.layout.logger.Error("Failed to parse chat model", zap.Error(err))
			m.layout.paginationManager.RespondWithError(event, "Failed to parse chat model. Please try again.")
			return
		}

		// Update user settings with new chat model
		settings.ChatModel = chatModel
		if err := m.layout.db.Settings().SaveUserSettings(context.Background(), settings); err != nil {
			m.layout.logger.Error("Failed to save chat model setting", zap.Error(err))
			m.layout.paginationManager.RespondWithError(event, "Failed to switch chat model. Please try again.")
			return
		}

		// Update session and refresh the menu
		s.Set(constants.SessionKeyUserSettings, settings)
		m.Show(event, s, fmt.Sprintf("Switched to %s model", chatModel.String()))
	}
}

// handleModal processes modal submissions for chat input.
func (m *Menu) handleModal(event *events.ModalSubmitInteractionCreate, s *session.Session) {
	switch event.Data.CustomID {
	case constants.ChatInputModalID:
		message := event.Data.Text(constants.ChatInputCustomID)
		if message == "" {
			m.layout.paginationManager.NavigateTo(event, s, m.page, "Message cannot be empty")
			return
		}

		// Get user settings
		var userSettings *types.UserSetting
		s.GetInterface(constants.SessionKeyUserSettings, &userSettings)

		// Check message limits
		if allowed, errMsg := m.checkMessageLimits(s, userSettings); !allowed {
			m.layout.paginationManager.NavigateTo(event, s, m.page, errMsg)
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

		// Get chat history
		var history ai.ChatHistory
		s.GetInterface(constants.SessionKeyChatHistory, &history)

		// Stream AI response
		responseChan, historyChan := m.layout.chatHandler.StreamResponse(
			context.Background(),
			history.ToGenAIHistory(),
			userSettings.ChatModel.String(),
			message,
		)

		// Stream AI response
		var lastUpdate time.Time
		var aiResponse string
		for response := range responseChan {
			aiResponse += response

			// Update message at most once per second to avoid rate limits
			if time.Since(lastUpdate) > 1*time.Second {
				m.layout.paginationManager.NavigateTo(event, s, m.page, "Receiving response...")
				lastUpdate = time.Now()
			}
		}

		// Get final history from channel
		if genAIHistory := <-historyChan; genAIHistory != nil {
			// Get existing history from session
			var existingHistory ai.ChatHistory
			s.GetInterface(constants.SessionKeyChatHistory, &existingHistory)

			// Append the new messages to existing history
			for _, msg := range genAIHistory {
				existingHistory.Messages = append(existingHistory.Messages, &ai.ChatMessage{
					Role:    msg.Role,
					Content: string(msg.Parts[0].(genai.Text)),
				})
			}

			// Update session with combined history
			s.Set(constants.SessionKeyChatHistory, existingHistory)
		}

		// Calculate new page number to show latest messages
		s.Set(constants.SessionKeyPaginationPage, 0)
		s.Set(constants.SessionKeyIsStreaming, false)

		// Show final message
		m.layout.paginationManager.NavigateTo(event, s, m.page, "Response completed.")
	}
}

// checkMessageLimits checks if the user has exceeded their daily message limit.
// Returns true if the message should be allowed, false if it should be blocked.
func (m *Menu) checkMessageLimits(s *session.Session, userSettings *types.UserSetting) (bool, string) {
	now := time.Now()
	if userSettings.ChatMessageUsage.FirstMessageTime.IsZero() ||
		now.Sub(userSettings.ChatMessageUsage.FirstMessageTime) > constants.ChatMessageResetLimit {
		// First message or past time limit - reset both time and count
		userSettings.ChatMessageUsage.FirstMessageTime = now
		userSettings.ChatMessageUsage.MessageCount = 1
	} else {
		// Within time limit - check count
		if userSettings.ChatMessageUsage.MessageCount >= constants.MaxChatMessagesPerDay {
			timeLeft := userSettings.ChatMessageUsage.FirstMessageTime.Add(constants.ChatMessageResetLimit).Sub(now)
			return false, fmt.Sprintf("You have reached the limit of %d messages per day. Please try again in %s.",
				constants.MaxChatMessagesPerDay,
				timeLeft.String())
		}
		userSettings.ChatMessageUsage.MessageCount++
	}

	// Save updated usage to database
	if err := m.layout.db.Settings().SaveUserSettings(context.Background(), userSettings); err != nil {
		m.layout.logger.Error("Failed to save chat message usage", zap.Error(err))
		return false, "Failed to update chat message usage. Please try again."
	}

	// Update session with new settings
	s.Set(constants.SessionKeyUserSettings, userSettings)

	return true, ""
}
