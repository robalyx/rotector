package user

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	builder "github.com/robalyx/rotector/internal/bot/builder/review/user"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/pagination"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/bot/interfaces"
	"github.com/robalyx/rotector/internal/common/queue"
	"github.com/robalyx/rotector/internal/common/storage/database/types"
	"github.com/robalyx/rotector/internal/common/storage/database/types/enum"
	"go.uber.org/zap"
)

// ReviewMenu handles the display and interaction logic for the review interface.
type ReviewMenu struct {
	layout *Layout
	page   *pagination.Page
}

// NewReviewMenu creates a ReviewMenu and sets up its page with message builders and
// interaction handlers. The page is configured to show user information
// and handle review actions.
func NewReviewMenu(layout *Layout) *ReviewMenu {
	m := &ReviewMenu{layout: layout}
	m.page = &pagination.Page{
		Name: "Review Menu",
		Message: func(s *session.Session) *discord.MessageUpdateBuilder {
			return builder.NewReviewBuilder(s, layout.translator, layout.db).Build()
		},
		SelectHandlerFunc: m.handleSelectMenu,
		ButtonHandlerFunc: m.handleButton,
		ModalHandlerFunc:  m.handleModal,
	}
	return m
}

// Show prepares and displays the review interface.
func (m *ReviewMenu) Show(event interfaces.CommonEvent, s *session.Session, content string) {
	var settings *types.BotSetting
	s.GetInterface(constants.SessionKeyBotSettings, &settings)
	var userSettings *types.UserSetting
	s.GetInterface(constants.SessionKeyUserSettings, &userSettings)

	// Force training mode if user is not a reviewer
	if !settings.IsReviewer(uint64(event.User().ID)) && userSettings.ReviewMode != enum.ReviewModeTraining {
		userSettings.ReviewMode = enum.ReviewModeTraining
		if err := m.layout.db.Settings().SaveUserSettings(context.Background(), userSettings); err != nil {
			m.layout.logger.Error("Failed to enforce training mode", zap.Error(err))
			m.layout.paginationManager.RespondWithError(event, "Failed to enforce training mode. Please try again.")
			return
		}
		s.Set(constants.SessionKeyUserSettings, userSettings)
	}

	var user *types.ReviewUser
	s.GetInterface(constants.SessionKeyTarget, &user)

	// If no user is set in session, fetch a new one
	if user == nil {
		var isBanned bool
		var err error
		user, isBanned, err = m.fetchNewTarget(event, s, uint64(event.User().ID))
		if err != nil {
			if errors.Is(err, types.ErrNoUsersToReview) {
				m.layout.paginationManager.NavigateBack(event, s, "No users to review. Please check back later.")
				return
			}
			m.layout.logger.Error("Failed to fetch a new user", zap.Error(err))
			m.layout.paginationManager.RespondWithError(event, "Failed to fetch a new user. Please try again.")
			return
		}

		if isBanned {
			m.layout.paginationManager.RespondWithError(event, "You have been banned for suspicious voting patterns.")
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
		flaggedFriends, err = m.layout.db.Users().GetUsersByIDs(context.Background(), friendIDs, types.UserFields{
			Basic:      true,
			Reason:     true,
			Confidence: true,
		})
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
		flaggedGroups, err = m.layout.db.Groups().GetGroupsByIDs(context.Background(), groupIDs, types.GroupFields{
			Basic:      true,
			Reason:     true,
			Confidence: true,
		})
		if err != nil {
			m.layout.logger.Error("Failed to get group data", zap.Error(err))
			return
		}
	}

	// Store data in session for the message builder
	s.Set(constants.SessionKeyFlaggedFriends, flaggedFriends)
	s.Set(constants.SessionKeyFlaggedGroups, flaggedGroups)

	m.layout.paginationManager.NavigateTo(event, s, m.page, content)
}

// handleSelectMenu processes select menu interactions.
func (m *ReviewMenu) handleSelectMenu(event *events.ComponentInteractionCreate, s *session.Session, customID string, option string) {
	if m.checkCaptchaRequired(event, s) {
		return
	}

	switch customID {
	case constants.SortOrderSelectMenuCustomID:
		m.handleSortOrderSelection(event, s, option)
	case constants.ActionSelectMenuCustomID:
		m.handleActionSelection(event, s, option)
	}
}

// handleSortOrderSelection processes sort order menu selections.
func (m *ReviewMenu) handleSortOrderSelection(event *events.ComponentInteractionCreate, s *session.Session, option string) {
	// Retrieve user settings from session
	var settings *types.UserSetting
	s.GetInterface(constants.SessionKeyUserSettings, &settings)

	// Parse option to review sort
	sortBy, err := enum.ReviewSortByString(option)
	if err != nil {
		m.layout.logger.Error("Failed to parse sort order", zap.Error(err))
		m.layout.paginationManager.RespondWithError(event, "Failed to parse sort order. Please try again.")
		return
	}

	// Update user's default sort preference
	settings.UserDefaultSort = sortBy
	if err := m.layout.db.Settings().SaveUserSettings(context.Background(), settings); err != nil {
		m.layout.logger.Error("Failed to save user settings", zap.Error(err))
		m.layout.paginationManager.RespondWithError(event, "Failed to save sort order. Please try again.")
		return
	}
	s.Set(constants.SessionKeyUserSettings, settings)

	m.Show(event, s, "Changed sort order. Will take effect for the next user.")
}

// handleActionSelection processes action menu selections.
func (m *ReviewMenu) handleActionSelection(event *events.ComponentInteractionCreate, s *session.Session, option string) {
	// Get bot settings to check reviewer status
	var settings *types.BotSetting
	s.GetInterface(constants.SessionKeyBotSettings, &settings)
	userID := uint64(event.User().ID)

	switch option {
	case constants.OpenFriendsMenuButtonCustomID:
		m.layout.friendsMenu.Show(event, s, 0)
	case constants.OpenGroupsMenuButtonCustomID:
		m.layout.groupsMenu.Show(event, s, 0)
	case constants.OpenOutfitsMenuButtonCustomID:
		m.layout.outfitsMenu.Show(event, s, 0)
	case constants.OpenAIChatButtonCustomID:
		if !settings.IsReviewer(userID) {
			m.layout.logger.Error("Non-reviewer attempted to open AI chat", zap.Uint64("user_id", userID))
			m.layout.paginationManager.RespondWithError(event, "You do not have permission to open the AI chat.")
			return
		}
		m.handleOpenAIChat(event, s)
	case constants.ViewUserLogsButtonCustomID:
		if !settings.IsReviewer(userID) {
			m.layout.logger.Error("Non-reviewer attempted to view logs", zap.Uint64("user_id", userID))
			m.layout.paginationManager.RespondWithError(event, "You do not have permission to view activity logs.")
			return
		}
		m.handleViewUserLogs(event, s)
	case constants.RecheckButtonCustomID:
		if !settings.IsReviewer(userID) {
			m.layout.logger.Error("Non-reviewer attempted to recheck user", zap.Uint64("user_id", userID))
			m.layout.paginationManager.RespondWithError(event, "You do not have permission to recheck users.")
			return
		}
		m.handleRecheck(event, s)
	case constants.ConfirmWithReasonButtonCustomID:
		if !settings.IsReviewer(userID) {
			m.layout.logger.Error("Non-reviewer attempted to use confirm with reason", zap.Uint64("user_id", userID))
			m.layout.paginationManager.RespondWithError(event, "You do not have permission to confirm users with custom reasons.")
			return
		}
		m.handleConfirmWithReason(event, s)
	case constants.ReviewModeOption:
		if !settings.IsReviewer(userID) {
			m.layout.logger.Error("Non-reviewer attempted to change review mode", zap.Uint64("user_id", userID))
			m.layout.paginationManager.RespondWithError(event, "You do not have permission to change review mode.")
			return
		}
		m.layout.settingLayout.ShowUpdate(event, s, constants.UserSettingPrefix, constants.ReviewModeOption)
	case constants.ReviewTargetModeOption:
		m.layout.settingLayout.ShowUpdate(event, s, constants.UserSettingPrefix, constants.ReviewTargetModeOption)
	}
}

// handleButton handles the buttons for the review menu.
func (m *ReviewMenu) handleButton(event *events.ComponentInteractionCreate, s *session.Session, customID string) {
	if m.checkCaptchaRequired(event, s) {
		return
	}

	switch customID {
	case constants.BackButtonCustomID:
		m.layout.paginationManager.NavigateBack(event, s, "")
	case constants.ConfirmButtonCustomID:
		m.handleConfirmUser(event, s)
	case constants.ClearButtonCustomID:
		m.handleClearUser(event, s)
	case constants.SkipButtonCustomID:
		m.handleSkipUser(event, s)
	}
}

// handleModal handles the modal for the review menu.
func (m *ReviewMenu) handleModal(event *events.ModalSubmitInteractionCreate, s *session.Session) {
	if m.checkCaptchaRequired(event, s) {
		return
	}

	switch event.Data.CustomID {
	case constants.ConfirmWithReasonModalCustomID:
		m.handleConfirmWithReasonModalSubmit(event, s)
	case constants.RecheckReasonModalCustomID:
		m.handleRecheckModalSubmit(event, s)
	}
}

// handleRecheck adds the user to the high priority queue for re-processing.
// If the user is already in queue, it shows the status menu instead.
func (m *ReviewMenu) handleRecheck(event *events.ComponentInteractionCreate, s *session.Session) {
	var user *types.ReviewUser
	s.GetInterface(constants.SessionKeyTarget, &user)

	// Check if user is already in queue to prevent duplicate entries
	status, _, _, err := m.layout.queueManager.GetQueueInfo(context.Background(), user.ID)
	if err == nil && status != "" {
		m.layout.statusMenu.Show(event, s)
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
		).
		Build()

	// Show modal to user
	if err := event.Modal(modal); err != nil {
		m.layout.logger.Error("Failed to create modal", zap.Error(err))
		m.layout.paginationManager.RespondWithError(event, "Failed to open the recheck reason for modal. Please try again.")
	}
}

// handleRecheckModalSubmit processes the custom recheck reason from the modal
// and performs the recheck with the provided reason.
func (m *ReviewMenu) handleRecheckModalSubmit(event *events.ModalSubmitInteractionCreate, s *session.Session) {
	var user *types.ReviewUser
	s.GetInterface(constants.SessionKeyTarget, &user)
	var settings *types.UserSetting
	s.GetInterface(constants.SessionKeyUserSettings, &settings)

	// Get and validate the recheck reason
	reason := event.Data.Text(constants.RecheckReasonInputCustomID)
	if reason == "" {
		m.Show(event, s, "Recheck reason cannot be empty. Please try again.")
		return
	}

	// Determine priority based on review mode
	priority := queue.HighPriority
	if settings.ReviewMode == enum.ReviewModeTraining {
		priority = queue.LowPriority
	}

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
		m.layout.paginationManager.RespondWithError(event, "Failed to add user to queue")
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
		m.layout.paginationManager.RespondWithError(event, "Failed to update queue info")
		return
	}

	// Track the queued user in session for status updates
	s.Set(constants.SessionKeyQueueUser, user.ID)

	m.layout.statusMenu.Show(event, s)
}

