package user

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/robalyx/rotector/internal/ai"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/interaction"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/bot/handlers/log"
	viewShared "github.com/robalyx/rotector/internal/bot/views/review/shared"
	view "github.com/robalyx/rotector/internal/bot/views/review/user"
	"github.com/robalyx/rotector/internal/database/types"
	"github.com/robalyx/rotector/internal/database/types/enum"
	"github.com/robalyx/rotector/internal/roblox/checker"
	"github.com/robalyx/rotector/pkg/utils"
	"go.uber.org/zap"

	"github.com/robalyx/rotector/internal/bot/handlers/review/shared"
)

var ErrBreakRequired = errors.New("break required")

// ReviewMenu handles the display and interaction logic for the review interface.
type ReviewMenu struct {
	shared.BaseReviewMenu

	layout *Layout
	page   *interaction.Page
}

// NewReviewMenu creates a new review menu.
func NewReviewMenu(layout *Layout) *ReviewMenu {
	m := &ReviewMenu{
		BaseReviewMenu: *shared.NewBaseReviewMenu(layout.logger, layout.captcha, layout.db),
		layout:         layout,
	}
	m.page = &interaction.Page{
		Name: constants.UserReviewPageName,
		Message: func(s *session.Session) *discord.MessageUpdateBuilder {
			return view.NewReviewBuilder(s, layout.db).Build()
		},
		ShowHandlerFunc:   m.Show,
		SelectHandlerFunc: m.handleSelectMenu,
		ButtonHandlerFunc: m.handleButton,
		ModalHandlerFunc:  m.handleModal,
	}

	return m
}

// Show prepares and displays the review interface.
func (m *ReviewMenu) Show(ctx *interaction.Context, s *session.Session) {
	// Force training mode if user is not a reviewer
	if !s.BotSettings().IsReviewer(uint64(ctx.Event().User().ID)) && session.UserReviewMode.Get(s) != enum.ReviewModeTraining {
		session.UserReviewMode.Set(s, enum.ReviewModeTraining)
	}

	// If no user is set in session, fetch a new one
	user := session.UserTarget.Get(s)
	if user == nil {
		var err error

		user, err = m.fetchNewTarget(ctx, s)
		if err != nil {
			if errors.Is(err, types.ErrNoUsersToReview) {
				ctx.Show(constants.DashboardPageName, "No users to review. Please check back later.")
				return
			}

			if errors.Is(err, ErrBreakRequired) {
				return
			}

			m.layout.logger.Error("Failed to fetch a new user", zap.Error(err))
			ctx.Error("Failed to fetch a new user. Please try again.")

			return
		}
	}

	// Fetch review logs for the user
	logs, nextCursor, err := m.layout.db.Model().Activity().GetLogs(
		ctx.Context(),
		types.ActivityFilter{
			UserID:       user.ID,
			GroupID:      0,
			ReviewerID:   0,
			ActivityType: enum.ActivityTypeAll,
			StartDate:    time.Time{},
			EndDate:      time.Time{},
		},
		nil,
		constants.ReviewLogsLimit,
	)
	if err != nil {
		m.layout.logger.Error("Failed to fetch review logs", zap.Error(err))

		logs = []*types.ActivityLog{} // Continue without logs - not critical
	}

	// Store logs in session
	session.ReviewLogs.Set(s, logs)
	session.ReviewLogsHasMore.Set(s, nextCursor != nil)

	// Check friend status and get friend data by looking up each friend in the database
	var flaggedFriends map[int64]*types.ReviewUser

	if len(user.Friends) > 0 {
		// Extract friend IDs for batch lookup
		friendIDs := make([]int64, len(user.Friends))
		for i, friend := range user.Friends {
			friendIDs[i] = friend.ID
		}

		// Get full user data and types for friends that exist in the database
		var err error

		flaggedFriends, err = m.layout.db.Model().User().GetUsersByIDs(
			ctx.Context(),
			friendIDs,
			types.UserFieldBasic|types.UserFieldReasons|types.UserFieldConfidence,
		)
		if err != nil {
			m.layout.logger.Error("Failed to get friend data", zap.Error(err))
			return
		}
	}

	// Check group status
	var flaggedGroups map[int64]*types.ReviewGroup

	if len(user.Groups) > 0 {
		// Extract group IDs for batch lookup
		groupIDs := make([]int64, len(user.Groups))
		for i, group := range user.Groups {
			groupIDs[i] = group.Group.ID
		}

		// Get full group data and types
		var err error

		flaggedGroups, err = m.layout.db.Model().Group().GetGroupsByIDs(
			ctx.Context(),
			groupIDs,
			types.GroupFieldBasic|types.GroupFieldReasons|types.GroupFieldConfidence,
		)
		if err != nil {
			m.layout.logger.Error("Failed to get group data", zap.Error(err))
			return
		}
	}

	// Store data in session for the message builder
	session.UserFlaggedFriends.Set(s, flaggedFriends)
	session.UserFlaggedGroups.Set(s, flaggedGroups)

	// Fetch comments for the user
	comments, err := m.layout.db.Model().Comment().GetUserComments(ctx.Context(), user.ID)
	if err != nil {
		m.layout.logger.Error("Failed to fetch user comments", zap.Error(err))

		comments = []*types.Comment{} // Continue without comments - not critical
	}

	session.ReviewComments.Set(s, comments)
}

