package chat

import (
	"fmt"
	"strings"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/bot/utils"
	"github.com/robalyx/rotector/internal/common/client/ai"
	"github.com/robalyx/rotector/internal/common/storage/database/types/enum"
)

// Builder creates the visual layout for the chat interface.
type Builder struct {
	model          enum.ChatModel
	chatContext    ai.ChatContext
	groupedContext ai.ContextMap
	firstMessage   time.Time
	messageCount   int
	page           int
	isStreaming    bool
}

// NewBuilder creates a new chat builder.
func NewBuilder(s *session.Session) *Builder {
	chatContext := session.AIChatContext.Get(s)
	return &Builder{
		model:          session.UserChatModel.Get(s),
		chatContext:    chatContext,
		groupedContext: chatContext.GroupByType(),
		firstMessage:   session.UserChatMessageUsageFirstMessageTime.Get(s),
		messageCount:   session.UserChatMessageUsageMessageCount.Get(s),
		page:           session.PaginationPage.Get(s),
		isStreaming:    session.PaginationIsStreaming.Get(s),
	}
}

// Build creates a Discord message showing the chat history and controls.
func (b *Builder) Build() *discord.MessageUpdateBuilder {
	return discord.NewMessageUpdateBuilder().
		SetEmbeds(b.buildEmbeds()...).
		AddContainerComponents(b.buildComponents()...)
}

// buildEmbeds creates all the embeds for the chat interface.
func (b *Builder) buildEmbeds() []discord.Embed {
	messageCount := len(b.getChatMessages()) / 2
	totalPages := max((messageCount-1)/constants.ChatMessagesPerPage, 0)

	// Add header embed
	headerEmbed := b.buildHeaderEmbed(totalPages)
	embeds := []discord.Embed{headerEmbed}

	// Add chat message embeds
	chatEmbeds := b.buildChatEmbeds()
	embeds = append(embeds, chatEmbeds...)

	// Add context embed on first page
	if b.page == 0 {
		if contextEmbed := b.buildContextEmbed(); contextEmbed != nil {
			embeds = append(embeds, *contextEmbed)
		}
	}

	return embeds
}

// buildHeaderEmbed creates the header embed with usage information.
func (b *Builder) buildHeaderEmbed(totalPages int) discord.Embed {
	messageCreditsInfo := fmt.Sprintf("This chat feature is experimental and may not work as expected. "+
		"Chat histories are stored temporarily and will be cleared when your session expires.\n\n"+
		"💬 **Messages:** %d/%d remaining", constants.MaxChatMessagesPerDay-b.messageCount, constants.MaxChatMessagesPerDay)

	// Add reset time if messages have been used
	if b.messageCount > 0 {
		resetTime := b.firstMessage.Add(constants.ChatMessageResetLimit)
		if time.Now().Before(resetTime) {
			messageCreditsInfo += fmt.Sprintf("\n⏰ **Credits reset:** <t:%d:R>", resetTime.Unix())
		}
	}

	builder := discord.NewEmbedBuilder().
		SetTitle("⚠️ AI Chat - Experimental Feature").
		SetDescription(messageCreditsInfo).
		SetColor(constants.DefaultEmbedColor)

	// Add page information if there are multiple pages
	if totalPages > 0 {
		builder.SetFooter(fmt.Sprintf("Page %d/%d", b.page+1, totalPages+1), "")
	}

	return builder.Build()
}

// buildChatEmbeds creates embeds for chat messages.
func (b *Builder) buildChatEmbeds() []discord.Embed {
	var embeds []discord.Embed
	chatMessages := b.getChatMessages()

	// Calculate page boundaries
	totalPairs := len(chatMessages) / 2
	startPair := max(totalPairs-(b.page+1)*constants.ChatMessagesPerPage, 0)
	endPair := min(totalPairs-b.page*constants.ChatMessagesPerPage, totalPairs)

	// Convert pair indices to message indices
	start := startPair * 2
	end := endPair * 2

	// Add message pairs to embeds
	if len(chatMessages) >= 2 && start < len(chatMessages) {
		for i := start; i < end && i+1 < len(chatMessages); i += 2 {
			embed := b.buildMessagePairEmbed(chatMessages[i], chatMessages[i+1], i)
			embeds = append(embeds, embed)
		}
	}

	return embeds
}

