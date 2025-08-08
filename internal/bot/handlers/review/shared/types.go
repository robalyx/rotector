package shared

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/captcha"
	"github.com/robalyx/rotector/internal/bot/core/interaction"
	"github.com/robalyx/rotector/internal/bot/core/session"
	view "github.com/robalyx/rotector/internal/bot/views/review/shared"
	"github.com/robalyx/rotector/internal/database"
	"github.com/robalyx/rotector/internal/database/types"
	"github.com/robalyx/rotector/internal/database/types/enum"
	"github.com/robalyx/rotector/pkg/utils"
	"go.uber.org/zap"
)

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
	var targetID int64
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

	var targetID int64
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
		ctx.UpdatePage(constants.DashboardPageName)
		ctx.Show(constants.TimeoutPageName, "")

		return true
	}

	// Check review count
	windowStartTime := session.UserReviewBreakWindowStartTime.Get(s)
	reviewCount := session.UserReviewBreakReviewCount.Get(s)

	// Reset count if outside window
	if time.Since(windowStartTime) > constants.ReviewSessionWindow {
		reviewCount = 0
		windowStartTime = time.Now()
		session.UserReviewBreakWindowStartTime.Set(s, windowStartTime)
		session.UserReviewBreakReviewCount.Set(s, reviewCount)
	}

	// Check if break needed
	if reviewCount >= constants.MaxReviewsBeforeBreak {
		nextTime := time.Now().Add(constants.MinBreakDuration)
		session.UserReviewBreakNextReviewTime.Set(s, nextTime)
		session.UserReviewBreakWindowStartTime.Set(s, nextTime)
		session.UserReviewBreakReviewCount.Set(s, 0) // Reset count

		ctx.UpdatePage(constants.DashboardPageName)
		ctx.Show(constants.TimeoutPageName, "")

		return true
	}

	// Increment review count
	session.UserReviewBreakReviewCount.Set(s, reviewCount+1)

	return false
}