// handleSelectMenu processes select menu interactions.
func (m *ReviewMenu) handleSelectMenu(ctx *interaction.Context, s *session.Session, customID, option string) {
	if m.CheckCaptchaRequired(ctx, s) {
		return
	}

	switch customID {
	case constants.SortOrderSelectMenuCustomID:
		m.handleSortOrderSelection(ctx, s, option)
	case constants.ActionSelectMenuCustomID:
		m.handleActionSelection(ctx, s, option)
	case constants.ReasonSelectMenuCustomID:
		m.handleReasonSelection(ctx, s, option)
	case constants.AIReasonSelectMenuCustomID:
		m.handleAIReasonSelection(ctx, s, option)
	}
}

// handleSortOrderSelection processes sort order menu selections.
func (m *ReviewMenu) handleSortOrderSelection(ctx *interaction.Context, s *session.Session, option string) {
	// Parse option to review sort
	sortBy, err := enum.ReviewSortByString(option)
	if err != nil {
		m.layout.logger.Error("Failed to parse sort order", zap.Error(err))
		ctx.Error("Failed to parse sort order. Please try again.")

		return
	}

	// Update user's default sort preference
	session.UserUserDefaultSort.Set(s, sortBy)

	ctx.Reload("Changed sort order. Will take effect for the next user.")
}

// handleActionSelection processes action menu selections.
func (m *ReviewMenu) handleActionSelection(ctx *interaction.Context, s *session.Session, option string) {
	userID := uint64(ctx.Event().User().ID)
	isReviewer := s.BotSettings().IsReviewer(userID)

	// Check reviewer-only options
	switch option {
	case constants.ViewUserLogsButtonCustomID,
		constants.ReviewModeOption,
		constants.ViewCommentsButtonCustomID:
		if !isReviewer {
			m.layout.logger.Error("Non-reviewer attempted restricted action",
				zap.Uint64("userID", userID),
				zap.String("action", option))
			ctx.Error("You do not have permission to perform this action.")

			return
		}
	}

	// Process selected option
	switch option {
	case constants.OpenFriendsMenuButtonCustomID:
		session.PaginationPage.Set(s, 0)
		ctx.Show(constants.UserFriendsPageName, "")
	case constants.OpenGroupsMenuButtonCustomID:
		session.PaginationPage.Set(s, 0)
		ctx.Show(constants.UserGroupsPageName, "")
	case constants.OpenOutfitsMenuButtonCustomID:
		session.PaginationPage.Set(s, 0)
		ctx.Show(constants.UserOutfitsPageName, "")
	case constants.CaesarCipherButtonCustomID:
		session.PaginationPage.Set(s, 0)
		ctx.Show(constants.UserCaesarPageName, "")
	case constants.ViewCommentsButtonCustomID:
		session.PaginationPage.Set(s, 0)
		ctx.Show(constants.UserCommentsPageName, "")
	case constants.AddCommentButtonCustomID:
		m.HandleAddComment(ctx, s)
	case constants.DeleteCommentButtonCustomID:
		m.HandleDeleteComment(ctx, s, viewShared.TargetTypeUser)
	case constants.ViewUserLogsButtonCustomID:
		m.handleViewUserLogs(ctx, s)
	case constants.ReviewModeOption:
		session.SettingType.Set(s, constants.UserSettingPrefix)
		session.SettingCustomID.Set(s, constants.ReviewModeOption)
		ctx.Show(constants.SettingUpdatePageName, "")
	case constants.ReviewTargetModeOption:
		session.SettingType.Set(s, constants.UserSettingPrefix)
		session.SettingCustomID.Set(s, constants.ReviewTargetModeOption)
		ctx.Show(constants.SettingUpdatePageName, "")
	}
}

// handleButton processes button clicks.
func (m *ReviewMenu) handleButton(ctx *interaction.Context, s *session.Session, customID string) {
	if m.CheckCaptchaRequired(ctx, s) {
		return
	}

	switch customID {
	case constants.BackButtonCustomID:
		ctx.NavigateBack("")
	case constants.PrevReviewButtonCustomID:
		m.handleNavigateUser(ctx, s, false)
	case constants.NextReviewButtonCustomID:
		m.handleNavigateUser(ctx, s, true)
	case constants.ConfirmButtonCustomID:
		m.handleConfirmUser(ctx, s)
	case constants.ClearButtonCustomID:
		m.handleClearUser(ctx, s)
	}
}

