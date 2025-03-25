package group

import (
	"errors"

	"github.com/disgoorg/disgo/discord"
	builder "github.com/robalyx/rotector/internal/bot/builder/review/group"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/interaction"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/common/storage/database/service"
	"github.com/robalyx/rotector/internal/common/storage/database/types"
	"go.uber.org/zap"
)

// CommentsMenu handles the display and interaction logic for viewing group comments.
type CommentsMenu struct {
	layout *Layout
	page   *interaction.Page
}

// NewCommentsMenu creates a new comments menu.
func NewCommentsMenu(layout *Layout) *CommentsMenu {
	m := &CommentsMenu{layout: layout}
	m.page = &interaction.Page{
		Name: constants.GroupCommentsPageName,
		Message: func(s *session.Session) *discord.MessageUpdateBuilder {
			return builder.NewCommentsBuilder(s).Build()
		},
		ShowHandlerFunc:   m.Show,
		ButtonHandlerFunc: m.handleButton,
		ModalHandlerFunc:  m.handleModal,
	}
	return m
}

// Show prepares and displays the comments interface.
func (m *CommentsMenu) Show(ctx *interaction.Context, s *session.Session) {
	group := session.GroupTarget.Get(s)

	// Fetch updated comments for the group
	comments, err := m.layout.db.Model().Comment().GetGroupComments(ctx.Context(), group.ID)
	if err != nil {
		m.layout.logger.Error("Failed to fetch user comments", zap.Error(err))
		comments = []*types.Comment{} // Continue without comments - not critical
	}
	session.ReviewComments.Set(s, comments)

	// Store pagination info in session
	totalPages := max((len(comments)-1)/constants.CommentsPerPage, 0)
	session.PaginationOffset.Set(s, session.PaginationPage.Get(s)*constants.CommentsPerPage)
	session.PaginationTotalItems.Set(s, len(comments))
	session.PaginationTotalPages.Set(s, totalPages)
}

// handleButton processes button interactions.
func (m *CommentsMenu) handleButton(ctx *interaction.Context, s *session.Session, customID string) {
	action := session.ViewerAction(customID)
	switch action {
	case session.ViewerFirstPage, session.ViewerPrevPage, session.ViewerNextPage, session.ViewerLastPage:
		totalPages := session.PaginationTotalPages.Get(s)
		page := action.ParsePageAction(s, totalPages)

		// Update pagination info
		session.PaginationPage.Set(s, page)
		session.PaginationOffset.Set(s, page*constants.CommentsPerPage)
		ctx.Reload("")
		return
	}

	switch customID {
	case constants.BackButtonCustomID:
		ctx.NavigateBack("")
	case constants.AddCommentButtonCustomID:
		m.handleAddComment(ctx, s)
	case constants.DeleteCommentButtonCustomID:
		m.handleDeleteComment(ctx, s)
	}
}

// handleModal processes modal submissions.
func (m *CommentsMenu) handleModal(ctx *interaction.Context, s *session.Session) {
	switch ctx.Event().CustomID() {
	case constants.AddCommentModalCustomID:
		m.handleCommentModalSubmit(ctx, s)
	}
}

// handleAddComment shows the modal for adding or editing a comment.
func (m *CommentsMenu) handleAddComment(ctx *interaction.Context, s *session.Session) {
	comments := session.ReviewComments.Get(s)
	page := session.PaginationPage.Get(s)
	start := page * constants.CommentsPerPage
	end := min(start+constants.CommentsPerPage, len(comments))

	// Check if user has an existing comment
	var existingComment *types.Comment
	for _, comment := range comments[start:end] {
		if comment.CommenterID == uint64(ctx.Event().User().ID) {
			existingComment = comment
			break
		}
	}

	modal := discord.NewModalCreateBuilder().
		SetCustomID(constants.AddCommentModalCustomID).
		SetTitle(map[bool]string{true: "Edit", false: "Add"}[existingComment != nil] + " Community Note")

	input := discord.NewTextInput(
		constants.CommentMessageInputCustomID,
		discord.TextInputStyleParagraph,
		"Note",
	).WithRequired(true).
		WithMinLength(10).
		WithMaxLength(512)

	if existingComment != nil {
		input = input.WithValue(existingComment.Message)
	}
	input = input.WithPlaceholder("Enter your note about this group...")

	modal.AddActionRow(input)
	ctx.Modal(modal)
}

// handleDeleteComment deletes the user's comment.
func (m *CommentsMenu) handleDeleteComment(ctx *interaction.Context, s *session.Session) {
	group := session.GroupTarget.Get(s)
	commenterID := uint64(ctx.Event().User().ID)

	if err := m.layout.db.Model().Comment().DeleteGroupComment(ctx.Context(), group.ID, commenterID); err != nil {
		m.layout.logger.Error("Failed to delete comment", zap.Error(err))
		ctx.Error("Failed to delete note. Please try again.")
		return
	}

	// Refresh comments
	ctx.Reload("Note deleted successfully.")
}

// handleCommentModalSubmit processes the comment from the modal.
func (m *CommentsMenu) handleCommentModalSubmit(ctx *interaction.Context, s *session.Session) {
	// Get message from modal
	message := ctx.Event().ModalData().Text(constants.CommentMessageInputCustomID)
	if message == "" {
		ctx.Cancel("Note cannot be empty")
		return
	}

	// Add comment for group
	group := session.GroupTarget.Get(s)
	comment := &types.GroupComment{
		Comment: types.Comment{
			TargetID:    group.ID,
			CommenterID: uint64(ctx.Event().User().ID),
			Message:     message,
		},
	}

	if err := m.layout.db.Service().Comment().AddGroupComment(ctx.Context(), comment); err != nil {
		switch {
		case errors.Is(err, service.ErrCommentTooSimilar):
			ctx.Cancel("Your note is too similar to an existing note. Please provide unique information.")
		case errors.Is(err, service.ErrInvalidLinks):
			ctx.Cancel("Only Roblox links are allowed in notes.")
		case errors.Is(err, types.ErrCommentExists):
			ctx.Cancel("You already have a note for this group. Delete your existing note first.")
		default:
			m.layout.logger.Error("Failed to add comment", zap.Error(err))
			ctx.Error("Failed to add note. Please try again.")
		}
		return
	}

	// Refresh page
	ctx.Reload("Successfully added note")
}
