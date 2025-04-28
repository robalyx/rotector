package user

import (
	"errors"
	"fmt"
	"slices"
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
			return view.NewReviewBuilder(s, layout.translator, layout.db).Build()
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
		var isBanned bool
		var err error
		user, isBanned, err = m.fetchNewTarget(ctx, s)
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

		if isBanned {
			ctx.Show(constants.BanPageName, "You have been banned for suspicious voting patterns.")
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
	var flaggedFriends map[uint64]*types.ReviewUser
	if len(user.Friends) > 0 {
		// Extract friend IDs for batch lookup
		friendIDs := make([]uint64, len(user.Friends))
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
	var flaggedGroups map[uint64]*types.ReviewGroup
	if len(user.Groups) > 0 {
		// Extract group IDs for batch lookup
		groupIDs := make([]uint64, len(user.Groups))
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
	case constants.OpenAIChatButtonCustomID,
		constants.ViewUserLogsButtonCustomID,
		constants.ReviewModeOption,
		constants.ViewCommentsButtonCustomID:
		if !isReviewer {
			m.layout.logger.Error("Non-reviewer attempted restricted action",
				zap.Uint64("user_id", userID),
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
	case constants.OpenAIChatButtonCustomID:
		m.handleOpenAIChat(ctx, s)
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
		m.handleReasonModalSubmit(ctx, s)
	case constants.AddCommentModalCustomID:
		m.HandleCommentModalSubmit(ctx, s, viewShared.TargetTypeUser)
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
		// Clear current user and load next one
		m.UpdateCounters(s)
		session.UserTarget.Delete(s)
		ctx.Reload("Skipped user.")

		// Log the skip action
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
		strconv.FormatUint(targetUserID, 10),
		types.UserFieldAll,
	)
	if err != nil {
		if errors.Is(err, types.ErrUserNotFound) {
			// Remove the missing user from history
			history = slices.Delete(history, index, index+1)
			session.UserReviewHistory.Set(s, history)

			// Adjust index if needed
			if index >= len(history) {
				index = len(history) - 1
			}
			session.UserReviewHistoryIndex.Set(s, index)

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
	session.ReasonsChanged.Set(s, false)

	// Log the view action
	go m.layout.db.Model().Activity().Log(ctx.Context(), &types.ActivityLog{
		ActivityTarget: types.ActivityTarget{
			UserID: user.ID,
		},
		ReviewerID:        uint64(ctx.Event().User().ID),
		ActivityType:      enum.ActivityTypeUserViewed,
		ActivityTimestamp: time.Now(),
		Details:           map[string]any{},
	})

	direction := map[bool]string{true: "next", false: "previous"}[isNext]
	ctx.Reload(fmt.Sprintf("Navigated to %s user.", direction))
}

// handleConfirmUser moves a user to the confirmed state and logs the action.
func (m *ReviewMenu) handleConfirmUser(ctx *interaction.Context, s *session.Session) {
	user := session.UserTarget.Get(s)
	reviewerID := uint64(ctx.Event().User().ID)

	var actionMsg string
	if session.UserReviewMode.Get(s) == enum.ReviewModeTraining {
		// Training mode - increment downvotes
		if err := m.layout.db.Service().Reputation().UpdateUserVotes(ctx.Context(), user.ID, reviewerID, false); err != nil {
			m.layout.logger.Error("Failed to update downvotes", zap.Error(err))
			ctx.Error("Failed to update downvotes. Please try again.")
			return
		}
		user.Reputation.Downvotes++
		actionMsg = "downvoted"

		// Log the training downvote action
		go m.layout.db.Model().Activity().Log(ctx.Context(), &types.ActivityLog{
			ActivityTarget: types.ActivityTarget{
				UserID: user.ID,
			},
			ReviewerID:        reviewerID,
			ActivityType:      enum.ActivityTypeUserTrainingDownvote,
			ActivityTimestamp: time.Now(),
			Details: map[string]any{
				"upvotes":   user.Reputation.Upvotes,
				"downvotes": user.Reputation.Downvotes,
			},
		})
	} else {
		// Standard mode - check permissions and confirm user
		if !s.BotSettings().IsReviewer(reviewerID) {
			m.layout.logger.Error("Non-reviewer attempted to confirm user",
				zap.Uint64("user_id", reviewerID))
			ctx.Error("You do not have permission to confirm users.")
			return
		}

		// Calculate vote percentages
		totalVotes := float64(user.Reputation.Upvotes + user.Reputation.Downvotes)
		if totalVotes >= constants.MinimumVotesRequired {
			upvotePercentage := float64(user.Reputation.Upvotes) / totalVotes

			// If there's a strong consensus for clearing, prevent confirmation
			if upvotePercentage >= constants.VoteConsensusThreshold {
				ctx.Cancel(fmt.Sprintf("Cannot confirm - %.0f%% of %d votes indicate this user is safe",
					upvotePercentage*100, int(totalVotes)))
				return
			}
		}

		// Confirm the user
		if err := m.layout.db.Service().User().ConfirmUser(ctx.Context(), user, reviewerID); err != nil {
			m.layout.logger.Error("Failed to confirm user", zap.Error(err))
			ctx.Error("Failed to confirm the user. Please try again.")
			return
		}
		actionMsg = "confirmed"

		// Log reason changes if any were made
		if session.ReasonsChanged.Get(s) {
			originalReasons := session.OriginalUserReasons.Get(s)
			go m.layout.db.Model().Activity().Log(ctx.Context(), &types.ActivityLog{
				ActivityTarget: types.ActivityTarget{
					UserID: user.ID,
				},
				ReviewerID:        reviewerID,
				ActivityType:      enum.ActivityTypeUserReasonUpdated,
				ActivityTimestamp: time.Now(),
				Details: map[string]any{
					"originalReasons": originalReasons.Messages(),
					"updatedReasons":  user.Reasons.Messages(),
				},
			})
		}

		// Log the confirm action
		go m.layout.db.Model().Activity().Log(ctx.Context(), &types.ActivityLog{
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

	// Get the number of flagged users left to review
	flaggedCount, err := m.layout.db.Model().User().GetFlaggedUsersCount(ctx.Context())
	if err != nil {
		m.layout.logger.Error("Failed to get flagged users count", zap.Error(err))
	}

	// Clear current user and load next one
	m.UpdateCounters(s)
	session.UserTarget.Delete(s)
	ctx.Reload(fmt.Sprintf("User %s. %d users left to review.", actionMsg, flaggedCount))
}

// handleClearUser removes a user from the flagged state and logs the action.
func (m *ReviewMenu) handleClearUser(ctx *interaction.Context, s *session.Session) {
	user := session.UserTarget.Get(s)
	reviewerID := uint64(ctx.Event().User().ID)

	var actionMsg string
	if session.UserReviewMode.Get(s) == enum.ReviewModeTraining {
		// Training mode - increment upvotes
		if err := m.layout.db.Service().Reputation().UpdateUserVotes(ctx.Context(), user.ID, reviewerID, true); err != nil {
			m.layout.logger.Error("Failed to update upvotes", zap.Error(err))
			ctx.Error("Failed to update upvotes. Please try again.")
			return
		}
		user.Reputation.Upvotes++
		actionMsg = "upvoted"

		// Log the training upvote action
		go m.layout.db.Model().Activity().Log(ctx.Context(), &types.ActivityLog{
			ActivityTarget: types.ActivityTarget{
				UserID: user.ID,
			},
			ReviewerID:        reviewerID,
			ActivityType:      enum.ActivityTypeUserTrainingUpvote,
			ActivityTimestamp: time.Now(),
			Details: map[string]any{
				"upvotes":   user.Reputation.Upvotes,
				"downvotes": user.Reputation.Downvotes,
			},
		})
	} else {
		// Standard mode - check permissions and clear user
		if !s.BotSettings().IsReviewer(reviewerID) {
			m.layout.logger.Error("Non-reviewer attempted to clear user",
				zap.Uint64("user_id", reviewerID))
			ctx.Error("You do not have permission to clear users.")
			return
		}

		// Calculate vote percentages
		totalVotes := float64(user.Reputation.Upvotes + user.Reputation.Downvotes)
		if totalVotes >= constants.MinimumVotesRequired {
			downvotePercentage := float64(user.Reputation.Downvotes) / totalVotes

			// If there's a strong consensus for confirming, prevent clearing
			if downvotePercentage >= constants.VoteConsensusThreshold {
				ctx.Cancel(fmt.Sprintf("Cannot clear - %.0f%% of %d votes indicate this user is suspicious",
					downvotePercentage*100, int(totalVotes)))
				return
			}
		}

		// Log reason changes if any were made
		if session.ReasonsChanged.Get(s) {
			originalReasons := session.OriginalUserReasons.Get(s)
			go m.layout.db.Model().Activity().Log(ctx.Context(), &types.ActivityLog{
				ActivityTarget: types.ActivityTarget{
					UserID: user.ID,
				},
				ReviewerID:        reviewerID,
				ActivityType:      enum.ActivityTypeUserReasonUpdated,
				ActivityTimestamp: time.Now(),
				Details: map[string]any{
					"originalReasons": originalReasons.Messages(),
					"updatedReasons":  user.Reasons.Messages(),
				},
			})
		}

		// Clear the user
		if err := m.layout.db.Service().User().ClearUser(ctx.Context(), user, reviewerID); err != nil {
			m.layout.logger.Error("Failed to clear user", zap.Error(err))
			ctx.Error("Failed to clear the user. Please try again.")
			return
		}
		actionMsg = "cleared"

		// Remove user from group tracking
		go m.layout.db.Model().Tracking().RemoveUserFromGroups(ctx.Context(), user.ID, user.Groups)

		// Log the clear action
		go m.layout.db.Model().Activity().Log(ctx.Context(), &types.ActivityLog{
			ActivityTarget: types.ActivityTarget{
				UserID: user.ID,
			},
			ReviewerID:        reviewerID,
			ActivityType:      enum.ActivityTypeUserCleared,
			ActivityTimestamp: time.Now(),
			Details:           map[string]any{},
		})
	}

	// Get the number of flagged users left to review
	flaggedCount, err := m.layout.db.Model().User().GetFlaggedUsersCount(ctx.Context())
	if err != nil {
		m.layout.logger.Error("Failed to get flagged users count", zap.Error(err))
	}

	// Clear current user and load next one
	m.UpdateCounters(s)
	session.UserTarget.Delete(s)
	ctx.Reload(fmt.Sprintf("User %s. %d users left to review.", actionMsg, flaggedCount))
}

// handleOpenAIChat handles the button to open the AI chat for the current user.
func (m *ReviewMenu) handleOpenAIChat(ctx *interaction.Context, s *session.Session) {
	user := session.UserTarget.Get(s)
	flaggedFriends := session.UserFlaggedFriends.Get(s)
	flaggedGroups := session.UserFlaggedGroups.Get(s)

	limit := 20

	// Get translated description
	description := user.Description
	var translatedDescription string
	if description != "" {
		translated, err := m.layout.translator.Translate(ctx.Context(), description, "auto", "en")
		if err == nil && translated != description {
			translatedDescription = translated
		}
	}

	// Build flagged friends information
	friendsInfo := make([]string, 0)
	flaggedFriendsCount := len(flaggedFriends)
	shownFriends := 0

	for _, friend := range user.Friends {
		if flagged := flaggedFriends[friend.ID]; flagged != nil {
			if shownFriends >= limit {
				break
			}
			messages := flagged.Reasons.Messages()
			friendsInfo = append(friendsInfo, fmt.Sprintf("- %s (ID: %d) | Status: %s | Reasons: %s | Confidence: %.2f",
				friend.Name, friend.ID, flagged.Status.String(), strings.Join(messages, "; "), flagged.Confidence))
			shownFriends++
		}
	}

	// Build flagged groups information
	groupsInfo := make([]string, 0)
	flaggedGroupsCount := len(flaggedGroups)
	shownGroups := 0

	for _, group := range user.Groups {
		if flagged := flaggedGroups[group.Group.ID]; flagged != nil {
			if shownGroups >= limit {
				break
			}
			messages := flagged.Reasons.Messages()
			groupsInfo = append(groupsInfo, fmt.Sprintf("- %s (ID: %d) | Role: %s | Status: %s | Reasons: %s | Confidence: %.2f",
				group.Group.Name, group.Group.ID, group.Role.Name, flagged.Status.String(), strings.Join(messages, "; "), flagged.Confidence))
			shownGroups++
		}
	}

	// Build outfits information
	outfitsInfo := make([]string, 0)
	for i, outfit := range user.Outfits {
		if i >= limit {
			break
		}
		outfitsInfo = append(outfitsInfo, fmt.Sprintf("- %s (ID: %d)", outfit.Name, outfit.ID))
	}

	// Build games information
	gamesInfo := make([]string, 0)
	for i, game := range user.Games {
		if i >= limit {
			break
		}
		gamesInfo = append(gamesInfo, fmt.Sprintf("- %s (ID: %d) | Visits: %d",
			game.Name, game.ID, game.PlaceVisits))
	}

	userContext := ai.Context{
		Type: ai.ContextTypeUser,
		Content: fmt.Sprintf(`User Information:

Basic Info:
- Username: %s
- Display Name: %s
- Description: %s%s
- Account Created: %s
- Reasons: %s
- Confidence: %.2f

Status Information:
- Current Status: %s
- Reputation: %d Reports, %d Safe Votes
- Last Updated: %s

Flagged Friends (showing %d of %d flagged, %d total):
%s

Flagged Groups (showing %d of %d flagged, %d total):
%s

Recent Outfits (showing %d of %d):
%s

Recent Games (showing %d of %d):
%s`,
			user.Name,
			user.DisplayName,
			description,
			map[bool]string{true: "\n- Translated Description: " + translatedDescription, false: ""}[translatedDescription != ""],
			user.CreatedAt.Format(time.RFC3339),
			strings.Join(user.Reasons.Messages(), "; "),
			user.Confidence,
			user.Status.String(),
			user.Reputation.Downvotes,
			user.Reputation.Upvotes,
			user.LastUpdated.Format(time.RFC3339),
			shownFriends, flaggedFriendsCount, len(user.Friends),
			strings.Join(friendsInfo, "\n"),
			shownGroups, flaggedGroupsCount, len(user.Groups),
			strings.Join(groupsInfo, "\n"),
			len(outfitsInfo), len(user.Outfits),
			strings.Join(outfitsInfo, "\n"),
			len(gamesInfo), len(user.Games),
			strings.Join(gamesInfo, "\n")),
	}

	// Append to existing chat context
	chatContext := session.ChatContext.Get(s)
	chatContext = append(chatContext, userContext)
	session.ChatContext.Set(s, chatContext)

	// Navigate to chat
	session.PaginationPage.Set(s, 0)
	ctx.Show(constants.ChatPageName, "")
}

// handleReasonModalSubmit processes the reason message from the modal.
func (m *ReviewMenu) handleReasonModalSubmit(ctx *interaction.Context, s *session.Session) {
	// Get the reason type from session
	reasonTypeStr := session.SelectedReasonType.Get(s)
	reasonType, err := enum.UserReasonTypeString(reasonTypeStr)
	if err != nil {
		ctx.Error("Invalid reason type: " + reasonTypeStr)
		return
	}

	// Get current user
	user := session.UserTarget.Get(s)

	// Initialize reasons map if nil
	if user.Reasons == nil {
		user.Reasons = make(types.Reasons[enum.UserReasonType])
	}

	// Get the reason message from the modal
	data := ctx.Event().ModalData()
	reasonMessage := data.Text(constants.AddReasonInputCustomID)
	confidenceStr := data.Text(constants.AddReasonConfidenceInputCustomID)
	evidenceText := data.Text(constants.AddReasonEvidenceInputCustomID)

	// Get existing reason if editing
	var existingReason *types.Reason
	if existing, exists := user.Reasons[reasonType]; exists {
		existingReason = existing
	}

	// Create or update reason
	var reason types.Reason
	if existingReason != nil {
		// Check if reasons field is empty
		if reasonMessage == "" {
			delete(user.Reasons, reasonType)
			user.Confidence = utils.CalculateConfidence(user.Reasons)

			// Update session
			session.UserTarget.Set(s, user)
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
	user.Reasons[reasonType] = &reason

	// Recalculate overall confidence
	user.Confidence = utils.CalculateConfidence(user.Reasons)

	// Update session
	session.UserTarget.Set(s, user)
	session.SelectedReasonType.Delete(s)
	session.ReasonsChanged.Set(s, true)

	action := "added"
	if existingReason != nil {
		action = "updated"
	}
	ctx.Reload(fmt.Sprintf("Successfully %s %s reason", action, reasonType.String()))
}

// handleReasonSelection processes reason management dropdown selections.
func (m *ReviewMenu) handleReasonSelection(ctx *interaction.Context, s *session.Session, option string) {
	// Check if user is a reviewer
	if !s.BotSettings().IsReviewer(uint64(ctx.Event().User().ID)) {
		m.layout.logger.Error("Non-reviewer attempted to manage reasons",
			zap.Uint64("user_id", uint64(ctx.Event().User().ID)))
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
		session.ReasonsChanged.Set(s, false)

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

	// Initialize reasons map if nil
	if user.Reasons == nil {
		user.Reasons = make(types.Reasons[enum.UserReasonType])
	}

	// Store the selected reason type in session
	session.SelectedReasonType.Set(s, option)

	// Check if we're editing an existing reason
	var existingReason *types.Reason
	if existing, exists := user.Reasons[reasonType]; exists {
		existingReason = existing
	}

	// Show modal to user
	ctx.Modal(m.buildReasonModal(reasonType, existingReason))
}

// buildReasonModal creates a modal for adding or editing a reason.
func (m *ReviewMenu) buildReasonModal(reasonType enum.UserReasonType, existingReason *types.Reason) *discord.ModalCreateBuilder {
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
			WithPlaceholder("Enter the reason for flagging this user")
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
			WithValue(strings.Join(escapedEvidence, "\n")).
			WithPlaceholder("Enter new evidence items, one per line")
	} else {
		evidenceInput = evidenceInput.WithRequired(false).
			WithMaxLength(1000).
			WithPlaceholder("Enter evidence items, one per line")
	}
	modal.AddActionRow(evidenceInput)

	return modal
}

// fetchNewTarget gets a new user to review based on the current sort order.
func (m *ReviewMenu) fetchNewTarget(ctx *interaction.Context, s *session.Session) (*types.ReviewUser, bool, error) {
	if m.CheckBreakRequired(ctx, s) {
		return nil, false, ErrBreakRequired
	}

	// Check if user is banned for low accuracy
	isBanned, err := m.layout.db.Service().Vote().CheckVoteAccuracy(ctx.Context(), uint64(ctx.Event().User().ID))
	if err != nil {
		m.layout.logger.Error("Failed to check vote accuracy",
			zap.Error(err),
			zap.Uint64("user_id", uint64(ctx.Event().User().ID)))
		// Continue anyway - not a big requirement
	}

	// Get the next user to review
	reviewerID := uint64(ctx.Event().User().ID)
	defaultSort := session.UserUserDefaultSort.Get(s)
	reviewTargetMode := session.UserReviewTargetMode.Get(s)

	user, err := m.layout.db.Service().User().GetUserToReview(
		ctx.Context(), defaultSort, reviewTargetMode, reviewerID,
	)
	if err != nil {
		return nil, isBanned, err
	}

	// Store the user and their original reasons in session
	session.UserTarget.Set(s, user)
	session.OriginalUserReasons.Set(s, user.Reasons)
	session.ReasonsChanged.Set(s, false)

	// Add current user to history and set index to point to it
	history := session.UserReviewHistory.Get(s)
	history = append(history, user.ID)

	// Trim history if it exceeds the maximum size
	if len(history) > constants.MaxReviewHistorySize {
		history = history[len(history)-constants.MaxReviewHistorySize:]
	}

	session.UserReviewHistory.Set(s, history)
	session.UserReviewHistoryIndex.Set(s, len(history)-1)

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

	return user, isBanned, nil
}