// handleModal handles modal submissions for the review menu.
func (m *ReviewMenu) handleModal(ctx *interaction.Context, s *session.Session) {
	if m.CheckCaptchaRequired(ctx, s) {
		return
	}

	switch ctx.Event().CustomID() {
	case constants.AddReasonModalCustomID:
		user := session.UserTarget.Get(s)
		shared.HandleReasonModalSubmit(
			ctx, s, user.Reasons, enum.UserReasonTypeString,
			func(r types.Reasons[enum.UserReasonType]) {
				user.Reasons = r
				session.UserTarget.Set(s, user)
			},
			func(c float64) {
				user.Confidence = c
				session.UserTarget.Set(s, user)
			},
		)
	case constants.AddCommentModalCustomID:
		m.HandleCommentModalSubmit(ctx, s, viewShared.TargetTypeUser)
	case constants.GenerateProfileReasonModalCustomID:
		m.handleGenerateProfileReasonModalSubmit(ctx, s)
	}
}

// handleViewUserLogs handles the shortcut to view user logs.
func (m *ReviewMenu) handleViewUserLogs(ctx *interaction.Context, s *session.Session) {
	// Get current user
	user := session.UserTarget.Get(s)

	// Set the user ID filter
	log.ResetLogs(s)
	log.ResetFilters(s)
	session.LogFilterUserID.Set(s, user.ID)

	// Show the logs menu
	ctx.Show(constants.LogPageName, "")
}

// handleNavigateUser handles navigation to previous or next user based on the button pressed.
func (m *ReviewMenu) handleNavigateUser(ctx *interaction.Context, s *session.Session, isNext bool) {
	// Get the review history and current index
	history := session.UserReviewHistory.Get(s)
	index := session.UserReviewHistoryIndex.Get(s)

	// If navigating next and we're at the end of history, treat it as a skip
	if isNext && (index >= len(history)-1 || len(history) == 0) {
		// Log the skip action before clearing the user
		user := session.UserTarget.Get(s)
		if user != nil {
			m.layout.db.Model().Activity().Log(ctx.Context(), &types.ActivityLog{
				ActivityTarget: types.ActivityTarget{
					UserID: user.ID,
				},
				ReviewerID:        uint64(ctx.Event().User().ID),
				ActivityType:      enum.ActivityTypeUserSkipped,
				ActivityTimestamp: time.Now(),
				Details:           map[string]any{},
			})
		}

		// Navigate to next user or fetch new one
		m.UpdateCounters(s)
		m.navigateAfterAction(ctx, s, "Skipped user.")

		return
	}

	// For previous navigation or when there's history to navigate
	if isNext {
		if index >= len(history)-1 {
			ctx.Cancel("No next user to navigate to.")
			return
		}

		index++
	} else {
		if index <= 0 || len(history) == 0 {
			ctx.Cancel("No previous user to navigate to.")
			return
		}

		index--
	}

	// Update index in session
	session.UserReviewHistoryIndex.Set(s, index)

	// Fetch the user data
	targetUserID := history[index]

	user, err := m.layout.db.Service().User().GetUserByID(
		ctx.Context(),
		strconv.FormatInt(targetUserID, 10),
		types.UserFieldAll,
	)
	if err != nil {
		if errors.Is(err, types.ErrUserNotFound) {
			// Remove the missing user from history
			session.RemoveFromReviewHistory(s, session.UserReviewHistoryType, index)

			// Try again with updated history
			m.handleNavigateUser(ctx, s, isNext)

			return
		}

		direction := map[bool]string{true: "next", false: "previous"}[isNext]
		m.layout.logger.Error(fmt.Sprintf("Failed to fetch %s user", direction), zap.Error(err))
		ctx.Error(fmt.Sprintf("Failed to load %s user. Please try again.", direction))

		return
	}

	// Set as current user and reload
	session.UserTarget.Set(s, user)
	session.OriginalUserReasons.Set(s, user.Reasons)
	session.UnsavedUserReasons.Delete(s)
	session.ReasonsChanged.Delete(s)

	direction := map[bool]string{true: "next", false: "previous"}[isNext]
	ctx.Reload(fmt.Sprintf("Navigated to %s user.", direction))

	// Log the view action
	m.layout.db.Model().Activity().Log(ctx.Context(), &types.ActivityLog{
		ActivityTarget: types.ActivityTarget{
			UserID: user.ID,
		},
		ReviewerID:        uint64(ctx.Event().User().ID),
		ActivityType:      enum.ActivityTypeUserViewed,
		ActivityTimestamp: time.Now(),
		Details:           map[string]any{},
	})
}

