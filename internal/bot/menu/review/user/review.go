package user

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	builder "github.com/robalyx/rotector/internal/bot/builder/review/user"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/pagination"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/bot/interfaces"
	"github.com/robalyx/rotector/internal/bot/menu/log"
	"github.com/robalyx/rotector/internal/common/queue"
	"github.com/robalyx/rotector/internal/common/storage/database/types"
	"github.com/robalyx/rotector/internal/common/storage/database/types/enum"
	"github.com/robalyx/rotector/internal/common/utils"
	"go.uber.org/zap"
)

var ErrBreakRequired = errors.New("break required")

// ReviewMenu handles the display and interaction logic for the review interface.
type ReviewMenu struct {
	layout *Layout
	page   *pagination.Page
}

// NewReviewMenu creates a new review menu.
func NewReviewMenu(layout *Layout) *ReviewMenu {
	m := &ReviewMenu{layout: layout}
	m.page = &pagination.Page{
		Name: constants.UserReviewPageName,
		Message: func(s *session.Session) *discord.MessageUpdateBuilder {
			return builder.NewReviewBuilder(s, layout.translator, layout.db).Build()
		},
		ShowHandlerFunc:   m.Show,
		SelectHandlerFunc: m.handleSelectMenu,
		ButtonHandlerFunc: m.handleButton,
		ModalHandlerFunc:  m.handleModal,
	}
	return m
}

