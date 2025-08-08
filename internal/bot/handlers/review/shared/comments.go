package shared

import (
	"github.com/disgoorg/disgo/discord"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/interaction"
	"github.com/robalyx/rotector/internal/bot/core/session"
	view "github.com/robalyx/rotector/internal/bot/views/review/shared"
	"github.com/robalyx/rotector/internal/database"
	"github.com/robalyx/rotector/internal/database/types"
	"go.uber.org/zap"
)

// CommentsMenu handles the display and interaction logic for viewing comments.
type CommentsMenu struct {
	BaseReviewMenu

	targetType view.TargetType
	page       *interaction.Page
}

// NewCommentsMenu creates a new comments menu.
func NewCommentsMenu(logger *zap.Logger, db database.Client, targetType view.TargetType, pageName string) *CommentsMenu {
	m := &CommentsMenu{
		BaseReviewMenu: *NewBaseReviewMenu(logger, nil, db),
		targetType:     targetType,
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
	var targetID int64
	if m.targetType == view.TargetTypeUser {
		targetID = session.UserTarget.Get(s).ID
	} else {
		targetID = session.GroupTarget.Get(s).ID
	}

	// Fetch updated comments
	var (
		comments []*types.Comment
		err      error
	)

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
		m.HandleAddComment(ctx, s)
	case constants.DeleteCommentButtonCustomID:
		m.HandleDeleteComment(ctx, s, m.targetType)
	}
}

// handleModal processes modal submissions.
func (m *CommentsMenu) handleModal(ctx *interaction.Context, s *session.Session) {
	switch ctx.Event().CustomID() {
	case constants.AddCommentModalCustomID:
		m.HandleCommentModalSubmit(ctx, s, m.targetType)
	}
}