// handleConfirmUser moves a user to the confirmed state and logs the action.
func (m *ReviewMenu) handleConfirmUser(ctx *interaction.Context, s *session.Session) {
	user := session.UserTarget.Get(s)
	reviewerID := uint64(ctx.Event().User().ID)

	// Ensure user is a reviewer
	isReviewer := s.BotSettings().IsReviewer(reviewerID)
	if !isReviewer {
		m.layout.logger.Error("Non-reviewer attempted to confirm user",
			zap.Uint64("userID", reviewerID))
		ctx.Error("You do not have permission to confirm users.")

		return
	}

	// Re-classify category if reasons have been modified
	if session.ReasonsChanged.Get(s) {
		// Prepare user map for classification
		usersToClassify := map[int64]*types.ReviewUser{user.ID: user}

		// Call category analyzer
		categoryResults := m.layout.categoryAnalyzer.ClassifyUsers(ctx.Context(), usersToClassify, 0)

		// Update user category if classification was successful
		if category, exists := categoryResults[user.ID]; exists {
			oldCategory := user.Category
			user.Category = category

			m.layout.logger.Info("Re-classified user category",
				zap.Int64("userID", user.ID),
				zap.String("username", user.Name),
				zap.String("oldCategory", oldCategory.String()),
				zap.String("newCategory", category.String()))
		} else {
			m.layout.logger.Warn("Failed to re-classify user category, keeping existing category",
				zap.Int64("userID", user.ID),
				zap.String("username", user.Name))
		}
	}

	// Confirm the user
	if err := m.layout.db.Service().User().ConfirmUser(ctx.Context(), user, reviewerID); err != nil {
		m.layout.logger.Error("Failed to confirm user", zap.Error(err))
		ctx.Error("Failed to confirm the user. Please try again.")

		return
	}

	// Get the number of flagged users left to review
	flaggedCount, err := m.layout.db.Model().User().GetFlaggedUsersCount(ctx.Context())
	if err != nil {
		m.layout.logger.Error("Failed to get flagged users count", zap.Error(err))
	}

	// Navigate to next user in history or fetch new one
	m.UpdateCounters(s)
	m.navigateAfterAction(ctx, s, fmt.Sprintf("User confirmed. %d users left to review.", flaggedCount))

	// Add or update the user in the D1 database
	if err := m.layout.cfClient.UserFlags.AddConfirmed(ctx.Context(), user, reviewerID); err != nil {
		m.layout.logger.Error("Failed to add confirmed user to D1 database",
			zap.Error(err),
			zap.Int64("userID", user.ID),
			zap.Uint64("reviewerID", reviewerID))
	}

	// Log the confirm action
	m.layout.db.Model().Activity().Log(ctx.Context(), &types.ActivityLog{
		ActivityTarget: types.ActivityTarget{
			UserID: user.ID,
		},
		ReviewerID:        reviewerID,
		ActivityType:      enum.ActivityTypeUserConfirmed,
		ActivityTimestamp: time.Now(),
		Details: map[string]any{
			"reasons":    user.Reasons.Messages(),
			"confidence": user.Confidence,
		},
	})
}

// handleClearUser removes a user from the flagged state and logs the action.
func (m *ReviewMenu) handleClearUser(ctx *interaction.Context, s *session.Session) {
	user := session.UserTarget.Get(s)
	reviewerID := uint64(ctx.Event().User().ID)

	// Ensure user is a reviewer
	isReviewer := s.BotSettings().IsReviewer(reviewerID)
	if !isReviewer {
		m.layout.logger.Error("Non-reviewer attempted to clear user",
			zap.Uint64("userID", reviewerID))
		ctx.Error("You do not have permission to clear users.")

		return
	}

	// Clear the user
	if err := m.layout.db.Service().User().ClearUser(ctx.Context(), user, reviewerID); err != nil {
		m.layout.logger.Error("Failed to clear user", zap.Error(err))
		ctx.Error("Failed to clear the user. Please try again.")

		return
	}

	// Get the number of flagged users left to review
	flaggedCount, err := m.layout.db.Model().User().GetFlaggedUsersCount(ctx.Context())
	if err != nil {
		m.layout.logger.Error("Failed to get flagged users count", zap.Error(err))
	}

	// Navigate to next user in history or fetch new one
	m.UpdateCounters(s)
	m.navigateAfterAction(ctx, s, fmt.Sprintf("User cleared. %d users left to review.", flaggedCount))

	// Remove the user from the D1 database
	if err := m.layout.cfClient.UserFlags.Remove(ctx.Context(), user.ID); err != nil {
		m.layout.logger.Error("Failed to remove cleared user from D1 database",
			zap.Error(err),
			zap.Int64("userID", user.ID))
	}

	// Log the clear action
	m.layout.db.Model().Activity().Log(ctx.Context(), &types.ActivityLog{
		ActivityTarget: types.ActivityTarget{
			UserID: user.ID,
		},
		ReviewerID:        reviewerID,
		ActivityType:      enum.ActivityTypeUserCleared,
		ActivityTimestamp: time.Now(),
		Details:           map[string]any{},
	})
}