// buildContextEmbed creates an embed showing active context if any exists.
func (b *Builder) buildContextEmbed() *discord.Embed {
	if len(b.chatContext) == 0 {
		return nil
	}

	// Find the index of the last AI message by iterating backward
	lastMessageIndex := -1
	for i := len(b.chatContext) - 1; i >= 0; i-- {
		if b.chatContext[i].Type == ai.ContextTypeAI {
			lastMessageIndex = i
			break
		}
	}

	// Count contexts that were added after the last message pair
	counts := make(map[ai.ContextType]int)
	startIndex := lastMessageIndex + 1

	// Only proceed if there are items after the last AI message (or if no AI message exists)
	if startIndex < len(b.chatContext) {
		for _, ctx := range b.chatContext[startIndex:] {
			// Only count User and Group context types
			if ctx.Type == ai.ContextTypeUser || ctx.Type == ai.ContextTypeGroup {
				counts[ctx.Type]++
			}
		}
	}

	// If no relevant context items were found after the last message, return nil
	if len(counts) == 0 {
		return nil
	}

	// Build context indicator string
	var sb strings.Builder
	if count := counts[ai.ContextTypeUser]; count > 0 {
		sb.WriteString(fmt.Sprintf("👤 User context (%d items)", count))
	}
	if count := counts[ai.ContextTypeGroup]; count > 0 {
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(fmt.Sprintf("👥 Group context (%d items)", count))
	}

	// Build the context embed
	contextEmbed := discord.NewEmbedBuilder().
		SetColor(constants.DefaultEmbedColor)

	b.addPaddedMessage(contextEmbed, "Context", sb.String(), true)

	embed := contextEmbed.Build()
	return &embed
}

// buildMessagePairEmbed creates an embed for a user-AI message pair.
func (b *Builder) buildMessagePairEmbed(userMsg, aiMsg ai.Context, messageIndex int) discord.Embed {
	embed := discord.NewEmbedBuilder().
		SetColor(constants.DefaultEmbedColor)

	// Find the user message's position in the full context
	userMsgIndex := -1
	for i, ctx := range b.chatContext {
		if ctx == userMsg {
			userMsgIndex = i
			break
		}
	}

	// Check for context immediately before this user message
	var contextInfo string
	if userMsgIndex > 0 {
		counts := make(map[ai.ContextType]int)

		// Count consecutive context items before this user message
		for i := userMsgIndex - 1; i >= 0; i-- {
			ctxType := b.chatContext[i].Type
			if ctxType == ai.ContextTypeUser || ctxType == ai.ContextTypeGroup {
				counts[ctxType]++
			} else {
				// Stop when we hit a non-context item (like an AI or Human message)
				break
			}
		}

		// Build context indicator string if context items were found
		if len(counts) > 0 {
			var sb strings.Builder
			if count := counts[ai.ContextTypeUser]; count > 0 {
				sb.WriteString(fmt.Sprintf("👤 User context (%d items)", count))
			}
			if count := counts[ai.ContextTypeGroup]; count > 0 {
				if sb.Len() > 0 {
					sb.WriteString("\n")
				}
				sb.WriteString(fmt.Sprintf("👥 Group context (%d items)", count))
			}
			contextInfo = sb.String()
		}
	}

	// Add context and user message (right-aligned)
	if contextInfo != "" {
		// Add context
		embed.AddField("\u200b", "\u200b", true) // Padding
		embed.AddField("\u200b", "\u200b", true) // Padding
		embed.AddField("Context", fmt.Sprintf("```%s```", contextInfo), true)
	}

	// Add user message
	embed.AddField("\u200b", "\u200b", true) // Padding
	embed.AddField("\u200b", "\u200b", true) // Padding
	embed.AddField("User", fmt.Sprintf("```%s```", utils.NormalizeString(userMsg.Content)), true)

	// Add AI message (left-aligned)
	modelName := aiMsg.Model
	if modelName == "" {
		modelName = "Unknown Model"
	}
	b.addPaddedMessage(embed, modelName, aiMsg.Content, false)

	// Add message number to footer (relative to the start of the chat)
	messageNumber := (messageIndex / 2) + 1
	embed.SetFooter(fmt.Sprintf("Message %d", messageNumber), "")

	return embed.Build()
}

// getChatMessages returns only the human and AI messages from the context, ensuring alternation.
func (b *Builder) getChatMessages() []ai.Context {
	messages := make([]ai.Context, 0, len(b.chatContext))
	var lastType ai.ContextType

	// Collect messages in order, ensuring proper human-AI alternation
	for _, ctx := range b.chatContext {
		// Skip non-message types
		if ctx.Type != ai.ContextTypeHuman && ctx.Type != ai.ContextTypeAI {
			continue
		}

		// Skip if the current message type is the same as the last one added
		if len(messages) > 0 && lastType == ctx.Type {
			continue
		}

		// Ensure AI messages are always preceded by a human message in the filtered list
		if ctx.Type == ai.ContextTypeAI && lastType != ai.ContextTypeHuman {
			continue
		}

		messages = append(messages, ctx)
		lastType = ctx.Type
	}

	// Ensure the list doesn't end with a Human message without a following AI message
	if len(messages) > 0 && messages[len(messages)-1].Type == ai.ContextTypeHuman {
		messages = messages[:len(messages)-1]
	}

	return messages
}

