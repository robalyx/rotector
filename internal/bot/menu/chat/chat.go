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
	"github.com/robalyx/rotector/internal/common/client/ai"
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
		Name: constants.ChatPageName,
		Message: func(s *session.Session) *discord.MessageUpdateBuilder {
			return builder.NewBuilder(s).Build()
		},
		ShowHandlerFunc:   m.Show,
		SelectHandlerFunc: m.handleSelectMenu,
		ButtonHandlerFunc: m.handleButton,
		ModalHandlerFunc:  m.handleModal,
	}
	return m
}

// Show prepares and displays the chat interface.
func (m *Menu) Show(_ interfaces.CommonEvent, _ *session.Session, _ *pagination.Respond) {
	// Nothing needs to be done here
}

// handleButton processes button interactions.
func (m *Menu) handleButton(event *events.ComponentInteractionCreate, s *session.Session, r *pagination.Respond, customID string) {
	action := session.ViewerAction(customID)
	switch action {
	case session.ViewerFirstPage, session.ViewerPrevPage, session.ViewerNextPage, session.ViewerLastPage:
		history := session.ChatHistory.Get(s)

		maxPage := (len(history.Messages)/2 - 1) / constants.ChatMessagesPerPage
		page := action.ParsePageAction(s, action, maxPage)

		session.PaginationPage.Set(s, page)
		r.Reload(event, s, "")

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
			r.Error(event, "Failed to open chat input. Please try again.")
		}

	case constants.BackButtonCustomID:
		r.NavigateBack(event, s, "")

	case constants.ChatClearHistoryButtonID:
		// Clear chat history
		session.ChatHistory.Set(s, ai.ChatHistory{Messages: make([]*ai.ChatMessage, 0)})
		session.PaginationPage.Set(s, 0)
		r.Reload(event, s, "Chat history cleared.")

	case constants.ChatClearContextButtonID:
		session.ChatContext.Delete(s)
		r.Reload(event, s, "Context cleared.")
	}
}

// handleSelectMenu processes select menu interactions.
func (m *Menu) handleSelectMenu(event *events.ComponentInteractionCreate, s *session.Session, r *pagination.Respond, customID, option string) {
	switch customID {
	case constants.ChatModelSelectID:
		// Parse option to chat model
		chatModel, err := enum.ChatModelString(option)
		if err != nil {
			m.layout.logger.Error("Failed to parse chat model", zap.Error(err))
			r.Error(event, "Failed to parse chat model. Please try again.")
			return
		}

		// Update user settings with new chat model
		session.UserChatModel.Set(s, chatModel)

		// Refresh the menu
		r.Reload(event, s, fmt.Sprintf("Switched to %s model", chatModel.String()))
	}
}

// handleModal processes modal submissions for chat input.
func (m *Menu) handleModal(event *events.ModalSubmitInteractionCreate, s *session.Session, r *pagination.Respond) {
	switch event.Data.CustomID {
	case constants.ChatInputModalID:
		message := event.Data.Text(constants.ChatInputCustomID)
		if message == "" {
			r.Cancel(event, s, "Message cannot be empty")
			return
		}

		// Check message limits
		if allowed, errMsg := m.checkMessageLimits(s); !allowed {
			r.Cancel(event, s, errMsg)
			return
		}

		// Prepend context if available
		msgContext := session.ChatContext.Get(s)
		if msgContext != "" {
			message = fmt.Sprintf("%s\n\n%s", msgContext, message)
			session.ChatContext.Delete(s)
		}

		// Set streaming state
		session.PaginationIsStreaming.Set(s, true)

		// Show "AI is typing..." message
		r.Reload(event, s, "AI is typing...")

		// Stream AI response
		history := session.ChatHistory.Get(s)
		responseChan, historyChan := m.layout.chatHandler.StreamResponse(
			context.Background(),
			history.ToGenAIHistory(),
			session.UserChatModel.Get(s),
			message,
		)

		// Stream AI response
		var lastUpdate time.Time
		var aiResponse string
		for response := range responseChan {
			aiResponse += response

			// Update message at most once per second to avoid rate limits
			if time.Since(lastUpdate) > 1*time.Second {
				r.Reload(event, s, "Receiving response...")
				lastUpdate = time.Now()
			}
		}

		// Get final history from channel
		if genAIHistory := <-historyChan; genAIHistory != nil {
			// Get existing history from session
			existingHistory := session.ChatHistory.Get(s)

			// Append the new messages to existing history
			for _, msg := range genAIHistory {
				existingHistory.Messages = append(existingHistory.Messages, &ai.ChatMessage{
					Role:    msg.Role,
					Content: string(msg.Parts[0].(genai.Text)),
				})
			}

			// Update session with combined history
			session.ChatHistory.Set(s, existingHistory)
		}

		// Calculate new page number to show latest messages
		session.PaginationPage.Set(s, 0)
		session.PaginationIsStreaming.Set(s, false)

		// Show final message
		r.Reload(event, s, "Response completed.")
	}
}

// checkMessageLimits checks if the user has exceeded their daily message limit.
// Returns true if the message should be allowed, false if it should be blocked.
func (m *Menu) checkMessageLimits(s *session.Session) (bool, string) {
	now := time.Now()
	firstMessageTime := session.UserChatMessageUsageFirstMessageTime.Get(s)
	messageCount := session.UserChatMessageUsageMessageCount.Get(s)

	if firstMessageTime.IsZero() || now.Sub(firstMessageTime) > constants.ChatMessageResetLimit {
		// First message or past time limit - reset both time and count
		session.UserChatMessageUsageFirstMessageTime.Set(s, now)
		session.UserChatMessageUsageMessageCount.Set(s, 1)
	} else {
		// Within time limit - check and increment message count
		if messageCount >= constants.MaxChatMessagesPerDay {
			timeLeft := firstMessageTime.Add(constants.ChatMessageResetLimit).Sub(now)
			return false, fmt.Sprintf("You have reached the limit of %d messages per day. Please try again in %s.",
				constants.MaxChatMessagesPerDay,
				timeLeft.String())
		}

		session.UserChatMessageUsageMessageCount.Set(s, messageCount+1)
	}

	return true, ""
}
