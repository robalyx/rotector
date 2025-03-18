package guild

import (
	"context"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	builder "github.com/robalyx/rotector/internal/bot/builder/guild"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/pagination"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/bot/interfaces"
	"github.com/robalyx/rotector/internal/common/storage/database/types"
	"go.uber.org/zap"
)

// MessagesMenu handles the display of a user's inappropriate messages in a guild.
type MessagesMenu struct {
	layout *Layout
	page   *pagination.Page
}

// NewMessagesMenu creates a new messages menu.
func NewMessagesMenu(layout *Layout) *MessagesMenu {
	m := &MessagesMenu{layout: layout}
	m.page = &pagination.Page{
		Name: constants.GuildMessagesPageName,
		Message: func(s *session.Session) *discord.MessageUpdateBuilder {
			return builder.NewMessagesBuilder(s).Build()
		},
		ShowHandlerFunc:   m.Show,
		ButtonHandlerFunc: m.handleButton,
	}
	return m
}

// Show prepares and displays the message history interface.
func (m *MessagesMenu) Show(event interfaces.CommonEvent, s *session.Session, r *pagination.Respond) {
	// Get required IDs and cursor from session
	discordUserID := session.DiscordUserLookupID.Get(s)
	guildID := session.DiscordUserMessageGuildID.Get(s)
	cursor := session.DiscordUserMessageCursor.Get(s)

	// Fetch messages using cursor pagination
	messages, nextCursor, err := m.layout.db.Model().Message().GetUserMessagesByCursor(
		context.Background(),
		guildID,
		discordUserID,
		cursor,
		constants.GuildMessagesPerPage,
	)
	if err != nil {
		m.layout.logger.Error("Failed to get user messages",
			zap.Error(err),
			zap.Uint64("guild_id", guildID),
			zap.Uint64("discord_user_id", discordUserID))
		r.Error(event, "Failed to retrieve message history. Please try again.")
		return
	}

	// Get previous cursors array
	prevCursors := session.DiscordUserMessagePrevCursors.Get(s)

	// Store results in session
	session.DiscordUserMessages.Set(s, messages)
	session.DiscordUserMessageCursor.Set(s, cursor)
	session.DiscordUserMessageNextCursor.Set(s, nextCursor)
	session.PaginationHasNextPage.Set(s, nextCursor != nil)
	session.PaginationHasPrevPage.Set(s, len(prevCursors) > 0)
}

// handleButton processes button interactions.
func (m *MessagesMenu) handleButton(
	event *events.ComponentInteractionCreate, s *session.Session, r *pagination.Respond, customID string,
) {
	switch customID {
	case constants.BackButtonCustomID:
		r.NavigateBack(event, s, "")
	case constants.RefreshButtonCustomID:
		// Reset cursors and reload
		session.DiscordUserMessageCursor.Delete(s)
		session.DiscordUserMessageNextCursor.Delete(s)
		session.DiscordUserMessagePrevCursors.Delete(s)
		session.PaginationHasNextPage.Delete(s)
		session.PaginationHasPrevPage.Delete(s)
		r.Reload(event, s, "")
	case string(session.ViewerFirstPage),
		string(session.ViewerPrevPage),
		string(session.ViewerNextPage),
		string(session.ViewerLastPage):
		m.handlePagination(event, s, r, session.ViewerAction(customID))
	}
}

// handlePagination processes page navigation for messages.
func (m *MessagesMenu) handlePagination(
	event *events.ComponentInteractionCreate, s *session.Session, r *pagination.Respond, action session.ViewerAction,
) {
	switch action {
	case session.ViewerNextPage:
		if session.PaginationHasNextPage.Get(s) {
			cursor := session.DiscordUserMessageCursor.Get(s)
			nextCursor := session.DiscordUserMessageNextCursor.Get(s)
			prevCursors := session.DiscordUserMessagePrevCursors.Get(s)

			session.DiscordUserMessageCursor.Set(s, nextCursor)
			session.DiscordUserMessagePrevCursors.Set(s, append(prevCursors, cursor))
			r.Reload(event, s, "")
		}
	case session.ViewerPrevPage:
		prevCursors := session.DiscordUserMessagePrevCursors.Get(s)

		if len(prevCursors) > 0 {
			lastIdx := len(prevCursors) - 1
			session.DiscordUserMessagePrevCursors.Set(s, prevCursors[:lastIdx])
			session.DiscordUserMessageCursor.Set(s, prevCursors[lastIdx])
			r.Reload(event, s, "")
		}
	case session.ViewerFirstPage:
		session.DiscordUserMessageCursor.Set(s, nil)
		session.DiscordUserMessagePrevCursors.Set(s, make([]*types.MessageCursor, 0))
		r.Reload(event, s, "")
	case session.ViewerLastPage:
		// Not currently supported
		return
	}
}
