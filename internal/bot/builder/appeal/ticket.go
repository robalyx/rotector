package appeal

import (
	"fmt"
	"strconv"

	"github.com/disgoorg/disgo/discord"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/bot/utils"
	"github.com/robalyx/rotector/internal/common/storage/database/types"
	"github.com/robalyx/rotector/internal/common/storage/database/types/enum"
)

// TicketBuilder creates the visual layout for an individual appeal ticket.
type TicketBuilder struct {
	appeal        *types.FullAppeal
	messages      []*types.AppealMessage
	rejectedCount int
	page          int
	totalPages    int
	userID        uint64
	isReviewer    bool
	streamerMode  bool
}

// NewTicketBuilder creates a new ticket builder.
func NewTicketBuilder(s *session.Session) *TicketBuilder {
	userID := session.UserID.Get(s)
	return &TicketBuilder{
		appeal:        session.AppealSelected.Get(s),
		messages:      session.AppealMessages.Get(s),
		rejectedCount: session.AppealRejectedCount.Get(s),
		page:          session.PaginationPage.Get(s),
		totalPages:    session.PaginationTotalPages.Get(s),
		userID:        userID,
		isReviewer:    s.BotSettings().IsReviewer(userID),
		streamerMode:  session.UserStreamerMode.Get(s),
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
			discord.NewSecondaryButton("⏮️", string(session.ViewerFirstPage)).WithDisabled(b.page == 0),
			discord.NewSecondaryButton("◀️", string(session.ViewerPrevPage)).WithDisabled(b.page == 0),
			discord.NewSecondaryButton("▶️", string(session.ViewerNextPage)).WithDisabled(b.page == b.totalPages),
			discord.NewSecondaryButton("⏭️", string(session.ViewerLastPage)).WithDisabled(b.page == b.totalPages),
		),
	}

	// Add actions based on user role and appeal status
	switch b.appeal.Status {
	case enum.AppealStatusPending:
		// Create options array for the actions dropdown
		options := []discord.StringSelectMenuOption{
			discord.NewStringSelectMenuOption("Respond to Appeal", constants.AppealRespondButtonCustomID).
				WithEmoji(discord.ComponentEmoji{Name: "💬"}).
				WithDescription("Send a message in this appeal"),
		}

		if b.isReviewer {
			// Add reviewer-specific options
			options = append(options,
				discord.NewStringSelectMenuOption("Lookup User", constants.AppealLookupUserButtonCustomID).
					WithEmoji(discord.ComponentEmoji{Name: "🔍"}).
					WithDescription("View detailed user information"),
			)

			// Add claim option if appeal is unclaimed
			if b.appeal.ClaimedBy == 0 {
				options = append(options,
					discord.NewStringSelectMenuOption("Claim Appeal", constants.AppealClaimButtonCustomID).
						WithEmoji(discord.ComponentEmoji{Name: "📌"}).
						WithDescription("Claim this appeal for review"),
				)
			}

			// Add review action options
			options = append(options,
				discord.NewStringSelectMenuOption("Accept Appeal", constants.AcceptAppealButtonCustomID).
					WithEmoji(discord.ComponentEmoji{Name: "✅"}).
					WithDescription("Clear the user from the system and delete user data"),
				discord.NewStringSelectMenuOption("Reject Appeal", constants.RejectAppealButtonCustomID).
					WithEmoji(discord.ComponentEmoji{Name: "❌"}).
					WithDescription("Reject this appeal"),
				discord.NewStringSelectMenuOption("Delete Data & Opt-Out", constants.DeleteUserDataButtonCustomID).
					WithEmoji(discord.ComponentEmoji{Name: "🗑️"}).
					WithDescription("Delete user data without clearing user"),
			)
		} else {
			// Add close option for regular users
			options = append(options,
				discord.NewStringSelectMenuOption("Close Ticket", constants.AppealCloseButtonCustomID).
					WithEmoji(discord.ComponentEmoji{Name: "❌"}).
					WithDescription("Close this appeal ticket"),
			)
		}

		components = append(components, discord.NewActionRow(
			discord.NewStringSelectMenu(constants.AppealActionSelectID, "Appeal Actions", options...),
		))

	case enum.AppealStatusRejected, enum.AppealStatusAccepted:
		if b.isReviewer {
			// Add reviewer options for closed appeals
			options := []discord.StringSelectMenuOption{
				discord.NewStringSelectMenuOption("Lookup User", constants.AppealLookupUserButtonCustomID).
					WithEmoji(discord.ComponentEmoji{Name: "🔍"}).
					WithDescription("View detailed user information"),
				discord.NewStringSelectMenuOption("Reopen Appeal", constants.ReopenAppealButtonCustomID).
					WithEmoji(discord.ComponentEmoji{Name: "🔄"}).
					WithDescription("Reopen this appeal"),
			}

			components = append(components, discord.NewActionRow(
				discord.NewStringSelectMenu(constants.AppealActionSelectID, "Appeal Actions", options...),
			))
		}
	}

	builder.AddContainerComponents(components...)

	return builder
}

