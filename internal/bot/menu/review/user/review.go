package user

import (
	"context"
	"fmt"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	builder "github.com/rotector/rotector/internal/bot/builder/review/user"
	"github.com/rotector/rotector/internal/bot/constants"
	"github.com/rotector/rotector/internal/bot/core/pagination"
	"github.com/rotector/rotector/internal/bot/core/session"
	"github.com/rotector/rotector/internal/bot/interfaces"
	"github.com/rotector/rotector/internal/common/queue"
	"github.com/rotector/rotector/internal/common/storage/database/types"
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
	if !settings.IsReviewer(uint64(event.User().ID)) && userSettings.ReviewMode != types.TrainingReviewMode {
		userSettings.ReviewMode = types.TrainingReviewMode
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
		var err error
		user, err = m.fetchNewTarget(s, uint64(event.User().ID))
		if err != nil {
			m.layout.logger.Error("Failed to fetch a new user", zap.Error(err))
			m.layout.paginationManager.RespondWithError(event, "Failed to fetch a new user. Please try again.")
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
	if m.checkLastViewed(event, s) {
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

	// Update user's default sort preference
	settings.UserDefaultSort = types.ReviewSortBy(option)
	if err := m.layout.db.Settings().SaveUserSettings(context.Background(), settings); err != nil {
		m.layout.logger.Error("Failed to save user settings", zap.Error(err))
		m.layout.paginationManager.RespondWithError(event, "Failed to save sort order. Please try again.")
		return
	}

	m.Show(event, s, "Changed sort order. Will take effect for the next user.")
}

// handleActionSelection processes action menu selections.
func (m *ReviewMenu) handleActionSelection(event *events.ComponentInteractionCreate, s *session.Session, option string) {
	// Get bot settings to check reviewer status
	var settings *types.BotSetting
	s.GetInterface(constants.SessionKeyBotSettings, &settings)
	userID := uint64(event.User().ID)

	switch option {
	case constants.OpenOutfitsMenuButtonCustomID:
		m.layout.outfitsMenu.Show(event, s, 0)
	case constants.OpenFriendsMenuButtonCustomID:
		m.layout.friendsMenu.Show(event, s, 0)
	case constants.OpenGroupsMenuButtonCustomID:
		m.layout.groupsMenu.Show(event, s, 0)
	case constants.OpenAIChatButtonCustomID:
		if !settings.IsReviewer(userID) {
			m.layout.logger.Error("Non-reviewer attempted to open AI chat", zap.Uint64("user_id", userID))
			m.layout.paginationManager.RespondWithError(event, "You do not have permission to open the AI chat.")
			return
		}
		m.handleOpenAIChat(event, s)
	case constants.ConfirmWithReasonButtonCustomID:
		if !settings.IsReviewer(userID) {
			m.layout.logger.Error("Non-reviewer attempted to use confirm with reason", zap.Uint64("user_id", userID))
			m.layout.paginationManager.RespondWithError(event, "You do not have permission to confirm users with custom reasons.")
			return
		}
		m.handleConfirmWithReason(event, s)
	case constants.RecheckButtonCustomID:
		if !settings.IsReviewer(userID) {
			m.layout.logger.Error("Non-reviewer attempted to recheck user", zap.Uint64("user_id", userID))
			m.layout.paginationManager.RespondWithError(event, "You do not have permission to recheck users.")
			return
		}
		m.handleRecheck(event, s)
	case constants.ViewUserLogsButtonCustomID:
		if !settings.IsReviewer(userID) {
			m.layout.logger.Error("Non-reviewer attempted to view logs", zap.Uint64("user_id", userID))
			m.layout.paginationManager.RespondWithError(event, "You do not have permission to view activity logs.")
			return
		}
		m.handleViewUserLogs(event, s)
	case constants.SwitchReviewModeCustomID:
		if !settings.IsReviewer(userID) {
			m.layout.logger.Error("Non-reviewer attempted to switch review mode", zap.Uint64("user_id", userID))
			m.layout.paginationManager.RespondWithError(event, "You do not have permission to switch review modes.")
			return
		}
		m.handleSwitchReviewMode(event, s)
	case constants.SwitchTargetModeCustomID:
		m.handleSwitchTargetMode(event, s)
	case constants.ReviewTargetModeOption:
		m.layout.settingLayout.ShowUpdate(event, s, constants.UserSettingPrefix, constants.ReviewTargetModeOption)
	case constants.ReviewModeOption:
		m.layout.settingLayout.ShowUpdate(event, s, constants.UserSettingPrefix, constants.ReviewModeOption)
	}
}

// handleButton handles the buttons for the review menu.
func (m *ReviewMenu) handleButton(event *events.ComponentInteractionCreate, s *session.Session, customID string) {
	if m.checkLastViewed(event, s) {
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
	if m.checkLastViewed(event, s) {
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
	if settings.ReviewMode == types.TrainingReviewMode {
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
	m.layout.logLayout.ResetFilters(s)
	s.Set(constants.SessionKeyUserIDFilter, user.ID)

	// Show the logs menu
	m.layout.logLayout.Show(event, s)
}

// handleConfirmUser moves a user to the confirmed state and logs the action.
// After confirming, it loads a new user for review.
func (m *ReviewMenu) handleConfirmUser(event interfaces.CommonEvent, s *session.Session) {
	var user *types.ReviewUser
	s.GetInterface(constants.SessionKeyTarget, &user)

	var settings *types.UserSetting
	s.GetInterface(constants.SessionKeyUserSettings, &settings)

	var actionMsg string
	if settings.ReviewMode == types.TrainingReviewMode {
		// Training mode - increment downvotes
		if err := m.layout.db.Users().UpdateTrainingVotes(context.Background(), user.ID, false); err != nil {
			m.layout.logger.Error("Failed to update downvotes", zap.Error(err))
			m.layout.paginationManager.RespondWithError(event, "Failed to update downvotes. Please try again.")
			return
		}
		user.Downvotes++
		actionMsg = "downvoted"

		// Log the training downvote action
		go m.layout.db.UserActivity().Log(context.Background(), &types.UserActivityLog{
			ActivityTarget: types.ActivityTarget{
				UserID: user.ID,
			},
			ReviewerID:        uint64(event.User().ID),
			ActivityType:      types.ActivityTypeUserTrainingDownvote,
			ActivityTimestamp: time.Now(),
			Details: map[string]interface{}{
				"upvotes":   user.Upvotes,
				"downvotes": user.Downvotes,
			},
		})
	} else {
		// Standard mode - confirm user
		if err := m.layout.db.Users().ConfirmUser(context.Background(), user); err != nil {
			m.layout.logger.Error("Failed to confirm user", zap.Error(err))
			m.layout.paginationManager.RespondWithError(event, "Failed to confirm the user. Please try again.")
			return
		}
		actionMsg = "confirmed"

		// Log the confirm action
		go m.layout.db.UserActivity().Log(context.Background(), &types.UserActivityLog{
			ActivityTarget: types.ActivityTarget{
				UserID: user.ID,
			},
			ReviewerID:        uint64(event.User().ID),
			ActivityType:      types.ActivityTypeUserConfirmed,
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
}

// handleClearUser removes a user from the flagged state and logs the action.
// After clearing, it loads a new user for review.
func (m *ReviewMenu) handleClearUser(event interfaces.CommonEvent, s *session.Session) {
	var user *types.ReviewUser
	s.GetInterface(constants.SessionKeyTarget, &user)
	var settings *types.UserSetting
	s.GetInterface(constants.SessionKeyUserSettings, &settings)

	var actionMsg string
	if settings.ReviewMode == types.TrainingReviewMode {
		// Training mode - increment upvotes
		if err := m.layout.db.Users().UpdateTrainingVotes(context.Background(), user.ID, true); err != nil {
			m.layout.logger.Error("Failed to update upvotes", zap.Error(err))
			m.layout.paginationManager.RespondWithError(event, "Failed to update upvotes. Please try again.")
			return
		}
		user.Upvotes++
		actionMsg = "upvoted"

		// Log the training upvote action
		go m.layout.db.UserActivity().Log(context.Background(), &types.UserActivityLog{
			ActivityTarget: types.ActivityTarget{
				UserID: user.ID,
			},
			ReviewerID:        uint64(event.User().ID),
			ActivityType:      types.ActivityTypeUserTrainingUpvote,
			ActivityTimestamp: time.Now(),
			Details: map[string]interface{}{
				"upvotes":   user.Upvotes,
				"downvotes": user.Downvotes,
			},
		})
	} else {
		// Standard mode - clear user
		if err := m.layout.db.Users().ClearUser(context.Background(), user); err != nil {
			m.layout.logger.Error("Failed to clear user", zap.Error(err))
			m.layout.paginationManager.RespondWithError(event, "Failed to clear the user. Please try again.")
			return
		}
		actionMsg = "cleared"

		// Remove user from group tracking
		go m.layout.db.Tracking().RemoveUserFromGroups(context.Background(), user.ID, user.Groups)

		// Log the clear action
		go m.layout.db.UserActivity().Log(context.Background(), &types.UserActivityLog{
			ActivityTarget: types.ActivityTarget{
				UserID: user.ID,
			},
			ReviewerID:        uint64(event.User().ID),
			ActivityType:      types.ActivityTypeUserCleared,
			ActivityTimestamp: time.Now(),
			Details:           make(map[string]interface{}),
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
}

// handleSkipUser logs the skip action and moves to the next user without
// changing the current user's status.
func (m *ReviewMenu) handleSkipUser(event interfaces.CommonEvent, s *session.Session) {
	var user *types.ReviewUser
	s.GetInterface(constants.SessionKeyTarget, &user)

	// Log the skip action asynchronously
	go m.layout.db.UserActivity().Log(context.Background(), &types.UserActivityLog{
		ActivityTarget: types.ActivityTarget{
			UserID: user.ID,
		},
		ReviewerID:        uint64(event.User().ID),
		ActivityType:      types.ActivityTypeUserSkipped,
		ActivityTimestamp: time.Now(),
		Details:           make(map[string]interface{}),
	})

	// Get the number of flagged users left to review
	flaggedCount, err := m.layout.db.Users().GetFlaggedUsersCount(context.Background())
	if err != nil {
		m.layout.logger.Error("Failed to get flagged users count", zap.Error(err))
	}

	// Clear current user and load next one
	s.Delete(constants.SessionKeyTarget)
	m.Show(event, s, fmt.Sprintf("Skipped user. %d users left to review.", flaggedCount))
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

	// Log the custom confirm action asynchronously
	go m.layout.db.UserActivity().Log(context.Background(), &types.UserActivityLog{
		ActivityTarget: types.ActivityTarget{
			UserID: user.ID,
		},
		ReviewerID:        uint64(event.User().ID),
		ActivityType:      types.ActivityTypeUserConfirmedCustom,
		ActivityTimestamp: time.Now(),
		Details:           map[string]interface{}{"reason": user.Reason},
	})

	// Clear current user and load next one
	s.Delete(constants.SessionKeyTarget)
	m.Show(event, s, "User confirmed.")
}

// handleSwitchReviewMode switches between training and standard review modes.
func (m *ReviewMenu) handleSwitchReviewMode(event *events.ComponentInteractionCreate, s *session.Session) {
	var settings *types.UserSetting
	s.GetInterface(constants.SessionKeyUserSettings, &settings)

	// Toggle between modes
	if settings.ReviewMode == types.TrainingReviewMode {
		settings.ReviewMode = types.StandardReviewMode
	} else {
		settings.ReviewMode = types.TrainingReviewMode
	}

	// Save the updated setting
	if err := m.layout.db.Settings().SaveUserSettings(context.Background(), settings); err != nil {
		m.layout.logger.Error("Failed to save review mode setting", zap.Error(err))
		m.layout.paginationManager.RespondWithError(event, "Failed to switch review mode. Please try again.")
		return
	}

	// Update session and refresh the menu
	s.Set(constants.SessionKeyUserSettings, settings)
	m.Show(event, s, "Switched to "+settings.ReviewMode.FormatDisplay())
}

// handleSwitchTargetMode switches between reviewing flagged items and re-reviewing confirmed items.
func (m *ReviewMenu) handleSwitchTargetMode(event *events.ComponentInteractionCreate, s *session.Session) {
	var settings *types.UserSetting
	s.GetInterface(constants.SessionKeyUserSettings, &settings)

	// Toggle between modes
	if settings.ReviewTargetMode == types.FlaggedReviewTarget {
		settings.ReviewTargetMode = types.ConfirmedReviewTarget
	} else {
		settings.ReviewTargetMode = types.FlaggedReviewTarget
	}

	// Save the updated setting
	if err := m.layout.db.Settings().SaveUserSettings(context.Background(), settings); err != nil {
		m.layout.logger.Error("Failed to save target mode setting", zap.Error(err))
		m.layout.paginationManager.RespondWithError(event, "Failed to switch target mode. Please try again.")
		return
	}

	// Update session and refresh the menu
	s.Set(constants.SessionKeyUserSettings, settings)
	m.Show(event, s, "Switched to "+settings.ReviewTargetMode.FormatDisplay())
}

// fetchNewTarget gets a new user to review based on the current sort order.
func (m *ReviewMenu) fetchNewTarget(s *session.Session, reviewerID uint64) (*types.ReviewUser, error) {
	var settings *types.UserSetting
	s.GetInterface(constants.SessionKeyUserSettings, &settings)

	// Get the next user to review
	user, err := m.layout.db.Users().GetUserToReview(context.Background(), settings.UserDefaultSort, settings.ReviewTargetMode)
	if err != nil {
		return nil, err
	}

	// Store the user in session for the message builder
	s.Set(constants.SessionKeyTarget, user)

	// Log the view action
	go m.layout.db.UserActivity().Log(context.Background(), &types.UserActivityLog{
		ActivityTarget: types.ActivityTarget{
			UserID: user.ID,
		},
		ReviewerID:        reviewerID,
		ActivityType:      types.ActivityTypeUserViewed,
		ActivityTimestamp: time.Now(),
		Details:           make(map[string]interface{}),
	})

	return user, nil
}

// checkLastViewed checks if the current target user has timed out and needs to be refreshed.
// Clears the current user and loads a new one if the timeout is detected.
func (m *ReviewMenu) checkLastViewed(event interfaces.CommonEvent, s *session.Session) bool {
	var user *types.ReviewUser
	s.GetInterface(constants.SessionKeyTarget, &user)

	// Check if more than 10 minutes have passed since last view
	if user != nil && time.Since(user.LastViewed) > 10*time.Minute {
		// Clear current user and load new one
		s.Delete(constants.SessionKeyTarget)
		m.Show(event, s, "Previous review timed out after 10 minutes of inactivity. Showing new user.")
		return true
	}

	return false
}