// navigateAfterAction handles navigation after confirming or clearing a user.
// It tries to navigate to the next user in history, or fetches a new user if at the end.
func (m *ReviewMenu) navigateAfterAction(ctx *interaction.Context, s *session.Session, message string) {
	// Get the review history and current index
	history := session.UserReviewHistory.Get(s)
	index := session.UserReviewHistoryIndex.Get(s)

	// Clear current user data
	session.UserTarget.Delete(s)
	session.UnsavedUserReasons.Delete(s)

	// Check if there's a next user in history
	if index < len(history)-1 {
		// Navigate to next user in history
		index++
		session.UserReviewHistoryIndex.Set(s, index)

		// Fetch the user data
		targetUserID := history[index]

		user, err := m.layout.db.Service().User().GetUserByID(
			ctx.Context(),
			strconv.FormatInt(targetUserID, 10),
			types.UserFieldAll,
		)
		if err != nil {
			if errors.Is(err, types.ErrUserNotFound) {
				// Remove the missing user from history and try again
				session.RemoveFromReviewHistory(s, session.UserReviewHistoryType, index)

				// Try navigating again
				m.navigateAfterAction(ctx, s, message)

				return
			}

			m.layout.logger.Error("Failed to fetch next user from history", zap.Error(err))
			ctx.Error("Failed to load next user. Please try again.")

			return
		}

		// Set as current user and reload
		session.UserTarget.Set(s, user)
		session.OriginalUserReasons.Set(s, user.Reasons)
		session.UnsavedUserReasons.Delete(s)
		session.ReasonsChanged.Delete(s)

		ctx.Reload(message)

		// Log the view action
		m.layout.db.Model().Activity().Log(ctx.Context(), &types.ActivityLog{
			ActivityTarget: types.ActivityTarget{
				UserID: user.ID,
			},
			ReviewerID:        uint64(ctx.Event().User().ID),
			ActivityType:      enum.ActivityTypeUserViewed,
			ActivityTimestamp: time.Now(),
			Details:           map[string]any{},
		})

		return
	}

	// No next user in history, reload to fetch a new one
	ctx.Reload(message)
}

// handleReasonSelection processes reason management dropdown selections.
func (m *ReviewMenu) handleReasonSelection(ctx *interaction.Context, s *session.Session, option string) {
	// Check if user is a reviewer
	if !s.BotSettings().IsReviewer(uint64(ctx.Event().User().ID)) {
		m.layout.logger.Error("Non-reviewer attempted to manage reasons",
			zap.Uint64("userID", uint64(ctx.Event().User().ID)))
		ctx.Error("You do not have permission to manage reasons.")

		return
	}

	// Get current user
	user := session.UserTarget.Get(s)

	// Handle refresh option
	if option == constants.RefreshButtonCustomID {
		// Restore original reasons
		originalReasons := session.OriginalUserReasons.Get(s)
		user.Reasons = originalReasons
		user.Confidence = utils.CalculateConfidence(user.Reasons)

		// Update session
		session.UserTarget.Set(s, user)
		session.UnsavedUserReasons.Delete(s)
		session.ReasonsChanged.Delete(s)

		ctx.Reload("Successfully restored original reasons")

		return
	}

	// Parse reason type
	option = strings.TrimSuffix(option, constants.ModalOpenSuffix)

	reasonType, err := enum.UserReasonTypeString(option)
	if err != nil {
		ctx.Error("Invalid reason type: " + option)
		return
	}

	shared.HandleEditReason(
		ctx,
		s,
		m.layout.logger,
		reasonType,
		user.Reasons,
		func(r types.Reasons[enum.UserReasonType]) {
			user.Reasons = r
			session.UserTarget.Set(s, user)
		},
	)
}

// handleAIReasonSelection processes AI reason generation dropdown selections.
func (m *ReviewMenu) handleAIReasonSelection(ctx *interaction.Context, s *session.Session, option string) {
	// Check if user is a reviewer
	if !s.BotSettings().IsReviewer(uint64(ctx.Event().User().ID)) {
		m.layout.logger.Error("Non-reviewer attempted to generate AI reasons",
			zap.Uint64("userID", uint64(ctx.Event().User().ID)))
		ctx.Error("You do not have permission to generate AI reasons.")

		return
	}

	// Get current user
	user := session.UserTarget.Get(s)
	if user == nil {
		ctx.Error("No user selected for AI reason generation.")
		return
	}

	// Handle the specific AI reason generation option
	switch option {
	case constants.GenerateProfileReasonButtonCustomID:
		m.handleGenerateProfileReason(ctx)
	case constants.GenerateFriendReasonButtonCustomID:
		m.handleGenerateFriendReason(ctx, s, user)
	case constants.GenerateGroupReasonButtonCustomID:
		m.handleGenerateGroupReason(ctx, s, user)
	default:
		ctx.Error("Unknown AI reason generation option: " + option)
	}
}

