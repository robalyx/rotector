package chat

import (
	"fmt"
	"strings"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/robalyx/rotector/internal/ai"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/bot/utils"
	"github.com/robalyx/rotector/internal/database/types/enum"
)

// Builder creates the visual layout for the chat interface.
type Builder struct {
	model            enum.ChatModel
	chatContext      ai.ChatContext
	groupedContext   ai.ContextMap
	firstMessage     time.Time
	streamingMessage string
	messageCount     int
	page             int
	isStreaming      bool
}

// NewBuilder creates a new chat builder.
func NewBuilder(s *session.Session) *Builder {
	chatContext := session.ChatContext.Get(s)
	return &Builder{
		model:            session.UserChatModel.Get(s),
		chatContext:      chatContext,
		groupedContext:   chatContext.GroupByType(),
		firstMessage:     session.UserChatMessageUsageFirstMessageTime.Get(s),
		streamingMessage: session.ChatStreamingMessage.Get(s),
		messageCount:     session.UserChatMessageUsageMessageCount.Get(s),
		page:             session.PaginationPage.Get(s),
		isStreaming:      session.PaginationIsStreaming.Get(s),
	}
}

// Build creates a Discord message showing the chat history and controls.
func (b *Builder) Build() *discord.MessageUpdateBuilder {
	builder := discord.NewMessageUpdateBuilder()

	// Add containers
	builder.AddComponents(
		// Header container
		b.buildHeaderContainer(),
		// Chat container
		b.buildChatContainer(),
	)

	// Only add back button when not streaming
	if !b.isStreaming {
		builder.AddComponents(
			discord.NewActionRow(
				discord.NewSecondaryButton("◀️ Back", constants.BackButtonCustomID),
			),
		)
	}

	return builder
}

// buildHeaderContainer creates the header section with message usage info.
func (b *Builder) buildHeaderContainer() discord.ContainerComponent {
	var headerContent strings.Builder
	headerContent.WriteString("## ⚠️ AI Chat - Experimental Feature\n")
	headerContent.WriteString("This chat feature is experimental and may not work as expected. " +
		"Chat histories are stored temporarily and will be cleared when your session expires.\n\n")

	// Add message count
	resetTime := b.firstMessage.Add(constants.ChatMessageResetLimit)
	if b.messageCount > 0 && time.Now().Before(resetTime) {
		headerContent.WriteString(fmt.Sprintf("💬 **Messages:** %d/%d remaining",
			constants.MaxChatMessagesPerDay-b.messageCount, constants.MaxChatMessagesPerDay))
		headerContent.WriteString(fmt.Sprintf("\n⏰ **Credits reset:** <t:%d:R>", resetTime.Unix()))
	} else {
		headerContent.WriteString(fmt.Sprintf("💬 **Messages:** %d/%d remaining",
			constants.MaxChatMessagesPerDay, constants.MaxChatMessagesPerDay))
	}

	return discord.NewContainer(
		discord.NewTextDisplay(headerContent.String()),
	).WithAccentColor(constants.DefaultContainerColor)
}

// buildChatContainer creates the chat container with messages and controls.
func (b *Builder) buildChatContainer() discord.ContainerComponent {
	var chatComponents []discord.ContainerSubComponent

	// Add chat messages or streaming preview
	chatMessages := b.getChatMessages()

	if b.isStreaming {
		chatComponents = append(chatComponents, b.buildStreamingPreview()...)
	} else {
		chatComponents = append(chatComponents, b.buildChatHistory(chatMessages)...)

		// Add separator before model selection
		chatComponents = append(chatComponents, discord.NewLargeSeparator())

		// Add model selection dropdown
		chatComponents = append(chatComponents,
			discord.NewActionRow(
				discord.NewStringSelectMenu(constants.ChatModelSelectID, "Select Model", b.buildModelOptions()...),
			))
	}

	return discord.NewContainer(chatComponents...).WithAccentColor(constants.DefaultContainerColor)
}