// handleViewUserLogs handles the shortcut to view user logs.
// It stores the user ID in session for log filtering and shows the logs menu.
func (m *ReviewMenu) handleViewUserLogs(event *events.ComponentInteractionCreate, s *session.Session) {
	var user *types.ReviewUser
	s.GetInterface(constants.SessionKeyTarget, &user)
	if user == nil {
		m.layout.logger.Error("No user selected to view logs")
		m.layout.paginationManager.RespondWithError(event, "No user selected to view logs.")
		return
	}

	// Set the user ID filter
	m.layout.logLayout.ResetLogs(s)
	m.layout.logLayout.ResetFilters(s)
	s.Set(constants.SessionKeyUserIDFilter, user.ID)

	// Show the logs menu
	m.layout.logLayout.Show(event, s)
}

// handleConfirmUser moves a user to the confirmed state and logs the action.
// After confirming, it loads a new user for review.
func (m *ReviewMenu) handleConfirmUser(event interfaces.CommonEvent, s *session.Session) {
	var settings *types.UserSetting
	s.GetInterface(constants.SessionKeyUserSettings, &settings)
	var botSettings *types.BotSetting
	s.GetInterface(constants.SessionKeyBotSettings, &botSettings)
	var user *types.ReviewUser
	s.GetInterface(constants.SessionKeyTarget, &user)

	var actionMsg string
	if settings.ReviewMode == enum.ReviewModeTraining {
		// Training mode - increment downvotes
		if err := m.layout.db.Reputation().UpdateUserVotes(context.Background(), user.ID, uint64(event.User().ID), false); err != nil {
			m.layout.logger.Error("Failed to update downvotes", zap.Error(err))
			m.layout.paginationManager.RespondWithError(event, "Failed to update downvotes. Please try again.")
			return
		}
		user.Reputation.Downvotes++
		actionMsg = "downvoted"

		// Log the training downvote action
		go m.layout.db.Activity().Log(context.Background(), &types.ActivityLog{
			ActivityTarget: types.ActivityTarget{
				UserID: user.ID,
			},
			ReviewerID:        uint64(event.User().ID),
			ActivityType:      enum.ActivityTypeUserTrainingDownvote,
			ActivityTimestamp: time.Now(),
			Details: map[string]interface{}{
				"upvotes":   user.Reputation.Upvotes,
				"downvotes": user.Reputation.Downvotes,
			},
		})
	} else {
		// Standard mode - check permissions and confirm user
		if !botSettings.IsReviewer(uint64(event.User().ID)) {
			m.layout.logger.Error("Non-reviewer attempted to confirm user",
				zap.Uint64("user_id", uint64(event.User().ID)))
			m.layout.paginationManager.RespondWithError(event, "You do not have permission to confirm users.")
			return
		}

		// Calculate vote percentages
		totalVotes := float64(user.Reputation.Upvotes + user.Reputation.Downvotes)
		if totalVotes >= constants.MinimumVotesRequired {
			upvotePercentage := float64(user.Reputation.Upvotes) / totalVotes

			// If there's a strong consensus for clearing, prevent confirmation
			if upvotePercentage >= constants.VoteConsensusThreshold {
				m.layout.paginationManager.NavigateTo(event, s, m.page,
					fmt.Sprintf("Cannot confirm - %.0f%% of %d votes indicate this user is safe",
						upvotePercentage*100, int(totalVotes)))
				return
			}
		}

		// Confirm the user
		if err := m.layout.db.Users().ConfirmUser(context.Background(), user); err != nil {
			m.layout.logger.Error("Failed to confirm user", zap.Error(err))
			m.layout.paginationManager.RespondWithError(event, "Failed to confirm the user. Please try again.")
			return
		}
		actionMsg = "confirmed"

		// Log the confirm action
		go m.layout.db.Activity().Log(context.Background(), &types.ActivityLog{
			ActivityTarget: types.ActivityTarget{
				UserID: user.ID,
			},
			ReviewerID:        uint64(event.User().ID),
			ActivityType:      enum.ActivityTypeUserConfirmed,
			ActivityTimestamp: time.Now(),
			Details:           map[string]interface{}{"reason": user.Reason},
		})
	}

	// Get the number of flagged users left to review
	flaggedCount, err := m.layout.db.Users().GetFlaggedUsersCount(context.Background())
	if err != nil {
		m.layout.logger.Error("Failed to get flagged users count", zap.Error(err))
	}

	// Clear current user and load next one
	s.Delete(constants.SessionKeyTarget)
	m.Show(event, s, fmt.Sprintf("User %s. %d users left to review.", actionMsg, flaggedCount))
	m.updateCounters(s)
}