// handleGenerateFriendReason generates an AI generated friend reason for the current user.
func (m *ReviewMenu) handleGenerateFriendReason(ctx *interaction.Context, s *session.Session, user *types.ReviewUser) {
	// Check if user has friends to analyze
	if len(user.Friends) == 0 {
		ctx.Cancel("This user has no friends to analyze for friend reason generation.")
		return
	}

	// Get flagged friends data
	flaggedFriends := session.UserFlaggedFriends.Get(s)
	if len(flaggedFriends) == 0 {
		ctx.Cancel("This user has no flagged friends to analyze for reason generation.")
		return
	}

	// Show loading message
	ctx.Clear("Generating friend reason using AI... This may take a few moments.")

	// Prepare data for AI analysis
	confirmedFriendsMap := make(map[int64]map[int64]*types.ReviewUser)
	flaggedFriendsMap := make(map[int64]map[int64]*types.ReviewUser)

	confirmedFriends := make(map[int64]*types.ReviewUser)
	flaggedFriendsForUser := make(map[int64]*types.ReviewUser)

	for _, friend := range flaggedFriends {
		switch friend.Status {
		case enum.UserTypeConfirmed:
			confirmedFriends[friend.ID] = friend
		case enum.UserTypeFlagged:
			flaggedFriendsForUser[friend.ID] = friend
		case enum.UserTypeCleared:
			// Cleared users are not included in friend analysis
		case enum.UserTypeQueued, enum.UserTypeBloxDB, enum.UserTypeMixed, enum.UserTypePastOffender:
			// These statuses are not relevant for friend analysis
		}
	}

	confirmedFriendsMap[user.ID] = confirmedFriends
	flaggedFriendsMap[user.ID] = flaggedFriendsForUser

	// Generate AI reason using the friend analyzer
	reasons := m.layout.friendReasonAnalyzer.GenerateFriendReasons(
		ctx.Context(), []*types.ReviewUser{user}, confirmedFriendsMap, flaggedFriendsMap,
	)

	// Get the generated reason
	generatedReason, exists := reasons[user.ID]
	if !exists || generatedReason == "" {
		ctx.Error("Failed to generate friend reason. The AI could not analyze this user's friend network.")
		return
	}

	// Initialize reasons map if nil
	if user.Reasons == nil {
		user.Reasons = make(types.Reasons[enum.UserReasonType])
	}

	// Add or replace the friend reason with a confidence of 0.8
	user.Reasons[enum.UserReasonTypeFriend] = &types.Reason{
		Message:    generatedReason,
		Confidence: 1.0,
		Evidence:   []string{},
	}

	// Recalculate overall confidence
	user.Confidence = utils.CalculateConfidence(user.Reasons)

	// Update session
	session.UserTarget.Set(s, user)
	session.ReasonsChanged.Set(s, true)

	// Mark the friend reason as unsaved
	unsavedReasons := session.UnsavedUserReasons.Get(s)
	if unsavedReasons == nil {
		unsavedReasons = make(map[enum.UserReasonType]struct{})
	}

	unsavedReasons[enum.UserReasonTypeFriend] = struct{}{}
	session.UnsavedUserReasons.Set(s, unsavedReasons)

	ctx.Reload("Friend reason generated and applied successfully!")
}

// handleGenerateGroupReason generates an AI generated group reason for the current user.
func (m *ReviewMenu) handleGenerateGroupReason(ctx *interaction.Context, s *session.Session, user *types.ReviewUser) {
	// Check if user has groups to analyze
	if len(user.Groups) == 0 {
		ctx.Cancel("This user has no groups to analyze for group reason generation.")
		return
	}

	// Get flagged groups data
	flaggedGroups := session.UserFlaggedGroups.Get(s)
	if len(flaggedGroups) == 0 {
		ctx.Cancel("This user has no flagged groups to analyze for reason generation.")
		return
	}

	// Show loading message
	ctx.Clear("Generating group reason using AI... This may take a few moments.")

	// Prepare data for AI analysis
	confirmedGroupsMap := make(map[int64]map[int64]*types.ReviewGroup)
	flaggedGroupsMap := make(map[int64]map[int64]*types.ReviewGroup)
	mixedGroupsMap := make(map[int64]map[int64]*types.ReviewGroup)

	confirmedGroups := make(map[int64]*types.ReviewGroup)
	flaggedGroupsForUser := make(map[int64]*types.ReviewGroup)
	mixedGroupsForUser := make(map[int64]*types.ReviewGroup)

	for _, group := range flaggedGroups {
		switch group.Status {
		case enum.GroupTypeConfirmed:
			confirmedGroups[group.ID] = group
		case enum.GroupTypeFlagged:
			flaggedGroupsForUser[group.ID] = group
		case enum.GroupTypeMixed:
			mixedGroupsForUser[group.ID] = group
		}
	}

	confirmedGroupsMap[user.ID] = confirmedGroups
	flaggedGroupsMap[user.ID] = flaggedGroupsForUser
	mixedGroupsMap[user.ID] = mixedGroupsForUser

	// Generate AI reason using the group analyzer
	reasons := m.layout.groupReasonAnalyzer.GenerateGroupReasons(
		ctx.Context(), []*types.ReviewUser{user}, confirmedGroupsMap, flaggedGroupsMap, mixedGroupsMap,
	)

	// Get the generated reason
	generatedReason, exists := reasons[user.ID]
	if !exists || generatedReason == "" {
		ctx.Error("Failed to generate group reason. The AI could not analyze this user's group memberships.")
		return
	}

	// Initialize reasons map if nil
	if user.Reasons == nil {
		user.Reasons = make(types.Reasons[enum.UserReasonType])
	}

	// Add or replace the group reason with a confidence of 0.8
	user.Reasons[enum.UserReasonTypeGroup] = &types.Reason{
		Message:    generatedReason,
		Confidence: 1.0,
		Evidence:   []string{},
	}

	// Recalculate overall confidence
	user.Confidence = utils.CalculateConfidence(user.Reasons)

	// Update session
	session.UserTarget.Set(s, user)
	session.ReasonsChanged.Set(s, true)

	// Mark the group reason as unsaved
	unsavedReasons := session.UnsavedUserReasons.Get(s)
	if unsavedReasons == nil {
		unsavedReasons = make(map[enum.UserReasonType]struct{})
	}

	unsavedReasons[enum.UserReasonTypeGroup] = struct{}{}
	session.UnsavedUserReasons.Set(s, unsavedReasons)

	ctx.Reload("Group reason generated and applied successfully!")
}