// buildComponents creates all the interactive components.
func (b *Builder) buildComponents() []discord.ContainerComponent {
	messageCount := (len(b.getChatMessages())) / 2
	totalPages := max((messageCount-1)/constants.ChatMessagesPerPage, 0)

	components := []discord.ContainerComponent{
		discord.NewActionRow(
			discord.NewStringSelectMenu(constants.ChatModelSelectID, "Select Model",
				discord.NewStringSelectMenuOption("Gemini 1.5 Flash 8B", enum.ChatModelGeminiFlash1_5_8B.String()).
					WithDescription("Basic model - Limited capabilities but fastest responses").
					WithDefault(b.model == enum.ChatModelGeminiFlash1_5_8B),
				discord.NewStringSelectMenuOption("Gemini 1.5 Flash", enum.ChatModelGemini1_5Flash.String()).
					WithDescription("Basic model - Limited capabilities and slower responses").
					WithDefault(b.model == enum.ChatModelGemini1_5Flash),
				discord.NewStringSelectMenuOption("Gemini 2.0 Flash Lite", enum.ChatModelGemini2_0FlashLite.String()).
					WithDescription("Advanced model - Same abilities as 2.0 but with smaller knowledge base").
					WithDefault(b.model == enum.ChatModelGemini2_0FlashLite),
				discord.NewStringSelectMenuOption("Gemini 2.0 Flash", enum.ChatModelGemini2_0Flash.String()).
					WithDescription("Advanced model - Full capabilities with larger knowledge base").
					WithDefault(b.model == enum.ChatModelGemini2_0Flash),
			),
		),
		discord.NewActionRow(
			discord.NewSecondaryButton("◀️", constants.BackButtonCustomID),
			discord.NewSecondaryButton("⏮️", string(session.ViewerFirstPage)).WithDisabled(b.page == 0),
			discord.NewSecondaryButton("◀️", string(session.ViewerPrevPage)).WithDisabled(b.page == 0),
			discord.NewSecondaryButton("▶️", string(session.ViewerNextPage)).WithDisabled(b.page == totalPages),
			discord.NewSecondaryButton("⏭️", string(session.ViewerLastPage)).WithDisabled(b.page == totalPages),
		),
	}

	// Add action buttons row
	actionButtons := []discord.InteractiveComponent{
		discord.NewPrimaryButton("Send Message", constants.ChatSendButtonID),
		discord.NewDangerButton("Clear Chat", constants.ChatClearHistoryButtonID),
	}

	// Only show clear context button if the last item is a context
	if len(b.chatContext) > 0 {
		lastItem := b.chatContext[len(b.chatContext)-1]
		if lastItem.Type == ai.ContextTypeUser || lastItem.Type == ai.ContextTypeGroup {
			actionButtons = append(actionButtons,
				discord.NewDangerButton("Clear Context", constants.ChatClearContextButtonID))
		}
	}

	components = append(components, discord.NewActionRow(actionButtons...))
	return components
}

// addPaddedMessage adds a message to the embed with proper padding fields.
func (b *Builder) addPaddedMessage(embed *discord.EmbedBuilder, title string, content string, rightAlign bool) {
	if rightAlign {
		// User messages - no paragraph splitting
		embed.AddField("\u200b", "\u200b", true)
		embed.AddField("\u200b", "\u200b", true)
		embed.AddField(title, fmt.Sprintf("```%s```", utils.NormalizeString(content)), true)
		return
	}

	// AI messages - split into paragraphs and limit to 5
	paragraphs := strings.Split(strings.TrimSpace(content), "\n\n")
	if len(paragraphs) > 5 {
		paragraphs = paragraphs[:5]
		paragraphs[4] += " (...)"
	}

	for i, p := range paragraphs {
		p = utils.NormalizeString(p)
		if p == "" {
			continue
		}

		// Format title for multi-paragraph messages
		messageTitle := title
		if i > 0 {
			messageTitle = "↳" // continuation marker
		}

		// Add message then padding for left alignment
		embed.AddField(messageTitle, fmt.Sprintf("```%s```", p), true)
		embed.AddField("\u200b", "\u200b", true)
		embed.AddField("\u200b", "\u200b", true)
	}
}