// CheckCaptchaRequired checks if CAPTCHA verification is needed.
func (m *BaseReviewMenu) CheckCaptchaRequired(ctx *interaction.Context, s *session.Session) bool {
	if m.captcha.IsRequired(s) {
		ctx.Show(constants.CaptchaPageName, "Please complete CAPTCHA verification to continue.")
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

// HandleEditReason handles the edit reason button click for review menus.
func HandleEditReason[T types.ReasonType](
	ctx *interaction.Context, s *session.Session, logger *zap.Logger,
	reasonType T, reasons types.Reasons[T], updateReasons func(types.Reasons[T]),
) {
	// Check if user is a reviewer
	if !s.BotSettings().IsReviewer(uint64(ctx.Event().User().ID)) {
		logger.Error("Non-reviewer attempted to manage reasons",
			zap.Uint64("userID", uint64(ctx.Event().User().ID)))
		ctx.Error("You do not have permission to manage reasons.")

		return
	}

	// Initialize reasons map if nil
	if reasons == nil {
		reasons = make(types.Reasons[T])
		updateReasons(reasons)
	}

	// Store the reason type in session
	session.SelectedReasonType.Set(s, reasonType.String())

	// Get existing reason if editing
	var existingReason *types.Reason
	if existing, exists := reasons[reasonType]; exists {
		existingReason = existing
	}

	// Show modal to user
	ctx.Modal(BuildReasonModal(reasonType, existingReason))
}

// HandleReasonModalSubmit processes the reason message from the modal.
func HandleReasonModalSubmit[T types.ReasonType](
	ctx *interaction.Context, s *session.Session, reasons types.Reasons[T],
	parseReasonType func(string) (T, error), updateReasons func(types.Reasons[T]),
	updateConfidence func(float64),
) {
	// Get the reason type from session
	reasonTypeStr := session.SelectedReasonType.Get(s)

	reasonType, err := parseReasonType(reasonTypeStr)
	if err != nil {
		ctx.Error("Invalid reason type: " + reasonTypeStr)
		return
	}

	// Initialize reasons map if nil
	if reasons == nil {
		reasons = make(types.Reasons[T])
		updateReasons(reasons)
	}

	// Get the reason message from the modal
	data := ctx.Event().ModalData()
	reasonMessage := data.Text(constants.AddReasonInputCustomID)
	confidenceStr := data.Text(constants.AddReasonConfidenceInputCustomID)
	evidenceText := data.Text(constants.AddReasonEvidenceInputCustomID)

	// Get existing reason if editing
	var existingReason *types.Reason
	if existing, exists := reasons[reasonType]; exists {
		existingReason = existing
	}

	// Create or update reason
	var reason types.Reason

	if existingReason != nil {
		// Check if reasons field is empty
		if reasonMessage == "" {
			delete(reasons, reasonType)
			newConfidence := utils.CalculateConfidence(reasons)
			updateConfidence(newConfidence)
			updateReasons(reasons)

			// Mark reason as unsaved and update session
			markReasonAsUnsaved(s, reasonType)
			session.SelectedReasonType.Delete(s)
			session.ReasonsChanged.Set(s, true)

			ctx.Reload(fmt.Sprintf("Successfully removed %s reason", reasonType.String()))

			return
		}

		// Check if confidence is empty
		if confidenceStr == "" {
			ctx.Cancel("Confidence is required when updating a reason.")
			return
		}

		// Parse confidence
		confidence, err := strconv.ParseFloat(confidenceStr, 64)
		if err != nil || confidence < 0.01 || confidence > 1.0 {
			ctx.Cancel("Invalid confidence value. Please enter a number between 0.01 and 1.00.")
			return
		}

		// Parse evidence items
		var evidence []string

		for line := range strings.SplitSeq(evidenceText, "\n") {
			if trimmed := strings.TrimSpace(line); trimmed != "" {
				evidence = append(evidence, trimmed)
			}
		}

		reason = types.Reason{
			Message:    reasonMessage,
			Confidence: confidence,
			Evidence:   evidence,
		}
	} else {
		// For new reasons, message and confidence are required
		if reasonMessage == "" || confidenceStr == "" {
			ctx.Cancel("Reason message and confidence are required for new reasons.")
			return
		}

		// Parse confidence
		confidence, err := strconv.ParseFloat(confidenceStr, 64)
		if err != nil || confidence < 0.01 || confidence > 1.0 {
			ctx.Cancel("Invalid confidence value. Please enter a number between 0.01 and 1.00.")
			return
		}

		// Parse evidence items
		var evidence []string

		if evidenceText != "" {
			for line := range strings.SplitSeq(evidenceText, "\n") {
				if trimmed := strings.TrimSpace(line); trimmed != "" {
					evidence = append(evidence, trimmed)
				}
			}
		}

		reason = types.Reason{
			Message:    reasonMessage,
			Confidence: confidence,
			Evidence:   evidence,
		}
	}

	// Update the reason
	reasons[reasonType] = &reason

	// Recalculate overall confidence
	newConfidence := utils.CalculateConfidence(reasons)
	updateConfidence(newConfidence)
	updateReasons(reasons)

	// Mark reason as unsaved and update session
	markReasonAsUnsaved(s, reasonType)
	session.SelectedReasonType.Delete(s)
	session.ReasonsChanged.Set(s, true)

	action := "added"
	if existingReason != nil {
		action = "updated"
	}

	ctx.Reload(fmt.Sprintf("Successfully %s %s reason", action, reasonType.String()))
}

// BuildReasonModal creates a modal for adding or editing a reason.
func BuildReasonModal[T types.ReasonType](reasonType T, existingReason *types.Reason) *discord.ModalCreateBuilder {
	// Create modal for reason input
	modal := discord.NewModalCreateBuilder().
		SetCustomID(constants.AddReasonModalCustomID).
		SetTitle(
			fmt.Sprintf("%s %s Reason",
				map[bool]string{true: "Edit", false: "Add"}[existingReason != nil],
				reasonType.String(),
			),
		)

	// Add reason input field
	reasonInput := discord.NewTextInput(
		constants.AddReasonInputCustomID, discord.TextInputStyleParagraph, "Reason (leave empty to remove)",
	)
	if existingReason != nil {
		reasonInput = reasonInput.WithRequired(false).
			WithValue(existingReason.Message).
			WithPlaceholder("Enter new reason message, or leave empty to remove")
	} else {
		reasonInput = reasonInput.WithRequired(true).
			WithMinLength(32).
			WithMaxLength(256).
			WithPlaceholder("Enter the reason for flagging")
	}

	modal.AddActionRow(reasonInput)

	// Add confidence input field
	confidenceInput := discord.NewTextInput(
		constants.AddReasonConfidenceInputCustomID, discord.TextInputStyleShort, "Confidence",
	)
	if existingReason != nil {
		confidenceInput = confidenceInput.WithRequired(false).
			WithValue(fmt.Sprintf("%.2f", existingReason.Confidence)).
			WithPlaceholder("Enter new confidence value (0.01-1.00)")
	} else {
		confidenceInput = confidenceInput.WithRequired(true).
			WithMinLength(1).
			WithMaxLength(4).
			WithValue("1.00").
			WithPlaceholder("Enter confidence value (0.01-1.00)")
	}

	modal.AddActionRow(confidenceInput)

	// Add evidence input field
	evidenceInput := discord.NewTextInput(
		constants.AddReasonEvidenceInputCustomID, discord.TextInputStyleParagraph, "Evidence",
	)

	if existingReason != nil {
		// Replace newlines within each evidence item before joining
		escapedEvidence := make([]string, len(existingReason.Evidence))
		for i, evidence := range existingReason.Evidence {
			escapedEvidence[i] = strings.ReplaceAll(evidence, "\n", "\\n")
		}

		evidenceInput = evidenceInput.WithRequired(false).
			WithValue(strings.Join(escapedEvidence, "\n\n")).
			WithPlaceholder("Enter new evidence items, one per line")
	} else {
		evidenceInput = evidenceInput.WithRequired(false).
			WithMaxLength(1000).
			WithPlaceholder("Enter evidence items, one per line")
	}

	modal.AddActionRow(evidenceInput)

	return modal
}

// markReasonAsUnsaved marks a reason type as unsaved based on its type.
func markReasonAsUnsaved[T types.ReasonType](s *session.Session, reasonType T) {
	switch any(reasonType).(type) {
	case enum.UserReasonType:
		userReasonType := any(reasonType).(enum.UserReasonType)

		unsavedReasons := session.UnsavedUserReasons.Get(s)
		if unsavedReasons == nil {
			unsavedReasons = make(map[enum.UserReasonType]struct{})
		}

		unsavedReasons[userReasonType] = struct{}{}
		session.UnsavedUserReasons.Set(s, unsavedReasons)
	case enum.GroupReasonType:
		groupReasonType := any(reasonType).(enum.GroupReasonType)

		unsavedReasons := session.UnsavedGroupReasons.Get(s)
		if unsavedReasons == nil {
			unsavedReasons = make(map[enum.GroupReasonType]struct{})
		}

		unsavedReasons[groupReasonType] = struct{}{}
		session.UnsavedGroupReasons.Set(s, unsavedReasons)
	}
}
