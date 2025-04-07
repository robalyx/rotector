package shared

import (
	"errors"

	"github.com/disgoorg/disgo/discord"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/interaction"
	"github.com/robalyx/rotector/internal/bot/core/session"
	view "github.com/robalyx/rotector/internal/bot/views/review/shared"
	"github.com/robalyx/rotector/internal/database"
	"github.com/robalyx/rotector/internal/database/service"
	"github.com/robalyx/rotector/internal/database/types"
	"go.uber.org/zap"
)

// CommentsMenu handles the display and interaction logic for viewing comments.
type CommentsMenu struct {
	logger     *zap.Logger
	db         database.Client
	targetType view.TargetType
	page       *interaction.Page
}

// NewCommentsMenu creates a new comments menu.
func NewCommentsMenu(logger *zap.Logger, db database.Client, targetType view.TargetType, pageName string) *CommentsMenu {
	m := &CommentsMenu{
		logger:     logger,
		db:         db,
		targetType: targetType,
	}
	m.page = &interaction.Page{
		Name: pageName,
		Message: func(s *session.Session) *discord.MessageUpdateBuilder {
			return view.NewCommentsBuilder(s, targetType).Build()
		},
		ShowHandlerFunc:   m.Show,
		ButtonHandlerFunc: m.handleButton,
		ModalHandlerFunc:  m.handleModal,
	}
	return m
}

// Page returns the page for the comments menu.
func (m *CommentsMenu) Page() *interaction.Page {
	return m.page
}

// Show prepares and displays the comments interface.
func (m *CommentsMenu) Show(ctx *interaction.Context, s *session.Session) {
	var targetID uint64
	if m.targetType == view.TargetTypeUser {
		targetID = session.UserTarget.Get(s).ID
	} else {
		targetID = session.GroupTarget.Get(s).ID
	}

	// Fetch updated comments
	var comments []*types.Comment
	var err error
	if m.targetType == view.TargetTypeUser {
		comments, err = m.db.Model().Comment().GetUserComments(ctx.Context(), targetID)
	} else {
		comments, err = m.db.Model().Comment().GetGroupComments(ctx.Context(), targetID)
	}

	if err != nil {
		m.logger.Error("Failed to fetch comments", zap.Error(err))
		comments = []*types.Comment{} // Continue without comments - not critical
	}
	session.ReviewComments.Set(s, comments)

	// Store pagination info in session
	page := session.PaginationPage.Get(s)
	totalPages := max((len(comments)-1)/constants.CommentsPerPage, 0)

	session.PaginationOffset.Set(s, page*constants.CommentsPerPage)
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
	input = input.WithPlaceholder("Enter your note...")

	modal.AddActionRow(input)
	ctx.Modal(modal)
}

// handleDeleteComment deletes the user's comment.
func (m *CommentsMenu) handleDeleteComment(ctx *interaction.Context, s *session.Session) {
	var targetID uint64
	if m.targetType == view.TargetTypeUser {
		targetID = session.UserTarget.Get(s).ID
	} else {
		targetID = session.GroupTarget.Get(s).ID
	}

	commenterID := uint64(ctx.Event().User().ID)

	var err error
	if m.targetType == view.TargetTypeUser {
		err = m.db.Model().Comment().DeleteUserComment(ctx.Context(), targetID, commenterID)
	} else {
		err = m.db.Model().Comment().DeleteGroupComment(ctx.Context(), targetID, commenterID)
	}

	if err != nil {
		m.logger.Error("Failed to delete comment", zap.Error(err))
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

	var targetID uint64
	if m.targetType == view.TargetTypeUser {
		targetID = session.UserTarget.Get(s).ID
	} else {
		targetID = session.GroupTarget.Get(s).ID
	}

	// Create comment
	comment := &types.Comment{
		TargetID:    targetID,
		CommenterID: uint64(ctx.Event().User().ID),
		Message:     message,
	}

	var err error
	if m.targetType == view.TargetTypeUser {
		err = m.db.Service().Comment().AddUserComment(ctx.Context(), &types.UserComment{Comment: *comment})
	} else {
		err = m.db.Service().Comment().AddGroupComment(ctx.Context(), &types.GroupComment{Comment: *comment})
	}

	if err != nil {
		switch {
		case errors.Is(err, service.ErrCommentTooSimilar):
			ctx.Cancel("Your note is too similar to an existing note. Please provide unique information.")
		case errors.Is(err, service.ErrInvalidLinks):
			ctx.Cancel("Only Roblox links are allowed in notes.")
		case errors.Is(err, types.ErrCommentExists):
			ctx.Cancel("You already have a note for this target. Delete your existing note first.")
		default:
			m.logger.Error("Failed to add comment", zap.Error(err))
			ctx.Error("Failed to add note. Please try again.")
		}
		return
	}

	// Refresh page
	ctx.Reload("Successfully added note")
}