// buildStreamingPreview adds streaming preview components to the chat container.
func (b *Builder) buildStreamingPreview() []discord.ContainerSubComponent {
	var components []discord.ContainerSubComponent
	var previewContent strings.Builder

	// Add active context if any
	if contextInfo := b.getActiveContextInfo(); contextInfo != "" {
		previewContent.WriteString("### Context\n")
		previewContent.WriteString(utils.FormatString(contextInfo))
		previewContent.WriteString("\n\n")
	}

	// Find the last human message from context
	var lastHumanMessage string
	for i := len(b.chatContext) - 1; i >= 0; i-- {
		if b.chatContext[i].Type == ai.ContextTypeHuman {
			lastHumanMessage = b.chatContext[i].Content
			break
		}
	}

	// Add user message and AI response
	if lastHumanMessage != "" {
		previewContent.WriteString(fmt.Sprintf("### User\n%s\n",
			utils.FormatString(utils.NormalizeString(lastHumanMessage))))

		// Add AI response
		previewContent.WriteString(fmt.Sprintf("### %s\n%s",
			b.model.String(),
			b.formatAIMessage(b.streamingMessage)))

		components = append(components,
			discord.NewTextDisplay(previewContent.String()))
	}

	return components
}

// buildChatHistory adds chat history components to the chat container.
func (b *Builder) buildChatHistory(chatMessages []ai.Context) []discord.ContainerSubComponent {
	var components []discord.ContainerSubComponent

	// Calculate page boundaries
	totalPairs := len(chatMessages) / 2
	startPair := max(totalPairs-(b.page+1)*constants.ChatMessagesPerPage, 0)
	endPair := min(totalPairs-b.page*constants.ChatMessagesPerPage, totalPairs)

	// Convert pair indices to message indices
	start := startPair * 2
	end := endPair * 2

	// Add message pairs
	if len(chatMessages) >= 2 && start < len(chatMessages) {
		for i := start; i < end && i+1 < len(chatMessages); i += 2 {
			var content strings.Builder

			// Add message pair
			content.WriteString(b.buildMessagePair(chatMessages, i))

			// Add active context after the last message pair on first page
			if b.page == 0 && i == end-2 {
				if contextInfo := b.getActiveContextInfo(); contextInfo != "" {
					content.WriteString("\n### Context\n" + utils.FormatString(contextInfo))
				}
			}

			components = append(components,
				discord.NewTextDisplay(content.String()))
		}
	} else if b.page == 0 {
		// If no messages but have active context, show it
		if contextInfo := b.getActiveContextInfo(); contextInfo != "" {
			components = append(components,
				discord.NewTextDisplay("### Context\n"+utils.FormatString(contextInfo)))
		}
	}

	// Add separator before action buttons
	components = append(components, discord.NewLargeSeparator())

	// Add action buttons
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

	// Add pagination buttons
	totalPages := max((totalPairs-1)/constants.ChatMessagesPerPage, 0)
	components = append(components,
		discord.NewActionRow(
			discord.NewSecondaryButton("⏮️", string(session.ViewerFirstPage)).WithDisabled(b.page == 0),
			discord.NewSecondaryButton("◀️", string(session.ViewerPrevPage)).WithDisabled(b.page == 0),
			discord.NewSecondaryButton("▶️", string(session.ViewerNextPage)).WithDisabled(b.page == totalPages),
			discord.NewSecondaryButton("⏭️", string(session.ViewerLastPage)).WithDisabled(b.page == totalPages),
		))

	return components
}

// buildMessagePair adds a single message pair to the chat components.
func (b *Builder) buildMessagePair(chatMessages []ai.Context, i int) string {
	var messageContent strings.Builder

	// Add message number
	messageNumber := (i / 2) + 1
	messageContent.WriteString(fmt.Sprintf("-# Message %d\n", messageNumber))

	// Add context if available
	if contextInfo := b.getContextInfo(i); contextInfo != "" {
		messageContent.WriteString(fmt.Sprintf("### Context\n%s\n",
			utils.FormatString(contextInfo)))
	}

	// Add user message
	messageContent.WriteString(fmt.Sprintf("### User\n%s\n",
		utils.FormatString(utils.NormalizeString(chatMessages[i].Content))))

	// Add AI message
	modelName := chatMessages[i+1].Model
	if modelName == "" {
		modelName = "Unknown Model"
	}
	messageContent.WriteString(fmt.Sprintf("### %s\n%s",
		modelName, b.formatAIMessage(chatMessages[i+1].Content)))

	return messageContent.String()
}