// handleClearUser removes a user from the flagged state and logs the action.
// After clearing, it loads a new user for review.
func (m *ReviewMenu) handleClearUser(event interfaces.CommonEvent, s *session.Session) {
	var settings *types.UserSetting
	s.GetInterface(constants.SessionKeyUserSettings, &settings)
	var botSettings *types.BotSetting
	s.GetInterface(constants.SessionKeyBotSettings, &botSettings)
	var user *types.ReviewUser
	s.GetInterface(constants.SessionKeyTarget, &user)

	var actionMsg string
	if settings.ReviewMode == enum.ReviewModeTraining {
		// Training mode - increment upvotes
		if err := m.layout.db.Reputation().UpdateUserVotes(context.Background(), user.ID, uint64(event.User().ID), true); err != nil {
			m.layout.logger.Error("Failed to update upvotes", zap.Error(err))
			m.layout.paginationManager.RespondWithError(event, "Failed to update upvotes. Please try again.")
			return
		}
		user.Reputation.Upvotes++
		actionMsg = "upvoted"

		// Log the training upvote action
		go m.layout.db.Activity().Log(context.Background(), &types.ActivityLog{
			ActivityTarget: types.ActivityTarget{
				UserID: user.ID,
			},
			ReviewerID:        uint64(event.User().ID),
			ActivityType:      enum.ActivityTypeUserTrainingUpvote,
			ActivityTimestamp: time.Now(),
			Details: map[string]interface{}{
				"upvotes":   user.Reputation.Upvotes,
				"downvotes": user.Reputation.Downvotes,
			},
		})
	} else {
		// Standard mode - check permissions and clear user
		if !botSettings.IsReviewer(uint64(event.User().ID)) {
			m.layout.logger.Error("Non-reviewer attempted to clear user",
				zap.Uint64("user_id", uint64(event.User().ID)))
			m.layout.paginationManager.RespondWithError(event, "You do not have permission to clear users.")
			return
		}

		// Calculate vote percentages
		totalVotes := float64(user.Reputation.Upvotes + user.Reputation.Downvotes)
		if totalVotes >= constants.MinimumVotesRequired {
			downvotePercentage := float64(user.Reputation.Downvotes) / totalVotes

			// If there's a strong consensus for confirming, prevent clearing
			if downvotePercentage >= constants.VoteConsensusThreshold {
				m.layout.paginationManager.NavigateTo(event, s, m.page,
					fmt.Sprintf("Cannot clear - %.0f%% of %d votes indicate this user is suspicious",
						downvotePercentage*100, int(totalVotes)))
				return
			}
		}

		// Clear the user
		if err := m.layout.db.Users().ClearUser(context.Background(), user); err != nil {
			m.layout.logger.Error("Failed to clear user", zap.Error(err))
			m.layout.paginationManager.RespondWithError(event, "Failed to clear the user. Please try again.")
			return
		}
		actionMsg = "cleared"

		// Remove user from group tracking
		go m.layout.db.Tracking().RemoveUserFromGroups(context.Background(), user.ID, user.Groups)

		// Log the clear action
		go m.layout.db.Activity().Log(context.Background(), &types.ActivityLog{
			ActivityTarget: types.ActivityTarget{
				UserID: user.ID,
			},
			ReviewerID:        uint64(event.User().ID),
			ActivityType:      enum.ActivityTypeUserCleared,
			ActivityTimestamp: time.Now(),
			Details:           map[string]interface{}{},
		})
	}

	// Get the number of flagged users left to review
	flaggedCount, err := m.layout.db.Users().GetFlaggedUsersCount(context.Background())
	if err != nil {
		m.layout.logger.Error("Failed to get flagged users count", zap.Error(err))
	}

	// Clear current user and load next one
	s.Delete(constants.SessionKeyTarget)
	m.Show(event, s, fmt.Sprintf("User %s. %d users left to review.", actionMsg, flaggedCount))
	m.updateCounters(s)
}