// fetchNewTarget gets a new user to review based on the current sort order.
func (m *ReviewMenu) fetchNewTarget(ctx *interaction.Context, s *session.Session) (*types.ReviewUser, error) {
	if m.CheckBreakRequired(ctx, s) {
		return nil, ErrBreakRequired
	}

	// Get the next user to review
	reviewerID := uint64(ctx.Event().User().ID)
	defaultSort := session.UserUserDefaultSort.Get(s)
	reviewTargetMode := session.UserReviewTargetMode.Get(s)

	user, err := m.layout.db.Service().User().GetUserToReview(
		ctx.Context(), defaultSort, reviewTargetMode, reviewerID,
	)
	if err != nil {
		return nil, err
	}

	// Process friends and groups to update reasons
	reasonsMap := make(map[int64]types.Reasons[enum.UserReasonType])
	if len(user.Reasons) > 0 {
		reasonsMap[user.ID] = user.Reasons
	}

	// Create a slice with just this user for processing
	userSlice := []*types.ReviewUser{user}

	// Prepare maps for processing
	confirmedFriendsMap, flaggedFriendsMap := m.layout.friendChecker.PrepareFriendMaps(ctx.Context(), userSlice)
	confirmedGroupsMap, flaggedGroupsMap, mixedGroupsMap := m.layout.groupChecker.PrepareGroupMaps(ctx.Context(), userSlice)

	// Process friends if friend reason doesn't exist
	if _, hasFriendReason := user.Reasons[enum.UserReasonTypeFriend]; !hasFriendReason {
		m.layout.friendChecker.ProcessUsers(ctx.Context(), &checker.FriendCheckerParams{
			Users:                     userSlice,
			ReasonsMap:                reasonsMap,
			ConfirmedFriendsMap:       confirmedFriendsMap,
			FlaggedFriendsMap:         flaggedFriendsMap,
			ConfirmedGroupsMap:        confirmedGroupsMap,
			FlaggedGroupsMap:          flaggedGroupsMap,
			InappropriateFriendsFlags: nil,
		})
	}

	// Process groups if group reason doesn't exist
	if _, hasGroupReason := user.Reasons[enum.UserReasonTypeGroup]; !hasGroupReason {
		m.layout.groupChecker.ProcessUsers(ctx.Context(), &checker.GroupCheckerParams{
			Users:                    userSlice,
			ReasonsMap:               reasonsMap,
			ConfirmedFriendsMap:      confirmedFriendsMap,
			FlaggedFriendsMap:        flaggedFriendsMap,
			ConfirmedGroupsMap:       confirmedGroupsMap,
			FlaggedGroupsMap:         flaggedGroupsMap,
			MixedGroupsMap:           mixedGroupsMap,
			InappropriateGroupsFlags: nil,
		})
	}

	// Update user with any new reasons from friend/group checking
	if reasons, ok := reasonsMap[user.ID]; ok {
		user.Reasons = reasons
		user.Confidence = utils.CalculateConfidence(reasons)

		// Save updated user to database
		flaggedUsers := map[int64]*types.ReviewUser{user.ID: user}
		if err := m.layout.db.Service().User().SaveUsers(ctx.Context(), flaggedUsers); err != nil {
			m.layout.logger.Error("Failed to save updated user reasons", zap.Error(err))
		}
	}

	// Add current user to history and set index to point to it
	session.AddToReviewHistory(s, session.UserReviewHistoryType, user.ID)

	// Store user in session and show review menu
	session.UserTarget.Set(s, user)
	session.OriginalUserReasons.Set(s, user.Reasons)
	session.ReasonsChanged.Set(s, false)

	// Clear unsaved reasons tracking for new user
	session.UnsavedUserReasons.Set(s, make(map[enum.UserReasonType]struct{}))

	// Log the view action
	go m.layout.db.Model().Activity().Log(ctx.Context(), &types.ActivityLog{
		ActivityTarget: types.ActivityTarget{
			UserID: user.ID,
		},
		ReviewerID:        reviewerID,
		ActivityType:      enum.ActivityTypeUserViewed,
		ActivityTimestamp: time.Now(),
		Details:           map[string]any{},
	})

	return user, nil
}

