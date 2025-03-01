package chat

import (
	"fmt"
	"strings"

	"github.com/disgoorg/disgo/discord"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/bot/utils"
	"github.com/robalyx/rotector/internal/common/client/ai"
	"github.com/robalyx/rotector/internal/common/storage/database/types/enum"
)

// Builder creates the visual layout for the chat interface.
type Builder struct {
	model       enum.ChatModel
	history     ai.ChatHistory
	page        int
	isStreaming bool
	context     string
}

// NewBuilder creates a new chat builder.
func NewBuilder(s *session.Session) *Builder {
	return &Builder{
		model:       session.UserChatModel.Get(s),
		history:     session.ChatHistory.Get(s),
		page:        session.PaginationPage.Get(s),
		isStreaming: session.PaginationIsStreaming.Get(s),
		context:     session.ChatContext.Get(s),
	}
}

// Build creates a Discord message showing the chat history and controls.
func (b *Builder) Build() *discord.MessageUpdateBuilder {
	// Calculate message pairs and total pages
	messageCount := len(b.history.Messages) / 2
	totalPages := max((messageCount-1)/constants.ChatMessagesPerPage, 0)

	// Create embeds
	embedBuilders := []*discord.EmbedBuilder{discord.NewEmbedBuilder().
		SetTitle("⚠️ AI Chat - Experimental Feature").
		SetDescription("This chat feature is experimental and may not work as expected. Chat histories are stored temporarily and will be cleared when your session expires.").
		SetColor(constants.DefaultEmbedColor)}

	// Calculate page boundaries (showing latest messages first)
	end := max(len(b.history.Messages)-(b.page*constants.ChatMessagesPerPage*2), 0)
	start := max(end-(constants.ChatMessagesPerPage*2), 0)
	if end > len(b.history.Messages) {
		end = len(b.history.Messages)
	}

	// Add message pairs to embed
	for i := start; i < end; i += 2 {
		// Get messages for this pair
		userMsg := b.history.Messages[i]
		aiMsg := b.history.Messages[i+1]

		// Create new embed for this message pair
		pairEmbed := discord.NewEmbedBuilder().
			SetColor(constants.DefaultEmbedColor)

		// Add user message (right-aligned) and AI response (left-aligned)
		b.addPaddedMessage(pairEmbed, fmt.Sprintf("User (%d)", (i/2+1)), userMsg.Content, true)
		b.addPaddedMessage(pairEmbed, fmt.Sprintf("%s (%d)", b.model.String(), (i/2+1)), aiMsg.Content, false)

		embedBuilders = append(embedBuilders, pairEmbed)
	}

	// Check if there's pending context in the session
	if b.context != "" {
		// Create new embed for the pending context message
		contextEmbed := discord.NewEmbedBuilder().
			SetColor(constants.DefaultEmbedColor)

		// Add a message showing that context is ready
		b.addPaddedMessage(contextEmbed, fmt.Sprintf("User (%d)", len(b.history.Messages)/2+1), "📋 [Context information ready]", true)

		embedBuilders = append(embedBuilders, contextEmbed)
	}

	// Add page number to footer of last embed
	embedBuilders[len(embedBuilders)-1].
		SetFooter(fmt.Sprintf("Page %d/%d", b.page+1, totalPages+1), "")

	// Build all embeds
	embeds := make([]discord.Embed, len(embedBuilders))
	for i, builder := range embedBuilders {
		embeds[i] = builder.Build()
	}

	// Build message
	builder := discord.NewMessageUpdateBuilder().
		SetEmbeds(embeds...)

	// Only add components if not streaming
	if !b.isStreaming {
		components := []discord.ContainerComponent{
			discord.NewActionRow(
				discord.NewStringSelectMenu(constants.ChatModelSelectID, "Select Model",
					discord.NewStringSelectMenuOption("Gemini 1.5 Flash 8B", enum.ChatModelGeminiFlash1_5_8B.String()).
						WithDescription("Smaller model for fast reasoning and conversations").
						WithDefault(b.model == enum.ChatModelGeminiFlash1_5_8B),
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

		// Add action buttons row with conditional clear context button
		actionButtons := []discord.InteractiveComponent{
			discord.NewPrimaryButton("Send Message", constants.ChatSendButtonID),
			discord.NewDangerButton("Clear Chat", constants.ChatClearHistoryButtonID),
		}
		if b.context != "" {
			actionButtons = append(actionButtons,
				discord.NewDangerButton("Clear Context", constants.ChatClearContextButtonID),
			)
		}
		components = append(components, discord.NewActionRow(actionButtons...))

		builder.AddContainerComponents(components...)
	}

	return builder
}

// addPaddedMessage adds a message to the embed with proper padding fields.
func (b *Builder) addPaddedMessage(embed *discord.EmbedBuilder, title string, content string, rightAlign bool) {
	// Replace context with indicator in displayed message
	displayContent := content
	if start := strings.Index(displayContent, "<context>"); start != -1 {
		if end := strings.Index(displayContent, "</context>"); end != -1 {
			contextPart := displayContent[start : end+10] // include </context>
			displayContent = strings.Replace(displayContent, contextPart, "📋 [Context information provided]\n", 1)
		}
	}

	if rightAlign {
		// User messages - no paragraph splitting
		embed.AddField("\u200b", "\u200b", true)
		embed.AddField("\u200b", "\u200b", true)
		embed.AddField(title, fmt.Sprintf("```%s```", utils.NormalizeString(displayContent)), true)
		return
	}

	// AI messages - split into paragraphs and limit to 3
	paragraphs := strings.Split(strings.TrimSpace(displayContent), "\n\n")
	if len(paragraphs) > 3 {
		paragraphs = paragraphs[:3]
		paragraphs[2] += " (...)"
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