// handleSkipUser logs the skip action and moves to the next user without
// changing the current user's status.
func (m *ReviewMenu) handleSkipUser(event interfaces.CommonEvent, s *session.Session) {
	var settings *types.UserSetting
	s.GetInterface(constants.SessionKeyUserSettings, &settings)

	// Check if skipping is allowed
	if msg := settings.SkipUsage.CanSkip(); msg != "" {
		m.layout.paginationManager.NavigateTo(event, s, m.page, msg)
		return
	}

	// Get the number of flagged users left to review
	flaggedCount, err := m.layout.db.Users().GetFlaggedUsersCount(context.Background())
	if err != nil {
		m.layout.logger.Error("Failed to get flagged users count", zap.Error(err))
	}

	// Clear current user and load next one
	s.Delete(constants.SessionKeyTarget)
	m.Show(event, s, fmt.Sprintf("Skipped user. %d users left to review.", flaggedCount))

	// Update skip and captcha counters
	var botSettings *types.BotSetting
	s.GetInterface(constants.SessionKeyBotSettings, &botSettings)

	settings.SkipUsage.IncrementSkips()
	settings.CaptchaUsage.IncrementReviews(settings, botSettings)
	if err := m.layout.db.Settings().SaveUserSettings(context.Background(), settings); err != nil {
		m.layout.logger.Error("Failed to update skip tracking", zap.Error(err))
		m.layout.paginationManager.RespondWithError(event, "Failed to update skip tracking. Please try again.")
		return
	}
	s.Set(constants.SessionKeyUserSettings, settings)

	// Log the skip action
	var user *types.ReviewUser
	s.GetInterface(constants.SessionKeyTarget, &user)

	m.layout.db.Activity().Log(context.Background(), &types.ActivityLog{
		ActivityTarget: types.ActivityTarget{
			UserID: user.ID,
		},
		ReviewerID:        uint64(event.User().ID),
		ActivityType:      enum.ActivityTypeUserSkipped,
		ActivityTimestamp: time.Now(),
		Details:           map[string]interface{}{},
	})
}

