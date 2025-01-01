package appeal

import (
	"fmt"
	"strconv"

	"github.com/disgoorg/disgo/discord"
	"github.com/rotector/rotector/internal/bot/constants"
	"github.com/rotector/rotector/internal/bot/core/session"
	"github.com/rotector/rotector/internal/bot/utils"
	"github.com/rotector/rotector/internal/common/storage/database/types"
)

// TicketBuilder creates the visual layout for an individual appeal ticket.
type TicketBuilder struct {
	appeal      *types.Appeal
	messages    []*types.AppealMessage
	settings    *types.UserSetting
	botSettings *types.BotSetting
	page        int
	totalPages  int
	isReviewer  bool
	userID      uint64
}

// NewTicketBuilder creates a new ticket builder.
func NewTicketBuilder(s *session.Session) *TicketBuilder {
	var appeal *types.Appeal
	s.GetInterface(constants.SessionKeyAppeal, &appeal)
	var messages []*types.AppealMessage
	s.GetInterface(constants.SessionKeyAppealMessages, &messages)
	var settings *types.UserSetting
	s.GetInterface(constants.SessionKeyUserSettings, &settings)
	var botSettings *types.BotSetting
	s.GetInterface(constants.SessionKeyBotSettings, &botSettings)

	return &TicketBuilder{
		appeal:      appeal,
		messages:    messages,
		settings:    settings,
		botSettings: botSettings,
		page:        s.GetInt(constants.SessionKeyPaginationPage),
		totalPages:  s.GetInt(constants.SessionKeyTotalPages),
		isReviewer:  botSettings.IsReviewer(s.UserID()),
		userID:      s.UserID(),
	}
}

// Build creates a Discord message showing the appeal ticket conversation.
func (b *TicketBuilder) Build() *discord.MessageUpdateBuilder {
	// Create header embed with appeal info
	headerEmbed := b.buildHeaderEmbed()

	// Create conversation embed
	conversationEmbed := b.buildConversationEmbed()

	// Build message with components
	builder := discord.NewMessageUpdateBuilder().
		SetEmbeds(headerEmbed.Build(), conversationEmbed.Build())

	// Add navigation buttons
	components := []discord.ContainerComponent{
		discord.NewActionRow(
			discord.NewSecondaryButton("◀️", constants.BackButtonCustomID),
			discord.NewSecondaryButton("⏮️", string(utils.ViewerFirstPage)).WithDisabled(b.page == 0),
			discord.NewSecondaryButton("◀️", string(utils.ViewerPrevPage)).WithDisabled(b.page == 0),
			discord.NewSecondaryButton("▶️", string(utils.ViewerNextPage)).WithDisabled(b.page == b.totalPages),
			discord.NewSecondaryButton("⏭️", string(utils.ViewerLastPage)).WithDisabled(b.page == b.totalPages),
		),
	}

	// Add components if appeal is pending
	if b.appeal.Status == types.AppealStatusPending {
		// Create action buttons
		actionButtons := []discord.InteractiveComponent{
			discord.NewPrimaryButton("Respond", constants.AppealRespondButtonCustomID),
		}

		// Add reviewer buttons if user is a reviewer
		if b.isReviewer {
			actionButtons = append(actionButtons,
				discord.NewPrimaryButton("Review User", constants.AppealReviewUserButtonCustomID),
				discord.NewSuccessButton("Accept", constants.AcceptAppealButtonCustomID),
				discord.NewDangerButton("Reject", constants.RejectAppealButtonCustomID),
			)
		} else {
			// Add close button for regular users
			actionButtons = append(actionButtons,
				discord.NewDangerButton("Close Ticket", constants.AppealCloseButtonCustomID),
			)
		}

		components = append(components, discord.NewActionRow(actionButtons...))
	}

	builder.AddContainerComponents(components...)
	return builder
}

// buildHeaderEmbed creates the embed showing appeal information.
func (b *TicketBuilder) buildHeaderEmbed() *discord.EmbedBuilder {
	// Format status with emoji
	var statusEmoji string
	switch b.appeal.Status {
	case types.AppealStatusPending:
		statusEmoji = "⏳"
	case types.AppealStatusAccepted:
		statusEmoji = "✅"
	case types.AppealStatusRejected:
		statusEmoji = "❌"
	}

	// Create embed
	userIDStr := strconv.FormatUint(b.appeal.UserID, 10)

	embed := discord.NewEmbedBuilder().
		SetTitle(fmt.Sprintf("%s Appeal `#%d`", statusEmoji, b.appeal.ID)).
		SetColor(utils.GetMessageEmbedColor(b.settings.StreamerMode)).
		AddField("User", fmt.Sprintf("[%s](https://www.roblox.com/users/%d/profile)",
			utils.CensorString(userIDStr, b.settings.StreamerMode), b.appeal.UserID), true).
		AddField("Requester", fmt.Sprintf("<@%d>", b.appeal.RequesterID), true).
		AddField("Status", string(b.appeal.Status), true).
		AddField("Submitted", fmt.Sprintf("<t:%d:R>", b.appeal.Timestamp.Unix()), true).
		AddField("Last Viewed", fmt.Sprintf("<t:%d:R>", b.appeal.LastViewed.Unix()), true).
		AddField("Last Activity", fmt.Sprintf("<t:%d:R>", b.appeal.LastActivity.Unix()), true)

	if b.appeal.ClaimedBy != 0 {
		embed.AddField("Claimed By", fmt.Sprintf("<@%d>", b.appeal.ClaimedBy), true)
	}

	if b.appeal.ReviewerID != 0 {
		embed.AddField("Reviewed By", fmt.Sprintf("<@%d>", b.appeal.ReviewerID), true)
		// Censor any sensitive information in the review reason
		censoredReason := utils.CensorStringsInText(
			b.appeal.ReviewReason,
			b.settings.StreamerMode,
			userIDStr,
		)
		embed.AddField("Review Reason", censoredReason, false)
	}

	return embed
}

// buildConversationEmbed creates the embed showing the message history.
func (b *TicketBuilder) buildConversationEmbed() *discord.EmbedBuilder {
	embed := discord.NewEmbedBuilder().
		SetColor(utils.GetMessageEmbedColor(b.settings.StreamerMode))

	// Calculate page boundaries
	start := b.page * constants.AppealMessagesPerPage
	end := start + constants.AppealMessagesPerPage
	if end > len(b.messages) {
		end = len(b.messages)
	}

	// Add messages
	if len(b.messages) == 0 {
		embed.SetDescription("No messages yet.")
	} else {
		for _, msg := range b.messages[start:end] {
			// Format role
			var roleName string
			switch msg.Role {
			case types.MessageRoleModerator:
				roleName = "Moderator"
			case types.MessageRoleUser:
				roleName = "User"
			}

			// Format field title with role and time
			fieldName := fmt.Sprintf("%s - <t:%d:R>", roleName, msg.CreatedAt.Unix())

			// Censor message content
			censoredContent := utils.CensorStringsInText(
				msg.Content,
				b.settings.StreamerMode,
				strconv.FormatUint(b.appeal.UserID, 10),
			)

			// Format field value with message and user mention
			fieldValue := fmt.Sprintf("<@%d>\n%s", msg.UserID, censoredContent)

			embed.AddField(fieldName, fieldValue, false)
		}

		// Add page number to footer
		embed.SetFooter(fmt.Sprintf("Page %d/%d", b.page+1, b.totalPages+1), "")
	}

	return embed
}
