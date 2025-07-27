package shared

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/disgoorg/disgo/discord"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/bot/utils"
	"github.com/robalyx/rotector/internal/database/types"
)

// CommentsBuilder creates the visual layout for the comments interface.
type CommentsBuilder struct {
	botSettings *types.BotSetting
	targetType  TargetType
	targetName  string
	targetID    uint64
	commenterID uint64
	comments    []*types.Comment
	page        int
	offset      int
	totalItems  int
	totalPages  int
	privacyMode bool
}

// NewCommentsBuilder creates a new comments builder.
func NewCommentsBuilder(s *session.Session, targetType TargetType) *CommentsBuilder {
	var (
		targetName string
		targetID   uint64
	)

	// Get target info based on type

	if targetType == TargetTypeUser {
		user := session.UserTarget.Get(s)
		targetName = user.Name
		targetID = user.ID
	} else {
		group := session.GroupTarget.Get(s)
		targetName = group.Name
		targetID = group.ID
	}

	return &CommentsBuilder{
		botSettings: s.BotSettings(),
		targetType:  targetType,
		targetName:  targetName,
		targetID:    targetID,
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
		AddComponents(b.buildComponents()...)
}

// buildComponents creates all the components for the comments interface.
func (b *CommentsBuilder) buildComponents() []discord.LayoutComponent {
	var components []discord.ContainerSubComponent

	// Add header
	var content strings.Builder
	content.WriteString("## Community Notes\n")
	content.WriteString(fmt.Sprintf("```%s (%s)```",
		utils.CensorString(b.targetName, b.privacyMode),
		utils.CensorString(strconv.FormatUint(b.targetID, 10), b.privacyMode),
	))

	if len(b.comments) == 0 {
		content.WriteString("\nNo community notes yet. Be the first to add one!")
		components = append(components, discord.NewTextDisplay(content.String()))
	} else {
		components = append(components,
			discord.NewTextDisplay(content.String()),
			discord.NewLargeSeparator(),
		)

		// Calculate page boundaries
		end := min(b.offset+constants.CommentsPerPage, b.totalItems)

		// Add comments for this page
		var commentsContent strings.Builder

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

			commentsContent.WriteString(fmt.Sprintf("### %s\nFrom <@%d> - %s\n%s\n",
				roleTitle,
				comment.CommenterID,
				timestamp,
				utils.FormatString(comment.Message)))
		}

		components = append(components,
			discord.NewTextDisplay(commentsContent.String()),
			discord.NewLargeSeparator(),
		)

		// Add pagination footer
		footerContent := fmt.Sprintf("-# Page %d/%d • Showing %d-%d of %d notes",
			b.page+1, b.totalPages+1, b.offset+1, end, b.totalItems)
		components = append(components, discord.NewTextDisplay(footerContent))

		// Add pagination buttons
		components = append(components, discord.NewActionRow(
			discord.NewSecondaryButton("⏮️", string(session.ViewerFirstPage)).WithDisabled(b.page == 0),
			discord.NewSecondaryButton("◀️", string(session.ViewerPrevPage)).WithDisabled(b.page == 0),
			discord.NewSecondaryButton("▶️", string(session.ViewerNextPage)).WithDisabled(b.page == b.totalPages),
			discord.NewSecondaryButton("⏭️", string(session.ViewerLastPage)).WithDisabled(b.page == b.totalPages),
		))
	}

	// Create main container
	mainContainer := discord.NewContainer(components...).
		WithAccentColor(utils.GetContainerColor(b.privacyMode))

	// Create action buttons
	actionButtons := []discord.InteractiveComponent{
		discord.NewSecondaryButton("◀️ Back", constants.BackButtonCustomID),
	}
	hasExistingComment := false

	for _, comment := range b.comments {
		if comment.CommenterID == b.commenterID {
			hasExistingComment = true
			break
		}
	}

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

	// Create layout components
	return []discord.LayoutComponent{
		mainContainer,
		discord.NewActionRow(actionButtons...),
	}
}