// Show prepares and displays the review interface.
func (m *ReviewMenu) Show(event interfaces.CommonEvent, s *session.Session, r *pagination.Respond) {
	// Force training mode if user is not a reviewer
	if !s.BotSettings().IsReviewer(uint64(event.User().ID)) && session.UserReviewMode.Get(s) != enum.ReviewModeTraining {
		session.UserReviewMode.Set(s, enum.ReviewModeTraining)
	}

	// If no user is set in session, fetch a new one
	user := session.UserTarget.Get(s)
	if user == nil {
		var isBanned bool
		var err error
		user, isBanned, err = m.fetchNewTarget(event, s, r)
		if err != nil {
			if errors.Is(err, types.ErrNoUsersToReview) {
				r.Cancel(event, s, "No users to review. Please check back later.")
				return
			}
			if errors.Is(err, ErrBreakRequired) {
				return
			}
			m.layout.logger.Error("Failed to fetch a new user", zap.Error(err))
			r.Error(event, "Failed to fetch a new user. Please try again.")
			return
		}

		if isBanned {
			r.Show(event, s, constants.BanPageName, "You have been banned for suspicious voting patterns.")
			return
		}
	}

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
		flaggedFriends, err = m.layout.db.Models().Users().GetUsersByIDs(
			context.Background(),
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
		flaggedGroups, err = m.layout.db.Models().Groups().GetGroupsByIDs(
			context.Background(),
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
}

// handleSelectMenu processes select menu interactions.
func (m *ReviewMenu) handleSelectMenu(
	event *events.ComponentInteractionCreate, s *session.Session, r *pagination.Respond, customID, option string,
) {
	if m.checkCaptchaRequired(event, s, r) {
		return
	}

	switch customID {
	case constants.SortOrderSelectMenuCustomID:
		m.handleSortOrderSelection(event, s, r, option)
	case constants.ActionSelectMenuCustomID:
		m.handleActionSelection(event, s, r, option)
	case constants.ReasonSelectMenuCustomID:
		m.handleReasonSelection(event, s, r, option)
	}
}

// handleSortOrderSelection processes sort order menu selections.
func (m *ReviewMenu) handleSortOrderSelection(
	event *events.ComponentInteractionCreate, s *session.Session, r *pagination.Respond, option string,
) {
	// Parse option to review sort
	sortBy, err := enum.ReviewSortByString(option)
	if err != nil {
		m.layout.logger.Error("Failed to parse sort order", zap.Error(err))
		r.Error(event, "Failed to parse sort order. Please try again.")
		return
	}

	// Update user's default sort preference
	session.UserUserDefaultSort.Set(s, sortBy)

	r.Reload(event, s, "Changed sort order. Will take effect for the next user.")
}

// handleActionSelection processes action menu selections.
func (m *ReviewMenu) handleActionSelection(
	event *events.ComponentInteractionCreate, s *session.Session, r *pagination.Respond, option string,
) {
	userID := uint64(event.User().ID)
	isReviewer := s.BotSettings().IsReviewer(userID)

	// Check reviewer-only options
	switch option {
	case constants.OpenAIChatButtonCustomID,
		constants.ViewUserLogsButtonCustomID,
		constants.RecheckButtonCustomID,
		constants.ReviewModeOption:
		if !isReviewer {
			m.layout.logger.Error("Non-reviewer attempted restricted action",
				zap.Uint64("user_id", userID),
				zap.String("action", option))
			r.Error(event, "You do not have permission to perform this action.")
			return
		}
	}

	// Process selected option
	switch option {
	case constants.OpenFriendsMenuButtonCustomID:
		session.PaginationPage.Set(s, 0)
		r.Show(event, s, constants.UserFriendsPageName, "")
	case constants.OpenGroupsMenuButtonCustomID:
		session.PaginationPage.Set(s, 0)
		r.Show(event, s, constants.UserGroupsPageName, "")
	case constants.OpenOutfitsMenuButtonCustomID:
		session.PaginationPage.Set(s, 0)
		r.Show(event, s, constants.UserOutfitsPageName, "")
	case constants.OpenAIChatButtonCustomID:
		m.handleOpenAIChat(event, s, r)
	case constants.ViewUserLogsButtonCustomID:
		m.handleViewUserLogs(event, s, r)
	case constants.RecheckButtonCustomID:
		m.handleRecheck(event, s, r)
	case constants.ReviewModeOption:
		session.SettingType.Set(s, constants.UserSettingPrefix)
		session.SettingCustomID.Set(s, constants.ReviewModeOption)
		r.Show(event, s, constants.SettingUpdatePageName, "")
	case constants.ReviewTargetModeOption:
		session.SettingType.Set(s, constants.UserSettingPrefix)
		session.SettingCustomID.Set(s, constants.ReviewTargetModeOption)
		r.Show(event, s, constants.SettingUpdatePageName, "")
	}
}

// handleButton handles the buttons for the review menu.
func (m *ReviewMenu) handleButton(
	event *events.ComponentInteractionCreate, s *session.Session, r *pagination.Respond, customID string,
) {
	if m.checkCaptchaRequired(event, s, r) {
		return
	}

	switch customID {
	case constants.BackButtonCustomID:
		r.NavigateBack(event, s, "")
	case constants.ConfirmButtonCustomID:
		m.handleConfirmUser(event, s, r)
	case constants.ClearButtonCustomID:
		m.handleClearUser(event, s, r)
	case constants.SkipButtonCustomID:
		m.handleSkipUser(event, s, r)
	}
}

// handleModal handles modal submissions for the review menu.
func (m *ReviewMenu) handleModal(event *events.ModalSubmitInteractionCreate, s *session.Session, r *pagination.Respond) {
	if m.checkCaptchaRequired(event, s, r) {
		return
	}

	switch event.Data.CustomID {
	case constants.RecheckReasonModalCustomID:
		m.handleRecheckModalSubmit(event, s, r)
	case constants.AddReasonModalCustomID:
		m.handleReasonModalSubmit(event, s, r)
	}
}

// handleRecheck adds the user to the high priority queue for re-processing.
// If the user is already in queue, it shows the status menu instead.
func (m *ReviewMenu) handleRecheck(event *events.ComponentInteractionCreate, s *session.Session, r *pagination.Respond) {
	user := session.UserTarget.Get(s)

	// Check if user is already in queue to prevent duplicate entries
	status, _, _, err := m.layout.queueManager.GetQueueInfo(context.Background(), user.ID)
	if err == nil && status != "" {
		r.Show(event, s, constants.UserStatusPageName, "")
		return
	}

	// Create modal for reason input
	modal := discord.NewModalCreateBuilder().
		SetCustomID(constants.RecheckReasonModalCustomID).
		SetTitle("Recheck User").
		AddActionRow(
			discord.NewTextInput(constants.RecheckReasonInputCustomID, discord.TextInputStyleParagraph, "Recheck Reason").
				WithRequired(true).
				WithPlaceholder("Enter the reason for rechecking this user..."),
		)

	// Show modal to user
	r.Modal(event, s, modal)
}

// handleRecheckModalSubmit processes the custom recheck reason from the modal.
func (m *ReviewMenu) handleRecheckModalSubmit(
	event *events.ModalSubmitInteractionCreate, s *session.Session, r *pagination.Respond,
) {
	// Get and validate the recheck reason
	reason := event.Data.Text(constants.RecheckReasonInputCustomID)
	if reason == "" {
		r.Cancel(event, s, "Recheck reason cannot be empty. Please try again.")
		return
	}

	// Determine priority based on review mode
	priority := queue.PriorityHigh
	if session.UserReviewMode.Get(s) == enum.ReviewModeTraining {
		priority = queue.PriorityLow
	}

	user := session.UserTarget.Get(s)

	// Add to queue with reviewer information
	err := m.layout.queueManager.AddToQueue(context.Background(), &queue.Item{
		UserID:      user.ID,
		Priority:    priority,
		Reason:      reason,
		AddedBy:     uint64(event.User().ID),
		AddedAt:     time.Now(),
		Status:      queue.StatusPending,
		CheckExists: true,
	})
	if err != nil {
		m.layout.logger.Error("Failed to add user to queue", zap.Error(err))
		r.Error(event, "Failed to add user to queue")
		return
	}

	// Store queue position information for status display
	err = m.layout.queueManager.SetQueueInfo(
		context.Background(),
		user.ID,
		queue.StatusPending,
		priority,
		m.layout.queueManager.GetQueueLength(context.Background(), priority),
	)
	if err != nil {
		m.layout.logger.Error("Failed to update queue info", zap.Error(err))
		r.Error(event, "Failed to update queue info")
		return
	}

	// Show status menu to track progress
	session.QueueUser.Set(s, user.ID)
	r.Show(event, s, constants.UserStatusPageName, "")

	// Log the activity
	m.layout.db.Models().Activities().Log(context.Background(), &types.ActivityLog{
		ActivityTarget: types.ActivityTarget{
			UserID: user.ID,
		},
		ReviewerID:        uint64(event.User().ID),
		ActivityType:      enum.ActivityTypeUserRechecked,
		ActivityTimestamp: time.Now(),
		Details:           map[string]any{"reason": reason},
	})
}

// handleViewUserLogs handles the shortcut to view user logs.
func (m *ReviewMenu) handleViewUserLogs(
	event *events.ComponentInteractionCreate, s *session.Session, r *pagination.Respond,
) {
	user := session.UserTarget.Get(s)
	if user == nil {
		m.layout.logger.Error("No user selected to view logs")
		r.Error(event, "No user selected to view logs.")
		return
	}

	// Set the user ID filter
	log.ResetLogs(s)
	log.ResetFilters(s)
	session.LogFilterUserID.Set(s, user.ID)

	// Show the logs menu
	r.Show(event, s, constants.LogPageName, "")
}

// handleConfirmUser moves a user to the confirmed state and logs the action.
func (m *ReviewMenu) handleConfirmUser(event interfaces.CommonEvent, s *session.Session, r *pagination.Respond) {
	user := session.UserTarget.Get(s)

	var actionMsg string
	if session.UserReviewMode.Get(s) == enum.ReviewModeTraining {
		// Training mode - increment downvotes
		if err := m.layout.db.Models().Reputation().UpdateUserVotes(
			context.Background(), user.ID, uint64(event.User().ID), false,
		); err != nil {
			m.layout.logger.Error("Failed to update downvotes", zap.Error(err))
			r.Error(event, "Failed to update downvotes. Please try again.")
			return
		}
		user.Reputation.Downvotes++
		actionMsg = "downvoted"

		// Log the training downvote action
		go m.layout.db.Models().Activities().Log(context.Background(), &types.ActivityLog{
			ActivityTarget: types.ActivityTarget{
				UserID: user.ID,
			},
			ReviewerID:        uint64(event.User().ID),
			ActivityType:      enum.ActivityTypeUserTrainingDownvote,
			ActivityTimestamp: time.Now(),
			Details: map[string]any{
				"upvotes":   user.Reputation.Upvotes,
				"downvotes": user.Reputation.Downvotes,
			},
		})
	} else {
		// Standard mode - check permissions and confirm user
		if !s.BotSettings().IsReviewer(uint64(event.User().ID)) {
			m.layout.logger.Error("Non-reviewer attempted to confirm user",
				zap.Uint64("user_id", uint64(event.User().ID)))
			r.Error(event, "You do not have permission to confirm users.")
			return
		}

		// Calculate vote percentages
		totalVotes := float64(user.Reputation.Upvotes + user.Reputation.Downvotes)
		if totalVotes >= constants.MinimumVotesRequired {
			upvotePercentage := float64(user.Reputation.Upvotes) / totalVotes

			// If there's a strong consensus for clearing, prevent confirmation
			if upvotePercentage >= constants.VoteConsensusThreshold {
				r.Cancel(event, s, fmt.Sprintf("Cannot confirm - %.0f%% of %d votes indicate this user is safe",
					upvotePercentage*100, int(totalVotes)))
				return
			}
		}

		// Confirm the user
		if err := m.layout.db.Models().Users().ConfirmUser(context.Background(), user); err != nil {
			m.layout.logger.Error("Failed to confirm user", zap.Error(err))
			r.Error(event, "Failed to confirm the user. Please try again.")
			return
		}
		actionMsg = "confirmed"

		// Log reason changes if any were made
		if session.ReasonsChanged.Get(s) {
			originalReasons := session.OriginalUserReasons.Get(s)
			go m.layout.db.Models().Activities().Log(context.Background(), &types.ActivityLog{
				ActivityTarget: types.ActivityTarget{
					UserID: user.ID,
				},
				ReviewerID:        uint64(event.User().ID),
				ActivityType:      enum.ActivityTypeUserReasonUpdated,
				ActivityTimestamp: time.Now(),
				Details: map[string]any{
					"originalReasons": originalReasons.Messages(),
					"updatedReasons":  user.Reasons.Messages(),
				},
			})
		}

		// Log the confirm action
		go m.layout.db.Models().Activities().Log(context.Background(), &types.ActivityLog{
			ActivityTarget: types.ActivityTarget{
				UserID: user.ID,
			},
			ReviewerID:        uint64(event.User().ID),
			ActivityType:      enum.ActivityTypeUserConfirmed,
			ActivityTimestamp: time.Now(),
			Details: map[string]any{
				"reasons":    user.Reasons.Messages(),
				"confidence": user.Confidence,
			},
		})
	}

	// Get the number of flagged users left to review
	flaggedCount, err := m.layout.db.Models().Users().GetFlaggedUsersCount(context.Background())
	if err != nil {
		m.layout.logger.Error("Failed to get flagged users count", zap.Error(err))
	}

	// Clear current user and load next one
	session.UserTarget.Delete(s)
	r.Reload(event, s, fmt.Sprintf("User %s. %d users left to review.", actionMsg, flaggedCount))
	m.updateCounters(s)
}

// handleClearUser removes a user from the flagged state and logs the action.
func (m *ReviewMenu) handleClearUser(event interfaces.CommonEvent, s *session.Session, r *pagination.Respond) {
	user := session.UserTarget.Get(s)

	var actionMsg string
	if session.UserReviewMode.Get(s) == enum.ReviewModeTraining {
		// Training mode - increment upvotes
		if err := m.layout.db.Models().Reputation().UpdateUserVotes(
			context.Background(), user.ID, uint64(event.User().ID), true,
		); err != nil {
			m.layout.logger.Error("Failed to update upvotes", zap.Error(err))
			r.Error(event, "Failed to update upvotes. Please try again.")
			return
		}
		user.Reputation.Upvotes++
		actionMsg = "upvoted"

		// Log the training upvote action
		go m.layout.db.Models().Activities().Log(context.Background(), &types.ActivityLog{
			ActivityTarget: types.ActivityTarget{
				UserID: user.ID,
			},
			ReviewerID:        uint64(event.User().ID),
			ActivityType:      enum.ActivityTypeUserTrainingUpvote,
			ActivityTimestamp: time.Now(),
			Details: map[string]any{
				"upvotes":   user.Reputation.Upvotes,
				"downvotes": user.Reputation.Downvotes,
			},
		})
	} else {
		// Standard mode - check permissions and clear user
		if !s.BotSettings().IsReviewer(uint64(event.User().ID)) {
			m.layout.logger.Error("Non-reviewer attempted to clear user",
				zap.Uint64("user_id", uint64(event.User().ID)))
			r.Error(event, "You do not have permission to clear users.")
			return
		}

		// Calculate vote percentages
		totalVotes := float64(user.Reputation.Upvotes + user.Reputation.Downvotes)
		if totalVotes >= constants.MinimumVotesRequired {
			downvotePercentage := float64(user.Reputation.Downvotes) / totalVotes

			// If there's a strong consensus for confirming, prevent clearing
			if downvotePercentage >= constants.VoteConsensusThreshold {
				r.Cancel(event, s, fmt.Sprintf("Cannot clear - %.0f%% of %d votes indicate this user is suspicious",
					downvotePercentage*100, int(totalVotes)))
				return
			}
		}

		// Log reason changes if any were made
		if session.ReasonsChanged.Get(s) {
			originalReasons := session.OriginalUserReasons.Get(s)
			go m.layout.db.Models().Activities().Log(context.Background(), &types.ActivityLog{
				ActivityTarget: types.ActivityTarget{
					UserID: user.ID,
				},
				ReviewerID:        uint64(event.User().ID),
				ActivityType:      enum.ActivityTypeUserReasonUpdated,
				ActivityTimestamp: time.Now(),
				Details: map[string]any{
					"originalReasons": originalReasons.Messages(),
					"updatedReasons":  user.Reasons.Messages(),
				},
			})
		}

		// Clear the user
		if err := m.layout.db.Models().Users().ClearUser(context.Background(), user); err != nil {
			m.layout.logger.Error("Failed to clear user", zap.Error(err))
			r.Error(event, "Failed to clear the user. Please try again.")
			return
		}
		actionMsg = "cleared"

		// Remove user from group tracking
		go m.layout.db.Models().Tracking().RemoveUserFromGroups(context.Background(), user.ID, user.Groups)

		// Log the clear action
		go m.layout.db.Models().Activities().Log(context.Background(), &types.ActivityLog{
			ActivityTarget: types.ActivityTarget{
				UserID: user.ID,
			},
			ReviewerID:        uint64(event.User().ID),
			ActivityType:      enum.ActivityTypeUserCleared,
			ActivityTimestamp: time.Now(),
			Details:           map[string]any{},
		})
	}

	// Get the number of flagged users left to review
	flaggedCount, err := m.layout.db.Models().Users().GetFlaggedUsersCount(context.Background())
	if err != nil {
		m.layout.logger.Error("Failed to get flagged users count", zap.Error(err))
	}

	// Clear current user and load next one
	session.UserTarget.Delete(s)
	r.Reload(event, s, fmt.Sprintf("User %s. %d users left to review.", actionMsg, flaggedCount))
	m.updateCounters(s)
}

// handleSkipUser logs the skip action and moves to the next user.
func (m *ReviewMenu) handleSkipUser(event interfaces.CommonEvent, s *session.Session, r *pagination.Respond) {
	// Get the number of flagged users left to review
	flaggedCount, err := m.layout.db.Models().Users().GetFlaggedUsersCount(context.Background())
	if err != nil {
		m.layout.logger.Error("Failed to get flagged users count", zap.Error(err))
	}

	// Clear current user and load next one
	session.UserTarget.Delete(s)
	r.Reload(event, s, fmt.Sprintf("Skipped user. %d users left to review.", flaggedCount))
	m.updateCounters(s)

	// Log the skip action
	user := session.UserTarget.Get(s)
	if user != nil {
		m.layout.db.Models().Activities().Log(context.Background(), &types.ActivityLog{
			ActivityTarget: types.ActivityTarget{
				UserID: user.ID,
			},
			ReviewerID:        uint64(event.User().ID),
			ActivityType:      enum.ActivityTypeUserSkipped,
			ActivityTimestamp: time.Now(),
			Details:           map[string]any{},
		})
	}
}

// handleOpenAIChat handles the button to open the AI chat for the current user.
func (m *ReviewMenu) handleOpenAIChat(event *events.ComponentInteractionCreate, s *session.Session, r *pagination.Respond) {
	user := session.UserTarget.Get(s)
	flaggedFriends := session.UserFlaggedFriends.Get(s)
	flaggedGroups := session.UserFlaggedGroups.Get(s)

	// Build friends information
	friendsInfo := make([]string, 0, len(user.Friends))
	for _, friend := range user.Friends {
		info := fmt.Sprintf("- %s (ID: %d)", friend.Name, friend.ID)
		if flagged := flaggedFriends[friend.ID]; flagged != nil {
			messages := flagged.Reasons.Messages()
			info += fmt.Sprintf(" | Status: %s | Reasons: %s | Confidence: %.2f",
				flagged.Status.String(),
				strings.Join(messages, "; "),
				flagged.Confidence)
		}
		friendsInfo = append(friendsInfo, info)
	}

	// Build groups information
	groupsInfo := make([]string, 0, len(user.Groups))
	for _, group := range user.Groups {
		info := fmt.Sprintf("- %s (ID: %d) | Role: %s",
			group.Group.Name,
			group.Group.ID,
			group.Role.Name)
		if flagged := flaggedGroups[group.Group.ID]; flagged != nil {
			messages := flagged.Reasons.Messages()
			info += fmt.Sprintf(" | Status: %s | Reasons: %s | Confidence: %.2f",
				flagged.Status.String(),
				strings.Join(messages, "; "),
				flagged.Confidence)
		}
		groupsInfo = append(groupsInfo, info)
	}

	// Build outfits information
	outfitsInfo := make([]string, 0, len(user.Outfits))
	for _, outfit := range user.Outfits {
		outfitsInfo = append(outfitsInfo, fmt.Sprintf("- %s (ID: %d)", outfit.Name, outfit.ID))
	}

	// Build games information
	gamesInfo := make([]string, 0, len(user.Games))
	for _, game := range user.Games {
		gamesInfo = append(gamesInfo, fmt.Sprintf("- %s (ID: %d) | Visits: %d",
			game.Name, game.ID, game.PlaceVisits))
	}

	// Create context message about the user
	context := fmt.Sprintf(`<context>
User Information:

Basic Info:
- Username: %s
- Display Name: %s
- Description: %s
- Account Created: %s
- Reasons: %s
- Confidence: %.2f

Status Information:
- Current Status: %s
- Reputation: %d Reports, %d Safe Votes
- Last Updated: %s

Friends (%d total, %d flagged):
%s

Groups (%d total, %d flagged):
%s

Outfits (%d total):
%s

Games (%d total):
%s</context>`,
		user.Name,
		user.DisplayName,
		user.Description,
		user.CreatedAt.Format(time.RFC3339),
		user.Reasons.Messages(),
		user.Confidence,
		user.Status.String(),
		user.Reputation.Downvotes,
		user.Reputation.Upvotes,
		user.LastUpdated.Format(time.RFC3339),
		len(user.Friends),
		len(flaggedFriends),
		strings.Join(friendsInfo, " "),
		len(user.Groups),
		len(flaggedGroups),
		strings.Join(groupsInfo, " "),
		len(user.Outfits),
		strings.Join(outfitsInfo, " "),
		len(user.Games),
		strings.Join(gamesInfo, " "),
	)

	// Update session and navigate to chat
	session.ChatContext.Set(s, context)
	session.PaginationPage.Set(s, 0)
	r.Show(event, s, constants.ChatPageName, "")
}

// handleReasonModalSubmit processes the reason message from the modal.
func (m *ReviewMenu) handleReasonModalSubmit(
	event *events.ModalSubmitInteractionCreate, s *session.Session, r *pagination.Respond,
) {
	// Get the reason type from session
	reasonTypeStr := session.SelectedReasonType.Get(s)
	reasonType, err := enum.UserReasonTypeString(reasonTypeStr)
	if err != nil {
		r.Error(event, "Invalid reason type: "+reasonTypeStr)
		return
	}

	// Get the reason message from the modal
	reasonMessage := event.Data.Text(constants.AddReasonInputCustomID)
	if reasonMessage == "" {
		r.Cancel(event, s, "Reason message cannot be empty. Please try again.")
		return
	}

	// Get and validate confidence value
	confidenceStr := event.Data.Text(constants.AddReasonConfidenceInputCustomID)
	confidence, err := strconv.ParseFloat(confidenceStr, 64)
	if err != nil || confidence < 0.01 || confidence > 1.0 {
		r.Cancel(event, s, "Invalid confidence value. Please enter a number between 0.01 and 1.00.")
		return
	}

	// Get current user
	user := session.UserTarget.Get(s)
	if user == nil {
		r.Error(event, "No user selected")
		return
	}

	// Initialize reasons map if nil
	if user.Reasons == nil {
		user.Reasons = make(types.Reasons[enum.UserReasonType])
	}

	// Add the reason
	user.Reasons[reasonType] = &types.Reason{
		Message:    reasonMessage,
		Confidence: confidence,
	}

	// Recalculate overall confidence
	user.Confidence = utils.CalculateConfidence(user.Reasons)

	// Update session
	session.UserTarget.Set(s, user)
	session.SelectedReasonType.Delete(s)
	session.ReasonsChanged.Set(s, true)

	r.Reload(event, s, fmt.Sprintf("Successfully added %s reason", reasonType.String()))
}

// handleReasonSelection processes reason management dropdown selections.
func (m *ReviewMenu) handleReasonSelection(
	event *events.ComponentInteractionCreate, s *session.Session, r *pagination.Respond, option string,
) {
	// Check if user is a reviewer
	if !s.BotSettings().IsReviewer(uint64(event.User().ID)) {
		m.layout.logger.Error("Non-reviewer attempted to manage reasons",
			zap.Uint64("user_id", uint64(event.User().ID)))
		r.Error(event, "You do not have permission to manage reasons.")
		return
	}

	// Handle refresh option
	if option == constants.RefreshButtonCustomID {
		user := session.UserTarget.Get(s)
		if user == nil {
			r.Error(event, "No user selected")
			return
		}

		// Restore original reasons
		originalReasons := session.OriginalUserReasons.Get(s)
		user.Reasons = originalReasons
		user.Confidence = utils.CalculateConfidence(user.Reasons)

		// Update session
		session.UserTarget.Set(s, user)
		session.ReasonsChanged.Set(s, false)

		r.Reload(event, s, "Successfully restored original reasons")
		return
	}

	// Parse reason type
	option = strings.TrimSuffix(option, constants.ModalOpenSuffix)
	reasonType, err := enum.UserReasonTypeString(option)
	if err != nil {
		r.Error(event, "Invalid reason type: "+option)
		return
	}

	// Get current user
	user := session.UserTarget.Get(s)
	if user == nil {
		r.Error(event, "No user selected")
		return
	}

	// Initialize reasons map if nil
	if user.Reasons == nil {
		user.Reasons = make(types.Reasons[enum.UserReasonType])
	}

	// Check if reason exists
	if _, exists := user.Reasons[reasonType]; exists {
		// Remove existing reason
		delete(user.Reasons, reasonType)

		// Recalculate overall confidence
		user.Confidence = utils.CalculateConfidence(user.Reasons)

		// Update session
		session.UserTarget.Set(s, user)
		session.ReasonsChanged.Set(s, true)

		r.Reload(event, s, fmt.Sprintf("Successfully removed %s reason", reasonType.String()))
		return
	}

	// Store the selected reason type in session
	session.SelectedReasonType.Set(s, option)

	// Create modal for reason input
	modal := discord.NewModalCreateBuilder().
		SetCustomID(constants.AddReasonModalCustomID).
		SetTitle(fmt.Sprintf("Add %s Reason", reasonType.String())).
		AddActionRow(
			discord.NewTextInput(constants.AddReasonInputCustomID, discord.TextInputStyleParagraph, "Reason").
				WithRequired(true).
				WithMinLength(32).
				WithMaxLength(256).
				WithPlaceholder("Enter the reason for flagging this user..."),
		).
		AddActionRow(
			discord.NewTextInput(constants.AddReasonConfidenceInputCustomID, discord.TextInputStyleShort, "Confidence").
				WithRequired(true).
				WithMinLength(1).
				WithMaxLength(4).
				WithPlaceholder("Enter confidence (0.01-1.00)..."),
		)

	// Show modal to user
	r.Modal(event, s, modal)
}

// fetchNewTarget gets a new user to review based on the current sort order.
func (m *ReviewMenu) fetchNewTarget(
	event interfaces.CommonEvent, s *session.Session, r *pagination.Respond,
) (*types.ReviewUser, bool, error) {
	if m.checkBreakRequired(event, s, r) {
		return nil, false, ErrBreakRequired
	}

	// Check if user is banned for low accuracy
	isBanned, err := m.layout.db.Models().Votes().CheckVoteAccuracy(context.Background(), uint64(event.User().ID))
	if err != nil {
		m.layout.logger.Error("Failed to check vote accuracy",
			zap.Error(err),
			zap.Uint64("user_id", uint64(event.User().ID)))
		// Continue anyway - not a big requirement
	}

	// Get the next user to review
	reviewerID := uint64(event.User().ID)
	defaultSort := session.UserUserDefaultSort.Get(s)
	reviewTargetMode := session.UserReviewTargetMode.Get(s)

	user, err := m.layout.db.Models().Users().GetUserToReview(context.Background(), defaultSort, reviewTargetMode, reviewerID)
	if err != nil {
		return nil, isBanned, err
	}

	// Store the user and their original reasons in session
	session.UserTarget.Set(s, user)
	session.OriginalUserReasons.Set(s, user.Reasons)
	session.ReasonsChanged.Set(s, false)

	// Log the view action
	go m.layout.db.Models().Activities().Log(context.Background(), &types.ActivityLog{
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

// checkBreakRequired checks if a break is needed.
func (m *ReviewMenu) checkBreakRequired(event interfaces.CommonEvent, s *session.Session, r *pagination.Respond) bool {
	// Check if user needs a break
	nextReviewTime := session.UserReviewBreakNextReviewTime.Get(s)
	if !nextReviewTime.IsZero() && time.Now().Before(nextReviewTime) {
		// Show timeout menu if break time hasn't passed
		r.Show(event, s, constants.TimeoutPageName, "")
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
		r.Show(event, s, constants.TimeoutPageName, "")
		return true
	}

	// Increment review count
	session.UserReviewBreakSessionReviews.Set(s, sessionReviews+1)

	return false
}

// checkCaptchaRequired checks if CAPTCHA verification is needed.
func (m *ReviewMenu) checkCaptchaRequired(event interfaces.CommonEvent, s *session.Session, r *pagination.Respond) bool {
	if m.layout.captcha.IsRequired(s) {
		r.Cancel(event, s, "Please complete CAPTCHA verification to continue.")
		return true
	}
	return false
}

// updateCounters updates the review counters.
func (m *ReviewMenu) updateCounters(s *session.Session) {
	if err := m.layout.captcha.IncrementReviewCounter(s); err != nil {
		m.layout.logger.Error("Failed to update review counter", zap.Error(err))
	}
}