// handleOpenAIChat handles the button to open the AI chat for the current user.
// It adds a context message about the user and opens up the AI chat.
func (m *ReviewMenu) handleOpenAIChat(event *events.ComponentInteractionCreate, s *session.Session) {
	var target *types.ReviewUser
	s.GetInterface(constants.SessionKeyTarget, &target)

	// Create context message about the user
	context := fmt.Sprintf(`<context>
User Information:

Username: %s
Display Name: %s
Description: %s
Friends: %d
Groups: %d
Outfits: %d
Reason Flagged: %s
Confidence: %.2f</context>`,
		target.Name,
		target.DisplayName,
		target.Description,
		len(target.Friends),
		len(target.Groups),
		len(target.Outfits),
		target.Reason,
		target.Confidence,
	)

	// Update session and navigate to chat
	s.Set(constants.SessionKeyChatContext, context)
	s.Set(constants.SessionKeyPaginationPage, 0)
	m.layout.chatLayout.Show(event, s)
}

// handleConfirmWithReason opens a modal for entering a custom confirm reason.
// The modal pre-fills with the current reason if one exists.
func (m *ReviewMenu) handleConfirmWithReason(event *events.ComponentInteractionCreate, s *session.Session) {
	var user *types.ReviewUser
	s.GetInterface(constants.SessionKeyTarget, &user)

	// Create modal with pre-filled reason field
	modal := discord.NewModalCreateBuilder().
		SetCustomID(constants.ConfirmWithReasonModalCustomID).
		SetTitle("Confirm User with Reason").
		AddActionRow(
			discord.NewTextInput(constants.ConfirmReasonInputCustomID, discord.TextInputStyleParagraph, "Confirm Reason").
				WithRequired(true).
				WithPlaceholder("Enter the reason for confirming this user...").
				WithValue(user.Reason),
		).
		Build()

	// Show modal to user
	if err := event.Modal(modal); err != nil {
		m.layout.logger.Error("Failed to create modal", zap.Error(err))
		m.layout.paginationManager.RespondWithError(event, "Failed to open the confirm reason for modal. Please try again.")
	}
}