// buildModelOptions creates the model selection options.
func (b *Builder) buildModelOptions() []discord.StringSelectMenuOption {
	return []discord.StringSelectMenuOption{
		discord.NewStringSelectMenuOption("o4 Mini High", enum.ChatModelo4MiniHigh.String()).
			WithDescription("Most capable model - Best overall performance with more thinking tokens").
			WithDefault(b.model == enum.ChatModelo4MiniHigh),
		discord.NewStringSelectMenuOption("o4 Mini", enum.ChatModelo4Mini.String()).
			WithDescription("Most capable model - Best overall performance").
			WithDefault(b.model == enum.ChatModelo4Mini),
		discord.NewStringSelectMenuOption("Qwen 3 235B A22B", enum.ChatModelQwen3_235bA22b.String()).
			WithDescription("High performance model - Excellent reasoning and language abilities").
			WithDefault(b.model == enum.ChatModelQwen3_235bA22b),
		discord.NewStringSelectMenuOption("Gemini 2.5 Flash", enum.ChatModelGemini2_5Flash.String()).
			WithDescription("High performance model - Good balance of speed and capabilities").
			WithDefault(b.model == enum.ChatModelGemini2_5Flash),
		discord.NewStringSelectMenuOption("DeepSeek V3", enum.ChatModelDeepseekV3_0324.String()).
			WithDescription("Mid-tier model - Good language understanding").
			WithDefault(b.model == enum.ChatModelDeepseekV3_0324),
		discord.NewStringSelectMenuOption("GPT-4.1 Mini", enum.ChatModelGPT4_1Mini.String()).
			WithDescription("Mid-tier model - Basic capabilities").
			WithDefault(b.model == enum.ChatModelGPT4_1Mini),
	}
}

// getContextInfo returns context information for a specific message pair.
func (b *Builder) getContextInfo(messageIndex int) string {
	// Find the user message's position in the full context
	userMsg := b.getChatMessages()[messageIndex]
	userMsgIndex := -1
	for i, ctx := range b.chatContext {
		if ctx == userMsg {
			userMsgIndex = i
			break
		}
	}

	// Check for context immediately before this user message
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
			return sb.String()
		}
	}

	return ""
}

// getActiveContextInfo returns information about active context items.
func (b *Builder) getActiveContextInfo() string {
	if len(b.chatContext) == 0 {
		return ""
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

	// If no relevant context items were found after the last message, return empty string
	if len(counts) == 0 {
		return ""
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

	return sb.String()
}

// formatAIMessage formats the AI message content with proper markdown.
func (b *Builder) formatAIMessage(content string) string {
	content = strings.TrimSpace(content)
	content = strings.ReplaceAll(content, "<message>", "")
	content = strings.ReplaceAll(content, "</message>", "")

	// If there's a closing tag without an opening tag, treat everything before it as thinking
	if idx := strings.Index(content, "</think>"); idx >= 0 && !strings.Contains(content[:idx], "<think>") {
		content = "<think>" + content[:idx] + "</think>" + content[idx+8:]
	}

	var finalContent strings.Builder
	var lastThinkingBlock bool
	const limit = 1000

	// Helper function to process and format content blocks
	processContentBlock := func(text string) string {
		text = strings.TrimSpace(text)
		if text == "" {
			return ""
		}
		text = strings.ReplaceAll(text, "`", "")
		if len(text) > limit {
			lastNewline := strings.LastIndex(text[:limit], "\n")
			if lastNewline != -1 {
				text = text[:lastNewline] + "\n..."
			} else {
				text = text[:limit] + " ..."
			}
		}
		return utils.FormatString(text)
	}

	for {
		start := strings.Index(content, "<think>")
		if start == -1 {
			// Process remaining content
			if formatted := processContentBlock(content); formatted != "" {
				if lastThinkingBlock {
					finalContent.WriteString("\n")
				}
				finalContent.WriteString(formatted)
			}
			break
		}

		// Add content before thinking block if any
		if preContent := processContentBlock(content[:start]); preContent != "" {
			if lastThinkingBlock {
				finalContent.WriteString("\n")
			}
			finalContent.WriteString(preContent)
		}

		// Find end of thinking block
		end := strings.Index(content[start:], "</think>")
		if end == -1 {
			break
		}
		end += start

		// Add thinking indicator
		if finalContent.Len() > 0 {
			finalContent.WriteString("\n")
		}
		finalContent.WriteString("```💭 *thinking...*```")
		lastThinkingBlock = true

		// Continue with remaining content
		content = content[end+8:]
	}

	return finalContent.String()
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