// buildHeaderEmbed creates the embed showing appeal information.
func (b *TicketBuilder) buildHeaderEmbed() *discord.EmbedBuilder {
	// Get status and type emojis
	statusEmoji := b.appeal.Status.Emoji()
	typeEmoji := b.appeal.Type.Emoji()

	// Format timestamps
	submitted := "N/A"
	if !b.appeal.Timestamp.IsZero() {
		submitted = fmt.Sprintf("<t:%d:R>", b.appeal.Timestamp.Unix())
	}

	lastViewed := "N/A"
	if !b.appeal.LastViewed.IsZero() {
		lastViewed = fmt.Sprintf("<t:%d:R>", b.appeal.LastViewed.Unix())
	}

	lastActivity := "N/A"
	if !b.appeal.LastActivity.IsZero() {
		lastActivity = fmt.Sprintf("<t:%d:R>", b.appeal.LastActivity.Unix())
	}

	// Format user info based on appeal type
	var userInfo string
	if b.appeal.Type == enum.AppealTypeRoblox {
		userIDStr := strconv.FormatUint(b.appeal.UserID, 10)
		userInfo = fmt.Sprintf("[%s](https://www.roblox.com/users/%d/profile)",
			utils.CensorString(userIDStr, b.streamerMode), b.appeal.UserID)
	} else {
		userInfo = fmt.Sprintf("<@%d>", b.appeal.UserID)
	}

	// Create embed
	embed := discord.NewEmbedBuilder().
		SetTitle(fmt.Sprintf("%s %s Appeal `#%d`", statusEmoji, typeEmoji, b.appeal.ID)).
		SetColor(utils.GetMessageEmbedColor(b.streamerMode)).
		AddField("User", userInfo, true).
		AddField("Type", b.appeal.Type.String(), true).
		AddField("Status", b.appeal.Status.String(), true).
		AddField("Submitted", submitted, true).
		AddField("Last Viewed", lastViewed, true).
		AddField("Last Activity", lastActivity, true)

	// Add rejected appeals count if available
	if b.rejectedCount > 0 {
		embed.AddField("Rejected Appeals", fmt.Sprintf("`%d`", b.rejectedCount), true)
	}

	if b.appeal.ClaimedBy != 0 {
		// Add claimed information
		embed.AddField("Claimed By", fmt.Sprintf("<@%d>", b.appeal.ClaimedBy), true)

		// Show claimed time if available
		if !b.appeal.ClaimedAt.IsZero() {
			embed.AddField("Claimed At", fmt.Sprintf("<t:%d:R>", b.appeal.ClaimedAt.Unix()), true)
		}

		// Censor any sensitive information in the review reason
		if b.appeal.ReviewReason != "" {
			censoredReason := utils.CensorStringsInText(
				b.appeal.ReviewReason,
				b.streamerMode,
				strconv.FormatUint(b.appeal.UserID, 10),
			)
			embed.AddField("Review Reason", censoredReason, false)
		}
	}

	return embed
}

// buildConversationEmbed creates the embed showing the message history.
func (b *TicketBuilder) buildConversationEmbed() *discord.EmbedBuilder {
	embed := discord.NewEmbedBuilder().
		SetColor(utils.GetMessageEmbedColor(b.streamerMode))

	// Calculate page boundaries
	start := b.page * constants.AppealMessagesPerPage
	end := min(start+constants.AppealMessagesPerPage, len(b.messages))

	// Add messages
	if len(b.messages) == 0 {
		embed.SetDescription("No messages yet.")
	} else {
		for _, msg := range b.messages[start:end] {
			// Format role
			var roleName string
			switch msg.Role {
			case enum.MessageRoleModerator:
				roleName = "Moderator"
			case enum.MessageRoleUser:
				roleName = "User"
			}

			// Format field title with role and time
			fieldName := fmt.Sprintf("%s - <t:%d:R>", roleName, msg.CreatedAt.Unix())

			// Censor message content
			censoredContent := utils.CensorStringsInText(
				msg.Content,
				b.streamerMode,
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
