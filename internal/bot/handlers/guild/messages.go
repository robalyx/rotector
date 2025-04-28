package guild

import (
	"github.com/disgoorg/disgo/discord"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/interaction"
	"github.com/robalyx/rotector/internal/bot/core/session"
	view "github.com/robalyx/rotector/internal/bot/views/guild"
	"github.com/robalyx/rotector/internal/database/types"
	"go.uber.org/zap"
)

// MessagesMenu handles the display of a user's inappropriate messages in a guild.
type MessagesMenu struct {
	layout *Layout
	page   *interaction.Page
}

// NewMessagesMenu creates a new messages menu.
func NewMessagesMenu(layout *Layout) *MessagesMenu {
	m := &MessagesMenu{layout: layout}
	m.page = &interaction.Page{
		Name: constants.GuildMessagesPageName,
		Message: func(s *session.Session) *discord.MessageUpdateBuilder {
			return view.NewMessagesBuilder(s).Build()
		},
		ShowHandlerFunc:   m.Show,
		ButtonHandlerFunc: m.handleButton,
	}
	return m
}

// Show prepares and displays the message history interface.
func (m *MessagesMenu) Show(ctx *interaction.Context, s *session.Session) {
	// Get required IDs and cursor from session
	discordUserID := session.DiscordUserLookupID.Get(s)
	guildID := session.DiscordUserMessageGuildID.Get(s)
	cursor := session.DiscordUserMessageCursor.Get(s)

	// Fetch messages using cursor pagination
	messages, nextCursor, err := m.layout.db.Model().Message().GetUserMessagesByCursor(
		ctx.Context(),
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
		ctx.Error("Failed to retrieve message history. Please try again.")
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
func (m *MessagesMenu) handleButton(ctx *interaction.Context, s *session.Session, customID string) {
	switch customID {
	case constants.BackButtonCustomID:
		ctx.NavigateBack("")
	case string(session.ViewerFirstPage),
		string(session.ViewerPrevPage),
		string(session.ViewerNextPage),
		string(session.ViewerLastPage):
		m.handlePagination(ctx, s, session.ViewerAction(customID))
	}
}

// handlePagination processes page navigation for messages.
func (m *MessagesMenu) handlePagination(ctx *interaction.Context, s *session.Session, action session.ViewerAction) {
	switch action {
	case session.ViewerNextPage:
		if session.PaginationHasNextPage.Get(s) {
			cursor := session.DiscordUserMessageCursor.Get(s)
			nextCursor := session.DiscordUserMessageNextCursor.Get(s)
			prevCursors := session.DiscordUserMessagePrevCursors.Get(s)

			session.DiscordUserMessageCursor.Set(s, nextCursor)
			session.DiscordUserMessagePrevCursors.Set(s, append(prevCursors, cursor))
			ctx.Reload("")
		}
	case session.ViewerPrevPage:
		prevCursors := session.DiscordUserMessagePrevCursors.Get(s)

		if len(prevCursors) > 0 {
			lastIdx := len(prevCursors) - 1
			session.DiscordUserMessagePrevCursors.Set(s, prevCursors[:lastIdx])
			session.DiscordUserMessageCursor.Set(s, prevCursors[lastIdx])
			ctx.Reload("")
		}
	case session.ViewerFirstPage:
		session.DiscordUserMessageCursor.Set(s, nil)
		session.DiscordUserMessagePrevCursors.Set(s, make([]*types.MessageCursor, 0))
		ctx.Reload("")
	case session.ViewerLastPage:
		// Not currently supported
		return
	}
}
