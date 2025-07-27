package appeal

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/disgoorg/disgo/discord"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/bot/utils"
	"github.com/robalyx/rotector/internal/database/types"
	"github.com/robalyx/rotector/internal/database/types/enum"
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
	builder := discord.NewMessageUpdateBuilder()

	// Add containers
	builder.AddComponents(
		b.buildHeaderContainer(),
		b.buildConversationContainer(),
	)

	// Add back button
	builder.AddComponents(discord.NewActionRow(
		discord.NewSecondaryButton("◀️ Back", constants.BackButtonCustomID),
	))

	return builder
}

// buildHeaderContainer creates the header container with appeal information.
func (b *TicketBuilder) buildHeaderContainer() discord.ContainerComponent {
	var content strings.Builder

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

	// Build header content
	content.WriteString(fmt.Sprintf("## %s %s Appeal `#%d`\n\n", statusEmoji, typeEmoji, b.appeal.ID))
	content.WriteString(fmt.Sprintf("User: %s\n", userInfo))
	content.WriteString(fmt.Sprintf("Type: %s\n", b.appeal.Type.String()))
	content.WriteString(fmt.Sprintf("Status: %s\n", b.appeal.Status.String()))
	content.WriteString(fmt.Sprintf("Submitted: %s\n", submitted))
	content.WriteString(fmt.Sprintf("Last Viewed: %s\n", lastViewed))
	content.WriteString(fmt.Sprintf("Last Activity: %s\n", lastActivity))

	// Add rejected appeals count if available
	if b.rejectedCount > 0 {
		content.WriteString(fmt.Sprintf("Rejected Appeals: `%d`\n", b.rejectedCount))
	}

	if b.appeal.ClaimedBy != 0 {
		// Add claimed information
		content.WriteString(fmt.Sprintf("\nClaimed By: <@%d>\n", b.appeal.ClaimedBy))

		// Show claimed time if available
		if !b.appeal.ClaimedAt.IsZero() {
			content.WriteString(fmt.Sprintf("Claimed At: <t:%d:R>\n", b.appeal.ClaimedAt.Unix()))
		}

		// Censor any sensitive information in the review reason
		if b.appeal.ReviewReason != "" {
			censoredReason := utils.CensorStringsInText(
				b.appeal.ReviewReason,
				b.streamerMode,
				strconv.FormatUint(b.appeal.UserID, 10),
			)
			content.WriteString(fmt.Sprintf("\nReview Reason:\n```\n%s\n```", censoredReason))
		}
	}

	return discord.NewContainer(
		discord.NewTextDisplay(content.String()),
	).WithAccentColor(utils.GetContainerColor(b.streamerMode))
}

// buildConversationContainer creates the conversation container with messages and actions.
func (b *TicketBuilder) buildConversationContainer() discord.ContainerComponent {
	var content strings.Builder
	content.WriteString("## Conversation\n\n")

	// Calculate page boundaries
	start := b.page * constants.AppealMessagesPerPage
	end := min(start+constants.AppealMessagesPerPage, len(b.messages))

	// Add messages
	if len(b.messages) == 0 {
		content.WriteString("No messages yet.")
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
			content.WriteString(fmt.Sprintf("### %s - <t:%d:R>\n", roleName, msg.CreatedAt.Unix()))
			content.WriteString(fmt.Sprintf("<@%d>\n", msg.UserID))

			// Censor message content
			censoredContent := utils.CensorStringsInText(
				msg.Content,
				b.streamerMode,
				strconv.FormatUint(b.appeal.UserID, 10),
			)

			// Format field value with message in a code block
			content.WriteString(fmt.Sprintf("```\n%s\n```\n", censoredContent))
		}

		// Add page number to footer
		content.WriteString(fmt.Sprintf("-# Page %d/%d", b.page+1, b.totalPages+1))
	}

	// Create container components
	components := []discord.ContainerSubComponent{
		discord.NewTextDisplay(content.String()),
		discord.NewLargeSeparator(),
	}

	// Add action menu if applicable
	if actionMenu := b.buildActionMenu(); actionMenu != nil {
		components = append(components, actionMenu)
	}

	// Add pagination buttons
	components = append(components, discord.NewActionRow(
		discord.NewSecondaryButton("⏮️", string(session.ViewerFirstPage)).WithDisabled(b.page == 0),
		discord.NewSecondaryButton("◀️", string(session.ViewerPrevPage)).WithDisabled(b.page == 0),
		discord.NewSecondaryButton("▶️", string(session.ViewerNextPage)).WithDisabled(b.page == b.totalPages),
		discord.NewSecondaryButton("⏭️", string(session.ViewerLastPage)).WithDisabled(b.page == b.totalPages),
	))

	return discord.NewContainer(components...).WithAccentColor(utils.GetContainerColor(b.streamerMode))
}