// handleConfirmWithReasonModalSubmit processes the custom confirm reason from the modal
// and performs the confirm with the provided reason.
func (m *ReviewMenu) handleConfirmWithReasonModalSubmit(event *events.ModalSubmitInteractionCreate, s *session.Session) {
	var user *types.ReviewUser
	s.GetInterface(constants.SessionKeyTarget, &user)

	// Get and validate the confirm reason
	reason := event.Data.Text(constants.ConfirmReasonInputCustomID)
	if reason == "" {
		m.layout.paginationManager.NavigateTo(event, s, m.page, "Confirm reason cannot be empty. Please try again.")
		return
	}

	// Update user's reason with the custom input
	user.Reason = reason

	// Update user status in database
	if err := m.layout.db.Users().ConfirmUser(context.Background(), user); err != nil {
		m.layout.logger.Error("Failed to confirm user", zap.Error(err))
		m.layout.paginationManager.RespondWithError(event, "Failed to confirm the user. Please try again.")
		return
	}

	// Clear current user and load next one
	s.Delete(constants.SessionKeyTarget)
	m.Show(event, s, "User confirmed.")
	m.updateCounters(s)

	// Log the custom confirm action
	m.layout.db.Activity().Log(context.Background(), &types.ActivityLog{
		ActivityTarget: types.ActivityTarget{
			UserID: user.ID,
		},
		ReviewerID:        uint64(event.User().ID),
		ActivityType:      enum.ActivityTypeUserConfirmedCustom,
		ActivityTimestamp: time.Now(),
		Details:           map[string]interface{}{"reason": user.Reason},
	})
}

