package group

import (
	"fmt"

	"github.com/disgoorg/disgo/discord"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/bot/utils"
	"github.com/robalyx/rotector/internal/database/types"
)

// CommentsBuilder creates the visual layout for the comments interface.
type CommentsBuilder struct {
	botSettings *types.BotSetting
	group       *types.ReviewGroup
	commenterID uint64
	comments    []*types.Comment
	page        int
	offset      int
	totalItems  int
	totalPages  int
	privacyMode bool
}

// NewCommentsBuilder creates a new comments builder.
func NewCommentsBuilder(s *session.Session) *CommentsBuilder {
	return &CommentsBuilder{
		botSettings: s.BotSettings(),
		group:       session.GroupTarget.Get(s),
		commenterID: session.UserID.Get(s),
		comments:    session.ReviewComments.Get(s),
		page:        session.PaginationPage.Get(s),
		offset:      session.PaginationOffset.Get(s),
		totalItems:  session.PaginationTotalItems.Get(s),
		totalPages:  session.PaginationTotalPages.Get(s),
		privacyMode: session.UserStreamerMode.Get(s),
	}
}

// Build creates a Discord message showing the comments and controls.
func (b *CommentsBuilder) Build() *discord.MessageUpdateBuilder {
	return discord.NewMessageUpdateBuilder().
		SetEmbeds(b.buildEmbed().Build()).
		AddContainerComponents(b.buildComponents()...)
}

// buildEmbed creates the embed for the comments interface.
func (b *CommentsBuilder) buildEmbed() *discord.EmbedBuilder {
	embed := discord.NewEmbedBuilder().
		SetTitle("📝 Community Notes").
		SetDescription(fmt.Sprintf("Notes for group `%s`", utils.CensorString(b.group.Name, b.privacyMode))).
		SetColor(utils.GetMessageEmbedColor(b.privacyMode))

	if len(b.comments) == 0 {
		embed.AddField("No Notes", "No community notes yet. Be the first to add one!", false)
		return embed
	}

	// Calculate page boundaries
	end := min(b.offset+constants.CommentsPerPage, b.totalItems)

	// Add comments for this page
	for _, comment := range b.comments[b.offset:end] {
		timestamp := fmt.Sprintf("<t:%d:R>", comment.CreatedAt.Unix())
		if !comment.UpdatedAt.Equal(comment.CreatedAt) {
			timestamp += fmt.Sprintf(" (edited <t:%d:R>)", comment.UpdatedAt.Unix())
		}

		// Determine user role
		var roleTitle string
		switch {
		case b.botSettings.IsAdmin(comment.CommenterID):
			roleTitle = "Administrator Note"
		case b.botSettings.IsReviewer(comment.CommenterID):
			roleTitle = "Reviewer Note"
		default:
			roleTitle = "Community Note"
		}

		embed.AddField(
			roleTitle,
			fmt.Sprintf("From <@%d> - %s\n```%s```",
				comment.CommenterID,
				timestamp,
				comment.Message,
			),
			false,
		)
	}

	// Add page number and total items to footer
	embed.SetFooter(fmt.Sprintf("Page %d/%d • Showing %d-%d of %d notes",
		b.page+1, b.totalPages+1, b.offset+1, end, b.totalItems), "")

	return embed
}

// buildComponents creates all the interactive components.
func (b *CommentsBuilder) buildComponents() []discord.ContainerComponent {
	components := []discord.ContainerComponent{
		// Navigation buttons
		discord.NewActionRow(
			discord.NewSecondaryButton("◀️", constants.BackButtonCustomID),
			discord.NewSecondaryButton("⏮️", string(session.ViewerFirstPage)).WithDisabled(b.page == 0),
			discord.NewSecondaryButton("◀️", string(session.ViewerPrevPage)).WithDisabled(b.page == 0),
			discord.NewSecondaryButton("▶️", string(session.ViewerNextPage)).WithDisabled(b.page == b.totalPages),
			discord.NewSecondaryButton("⏭️", string(session.ViewerLastPage)).WithDisabled(b.page == b.totalPages),
		),
	}

	// Check if user has a comment
	var hasExistingComment bool
	for _, comment := range b.comments {
		if comment.CommenterID == b.commenterID {
			hasExistingComment = true
			break
		}
	}

	// Add appropriate action buttons
	actionButtons := []discord.InteractiveComponent{}
	if hasExistingComment {
		actionButtons = append(actionButtons,
			discord.NewPrimaryButton("Edit Note", constants.AddCommentButtonCustomID),
			discord.NewDangerButton("Delete Note", constants.DeleteCommentButtonCustomID),
		)
	} else {
		actionButtons = append(actionButtons,
			discord.NewPrimaryButton("Add Note", constants.AddCommentButtonCustomID),
		)
	}

	components = append(components, discord.NewActionRow(actionButtons...))
	return components
}