// buildActionMenu creates the action menu based on appeal status and user role.
func (b *TicketBuilder) buildActionMenu() discord.ContainerSubComponent {
	var options []discord.StringSelectMenuOption

	switch b.appeal.Status {
	case enum.AppealStatusPending:
		// Add respond option for ticket owner or reviewers
		if b.appeal.RequesterID == b.userID || (b.isReviewer && b.appeal.ClaimedBy == b.userID) {
			options = append(options,
				discord.NewStringSelectMenuOption("Respond to Appeal", constants.AppealRespondButtonCustomID).
					WithEmoji(discord.ComponentEmoji{Name: "💬"}).
					WithDescription("Send a message in this appeal"),
			)
		}

		if b.isReviewer {
			// Add lookup option for all reviewers
			options = append(options,
				discord.NewStringSelectMenuOption("Lookup User", constants.AppealLookupUserButtonCustomID).
					WithEmoji(discord.ComponentEmoji{Name: "🔍"}).
					WithDescription("View detailed user information"),
			)

			// Add claim option for reviewers who didn't claim it
			if b.appeal.ClaimedBy != b.userID {
				options = append(options,
					discord.NewStringSelectMenuOption("Claim Appeal", constants.AppealClaimButtonCustomID).
						WithEmoji(discord.ComponentEmoji{Name: "📌"}).
						WithDescription("Claim this appeal for review"),
				)
			} else {
				// Add review actions only if reviewer has claimed the appeal
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
					discord.NewStringSelectMenuOption("Blacklist User", constants.BlacklistUserButtonCustomID).
						WithEmoji(discord.ComponentEmoji{Name: "⛔"}).
						WithDescription("Reject this appeal and blacklist user from creating future appeals"),
				)
			}
		} else if b.appeal.RequesterID == b.userID {
			// Add close option for ticket owner
			options = append(options,
				discord.NewStringSelectMenuOption("Close Ticket", constants.AppealCloseButtonCustomID).
					WithEmoji(discord.ComponentEmoji{Name: "❌"}).
					WithDescription("Close this appeal ticket"),
			)
		}

	case enum.AppealStatusRejected, enum.AppealStatusAccepted:
		if b.isReviewer {
			// Add reviewer options for closed appeals
			options = append(options,
				discord.NewStringSelectMenuOption("Lookup User", constants.AppealLookupUserButtonCustomID).
					WithEmoji(discord.ComponentEmoji{Name: "🔍"}).
					WithDescription("View detailed user information"),
				discord.NewStringSelectMenuOption("Reopen Appeal", constants.ReopenAppealButtonCustomID).
					WithEmoji(discord.ComponentEmoji{Name: "🔄"}).
					WithDescription("Reopen this appeal"),
			)
		}
	}

	if len(options) > 0 {
		return discord.NewActionRow(
			discord.NewStringSelectMenu(constants.AppealActionSelectID, "Appeal Actions", options...),
		)
	}

	return nil
}
