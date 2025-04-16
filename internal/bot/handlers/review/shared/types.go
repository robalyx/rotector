package shared

import (
	"errors"
	"fmt"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/captcha"
	"github.com/robalyx/rotector/internal/bot/core/interaction"
	"github.com/robalyx/rotector/internal/bot/core/session"
	view "github.com/robalyx/rotector/internal/bot/views/review/shared"
	"github.com/robalyx/rotector/internal/database"
	"github.com/robalyx/rotector/internal/database/types"
	"go.uber.org/zap"
)

var ErrBreakRequired = errors.New("break required")

// BaseReviewMenu contains common fields and methods for review menus.
type BaseReviewMenu struct {
	logger  *zap.Logger
	captcha *captcha.Manager
	db      database.Client
}

// NewBaseReviewMenu creates a new base review menu.
func NewBaseReviewMenu(logger *zap.Logger, captcha *captcha.Manager, db database.Client) *BaseReviewMenu {
	return &BaseReviewMenu{
		logger:  logger,
		captcha: captcha,
		db:      db,
	}
}

// HandleAddComment shows the modal for adding or editing a comment.
func (m *BaseReviewMenu) HandleAddComment(ctx *interaction.Context, s *session.Session) {
	comments := session.ReviewComments.Get(s)

	// Check global comment limit
	if len(comments) >= constants.CommentLimit {
		ctx.Cancel(
			fmt.Sprintf("Cannot add more notes - global limit of %d notes has been reached.", constants.CommentLimit),
		)
		return
	}

	// Find user's existing comment
	userID := uint64(ctx.Event().User().ID)
	var existingComment *types.Comment
	for _, comment := range comments {
		if comment.CommenterID == userID {
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
		WithMaxLength(512).
		WithPlaceholder("Enter your note...")

	if existingComment != nil {
		input = input.WithValue(existingComment.Message)
	}

	modal.AddActionRow(input)
	ctx.Modal(modal)
}

// HandleDeleteComment deletes the user's comment.
func (m *BaseReviewMenu) HandleDeleteComment(ctx *interaction.Context, s *session.Session, targetType view.TargetType) {
	var targetID uint64
	if targetType == view.TargetTypeUser {
		targetID = session.UserTarget.Get(s).ID
	} else {
		targetID = session.GroupTarget.Get(s).ID
	}

	commenterID := uint64(ctx.Event().User().ID)

	var err error
	if targetType == view.TargetTypeUser {
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

// HandleCommentModalSubmit processes the comment from the modal.
func (m *BaseReviewMenu) HandleCommentModalSubmit(ctx *interaction.Context, s *session.Session, targetType view.TargetType) {
	// Get message from modal
	message := ctx.Event().ModalData().Text(constants.CommentMessageInputCustomID)
	if message == "" {
		ctx.Cancel("Note cannot be empty")
		return
	}

	var targetID uint64
	if targetType == view.TargetTypeUser {
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
	if targetType == view.TargetTypeUser {
		err = m.db.Service().Comment().AddUserComment(ctx.Context(), &types.UserComment{Comment: *comment})
	} else {
		err = m.db.Service().Comment().AddGroupComment(ctx.Context(), &types.GroupComment{Comment: *comment})
	}

	if err != nil {
		m.logger.Error("Failed to add comment", zap.Error(err))
		ctx.Error("Failed to add note. Please try again.")
		return
	}

	// Refresh page
	ctx.Reload("Successfully added note")
}

// CheckBreakRequired checks if a break is needed.
func (m *BaseReviewMenu) CheckBreakRequired(ctx *interaction.Context, s *session.Session) bool {
	// Check if user needs a break
	nextReviewTime := session.UserReviewBreakNextReviewTime.Get(s)
	if !nextReviewTime.IsZero() && time.Now().Before(nextReviewTime) {
		// Show timeout menu if break time hasn't passed
		ctx.Show(constants.TimeoutPageName, "")
		return true
	}

	// Check review count
	sessionReviews := session.UserReviewBreakSessionReviews.Get(s)
	sessionStartTime := session.UserReviewBreakSessionStartTime.Get(s)

	// Reset count if outside window
	if time.Since(sessionStartTime) > constants.ReviewSessionWindow {
		sessionReviews = 0
		sessionStartTime = time.Now()
		session.UserReviewBreakSessionStartTime.Set(s, sessionStartTime)
	}

	// Check if break needed
	if sessionReviews >= constants.MaxReviewsBeforeBreak {
		nextTime := time.Now().Add(constants.MinBreakDuration)
		session.UserReviewBreakSessionStartTime.Set(s, nextTime)
		session.UserReviewBreakNextReviewTime.Set(s, nextTime)
		session.UserReviewBreakSessionReviews.Set(s, 0) // Reset count
		ctx.Show(constants.TimeoutPageName, "")
		return true
	}

	// Increment review count
	session.UserReviewBreakSessionReviews.Set(s, sessionReviews+1)

	return false
}

// CheckCaptchaRequired checks if CAPTCHA verification is needed.
func (m *BaseReviewMenu) CheckCaptchaRequired(ctx *interaction.Context, s *session.Session) bool {
	if m.captcha.IsRequired(s) {
		ctx.Cancel("Please complete CAPTCHA verification to continue.")
		return true
	}
	return false
}

// UpdateCounters updates the review counters.
func (m *BaseReviewMenu) UpdateCounters(s *session.Session) {
	if err := m.captcha.IncrementReviewCounter(s); err != nil {
		m.logger.Error("Failed to update review counter", zap.Error(err))
	}
}