// fetchNewTarget gets a new user to review based on the current sort order.
func (m *ReviewMenu) fetchNewTarget(event interfaces.CommonEvent, s *session.Session, reviewerID uint64) (*types.ReviewUser, bool, error) {
	var settings *types.UserSetting
	s.GetInterface(constants.SessionKeyUserSettings, &settings)

	// Check if user is banned for low accuracy
	isBanned, err := m.layout.db.Votes().CheckVoteAccuracy(context.Background(), uint64(event.User().ID))
	if err != nil {
		m.layout.logger.Error("Failed to check vote accuracy",
			zap.Error(err),
			zap.Uint64("user_id", uint64(event.User().ID)))
		// Continue anyway - not a big requirement
	}

	// Get the next user to review
	user, err := m.layout.db.Users().GetUserToReview(context.Background(), settings.UserDefaultSort, settings.ReviewTargetMode, reviewerID)
	if err != nil {
		return nil, isBanned, err
	}

	// Store the user in session for the message builder
	s.Set(constants.SessionKeyTarget, user)

	// Log the view action
	go m.layout.db.Activity().Log(context.Background(), &types.ActivityLog{
		ActivityTarget: types.ActivityTarget{
			UserID: user.ID,
		},
		ReviewerID:        reviewerID,
		ActivityType:      enum.ActivityTypeUserViewed,
		ActivityTimestamp: time.Now(),
		Details:           map[string]interface{}{},
	})

	return user, isBanned, nil
}

// checkCaptchaRequired checks if CAPTCHA verification is needed.
func (m *ReviewMenu) checkCaptchaRequired(event interfaces.CommonEvent, s *session.Session) bool {
	var settings *types.UserSetting
	s.GetInterface(constants.SessionKeyUserSettings, &settings)
	if settings.CaptchaUsage.NeedsCaptcha() {
		m.layout.captchaLayout.Show(event, s, "Please complete CAPTCHA verification to continue.")
		return true
	}
	return false
}

// updateCounters updates the review and skip counters.
func (m *ReviewMenu) updateCounters(s *session.Session) {
	var settings *types.UserSetting
	s.GetInterface(constants.SessionKeyUserSettings, &settings)
	var botSettings *types.BotSetting
	s.GetInterface(constants.SessionKeyBotSettings, &botSettings)

	settings.CaptchaUsage.IncrementReviews(settings, botSettings)
	settings.SkipUsage.ResetSkips()
	if err := m.layout.db.Settings().SaveUserSettings(context.Background(), settings); err != nil {
		m.layout.logger.Error("Failed to update counters", zap.Error(err))
	}
	s.Set(constants.SessionKeyUserSettings, settings)
}