// handleGenerateProfileReason shows the modal for profile reason generation parameters.
func (m *ReviewMenu) handleGenerateProfileReason(ctx *interaction.Context) {
	// Create modal for profile reason generation parameters
	modal := discord.NewModalCreateBuilder().
		SetCustomID(constants.GenerateProfileReasonModalCustomID).
		SetTitle("Generate Profile Reason").
		AddLabel(
			"Hint",
			discord.NewTextInput(constants.ProfileReasonHintInputCustomID, discord.TextInputStyleParagraph).
				WithRequired(true).
				WithMinLength(1).
				WithMaxLength(256).
				WithPlaceholder("e.g., inappropriate username, sexual content in description"),
		).
		AddLabel(
			"Flagged Fields",
			discord.NewTextInput(constants.ProfileReasonFlaggedFieldsInputCustomID, discord.TextInputStyleParagraph).
				WithRequired(false).
				WithMaxLength(256).
				WithPlaceholder("username\ndisplayName\ndescription"),
		).
		AddLabel(
			"Language Used",
			discord.NewTextInput(constants.ProfileReasonLanguageUsedInputCustomID, discord.TextInputStyleParagraph).
				WithRequired(false).
				WithMaxLength(256).
				WithPlaceholder("english\nrot13\ncaesar\nleetspeak"),
		)

	ctx.Modal(modal)
}

// handleGenerateProfileReasonModalSubmit processes the profile reason generation modal submission.
func (m *ReviewMenu) handleGenerateProfileReasonModalSubmit(ctx *interaction.Context, s *session.Session) {
	// Get the current user
	user := session.UserTarget.Get(s)
	if user == nil {
		ctx.Error("No user selected for profile reason generation.")
		return
	}

	// Get modal data
	data := ctx.Event().ModalData()
	hint := data.Text(constants.ProfileReasonHintInputCustomID)
	flaggedFieldsText := data.Text(constants.ProfileReasonFlaggedFieldsInputCustomID)
	languageUsedText := data.Text(constants.ProfileReasonLanguageUsedInputCustomID)

	// Validate required fields
	if hint == "" {
		ctx.Cancel("Hint is required for profile reason generation.")
		return
	}

	// Parse input fields
	flaggedFields := utils.ParseDelimitedInput(flaggedFieldsText, "\n")

	// Convert flagged fields to boolean flags
	var hasUsernameViolation, hasDisplayNameViolation, hasDescriptionViolation bool

	for _, field := range flaggedFields {
		switch field {
		case "username":
			hasUsernameViolation = true
		case "displayName":
			hasDisplayNameViolation = true
		case "description":
			hasDescriptionViolation = true
		}
	}

	// Show loading message
	ctx.Clear("Generating profile reason using AI... This may take a few moments.")

	// Create user summary for analysis
	userSummary := &ai.UserSummary{
		Name: user.Name,
	}

	// Only include display name if it's different from the username
	if user.DisplayName != user.Name {
		userSummary.DisplayName = user.DisplayName
	}

	// Replace empty descriptions with placeholder
	description := user.Description
	if description == "" {
		description = "No description"
	}

	userSummary.Description = description

	// Create user reason request
	userReasonRequest := map[int64]ai.UserReasonRequest{
		user.ID: {
			User:                    userSummary,
			Confidence:              1.0,
			Hint:                    hint,
			HasUsernameViolation:    hasUsernameViolation,
			HasDisplayNameViolation: hasDisplayNameViolation,
			HasDescriptionViolation: hasDescriptionViolation,
			LanguageUsed:            languageUsedText,
			UserID:                  user.ID,
		},
	}

	// Create translated and original info maps
	translatedInfos := map[string]*types.ReviewUser{user.Name: user}
	originalInfos := map[string]*types.ReviewUser{user.Name: user}

	reasonsMap := make(map[int64]types.Reasons[enum.UserReasonType])
	if len(user.Reasons) > 0 {
		reasonsMap[user.ID] = user.Reasons
	}

	// Generate detailed reason using the user reason analyzer
	m.layout.userReasonAnalyzer.ProcessFlaggedUsers(
		ctx.Context(), userReasonRequest, translatedInfos, originalInfos, reasonsMap, 0,
	)

	// Check if a reason was generated
	if updatedReasons, exists := reasonsMap[user.ID]; exists {
		if profileReason, hasProfileReason := updatedReasons[enum.UserReasonTypeProfile]; hasProfileReason {
			// Initialize reasons map if nil
			if user.Reasons == nil {
				user.Reasons = make(types.Reasons[enum.UserReasonType])
			}

			// Add or replace the profile reason
			user.Reasons[enum.UserReasonTypeProfile] = profileReason

			// Recalculate overall confidence
			user.Confidence = utils.CalculateConfidence(user.Reasons)

			// Update session
			session.UserTarget.Set(s, user)
			session.ReasonsChanged.Set(s, true)

			// Mark the profile reason as unsaved
			unsavedReasons := session.UnsavedUserReasons.Get(s)
			if unsavedReasons == nil {
				unsavedReasons = make(map[enum.UserReasonType]struct{})
			}

			unsavedReasons[enum.UserReasonTypeProfile] = struct{}{}
			session.UnsavedUserReasons.Set(s, unsavedReasons)

			ctx.Reload("Profile reason generated and applied successfully!")

			return
		}
	}

	ctx.Error("Failed to generate profile reason. The AI could not analyze this user's profile content with the provided parameters.")
}
