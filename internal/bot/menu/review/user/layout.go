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
	"github.com/rotector/rotector/internal/common/storage/database/models"
	"github.com/rotector/rotector/internal/common/translator"
	"go.uber.org/zap"
)

// Menu handles the main review interface where moderators can view and take
// action on flagged users.
type Menu struct {
	handler *Handler
	page    *pagination.Page
}

// NewMenu creates a Menu and sets up its page with message builders and
// interaction handlers. The page is configured to show user information
// and handle review actions.
func NewMenu(h *Handler) *Menu {
	// Create translator for handling non-English descriptions
	translator := translator.New(h.roAPI.GetClient())

	m := Menu{handler: h}
	m.page = &pagination.Page{
		Name: "Review Menu",
		Message: func(s *session.Session) *discord.MessageUpdateBuilder {
			return builder.NewReviewBuilder(s, translator, h.db).Build()
		},
		SelectHandlerFunc: m.handleSelectMenu,
		ButtonHandlerFunc: m.handleButton,
		ModalHandlerFunc:  m.handleModal,
	}
	return &m
}

// ShowReviewMenu prepares and displays the review interface.
func (m *Menu) ShowReviewMenu(event interfaces.CommonEvent, s *session.Session, content string) {
	var settings *models.BotSetting
	s.GetInterface(constants.SessionKeyBotSettings, &settings)
	var userSettings *models.UserSetting
	s.GetInterface(constants.SessionKeyUserSettings, &userSettings)

	// Force training mode if user is not a reviewer
	if !settings.IsReviewer(uint64(event.User().ID)) && userSettings.ReviewMode != models.TrainingReviewMode {
		userSettings.ReviewMode = models.TrainingReviewMode
		if err := m.handler.db.Settings().SaveUserSettings(context.Background(), userSettings); err != nil {
			m.handler.logger.Error("Failed to enforce training mode", zap.Error(err))
		}
		s.Set(constants.SessionKeyUserSettings, userSettings)
	}

	var user *models.FlaggedUser
	s.GetInterface(constants.SessionKeyTarget, &user)

	// If no user is set in session, fetch a new one
	if user == nil {
		var err error
		user, err = m.fetchNewTarget(s, uint64(event.User().ID))
		if err != nil {
			m.handler.logger.Error("Failed to fetch a new user", zap.Error(err))
			m.handler.paginationManager.RespondWithError(event, "Failed to fetch a new user. Please try again.")
			return
		}
	}

	// Fetch follow counts for the user
	followCounts, err := m.handler.followFetcher.FetchUserFollowCounts(user.ID)
	if err != nil {
		m.handler.logger.Error("Failed to fetch follow counts", zap.Error(err))
	}

	// Check friend status and get friend data by looking up each friend in the database
	flaggedFriends := make(map[uint64]*models.User)
	friendTypes := make(map[uint64]string)
	if len(user.Friends) > 0 {
		// Extract friend IDs for batch lookup
		friendIDs := make([]uint64, len(user.Friends))
		for i, friend := range user.Friends {
			friendIDs[i] = friend.ID
		}

		// Get full user data and types for friends that exist in the database
		var err error
		flaggedFriends, friendTypes, err = m.handler.db.Users().GetUsersByIDs(context.Background(), friendIDs)
		if err != nil {
			m.handler.logger.Error("Failed to get friend data", zap.Error(err))
			return
		}
	}

	// Store data in session for the message builder
	s.Set(constants.SessionKeyFlaggedFriends, flaggedFriends)
	s.Set(constants.SessionKeyFriendTypes, friendTypes)
	s.Set(constants.SessionKeyFollowCounts, followCounts)

	m.handler.paginationManager.NavigateTo(event, s, m.page, content)
}

// handleSelectMenu processes select menu interactions.
func (m *Menu) handleSelectMenu(event *events.ComponentInteractionCreate, s *session.Session, customID string, option string) {
	switch customID {
	case constants.SortOrderSelectMenuCustomID:
		m.handleSortOrderSelection(event, s, option)
	case constants.ActionSelectMenuCustomID:
		m.handleActionSelection(event, s, option)
	}
}

// handleSortOrderSelection processes sort order menu selections.
func (m *Menu) handleSortOrderSelection(event *events.ComponentInteractionCreate, s *session.Session, option string) {
	// Retrieve user settings from session
	var settings *models.UserSetting
	s.GetInterface(constants.SessionKeyUserSettings, &settings)

	// Update user's default sort preference
	settings.UserDefaultSort = option
	if err := m.handler.db.Settings().SaveUserSettings(context.Background(), settings); err != nil {
		m.handler.logger.Error("Failed to save user settings", zap.Error(err))
		m.handler.paginationManager.RespondWithError(event, "Failed to save sort order. Please try again.")
		return
	}

	m.ShowReviewMenu(event, s, "Changed sort order. Will take effect for the next user.")
}

// handleActionSelection processes action menu selections.
func (m *Menu) handleActionSelection(event *events.ComponentInteractionCreate, s *session.Session, option string) {
	// Get bot settings to check reviewer status
	var settings *models.BotSetting
	s.GetInterface(constants.SessionKeyBotSettings, &settings)
	userID := uint64(event.User().ID)

	switch option {
	case constants.OpenOutfitsMenuButtonCustomID:
		m.handler.outfitsMenu.ShowOutfitsMenu(event, s, 0)
	case constants.OpenFriendsMenuButtonCustomID:
		m.handler.friendsMenu.ShowFriendsMenu(event, s, 0)
	case constants.OpenGroupsMenuButtonCustomID:
		m.handler.groupsMenu.ShowGroupsMenu(event, s, 0)

	case constants.ConfirmWithReasonButtonCustomID:
		if !settings.IsReviewer(userID) {
			m.handler.logger.Error("Non-reviewer attempted to use confirm with reason", zap.Uint64("user_id", userID))
			m.handler.paginationManager.RespondWithError(event, "You do not have permission to confirm users with custom reasons.")
			return
		}
		m.handleConfirmWithReason(event, s)

	case constants.RecheckButtonCustomID:
		if !settings.IsReviewer(userID) {
			m.handler.logger.Error("Non-reviewer attempted to recheck user", zap.Uint64("user_id", userID))
			m.handler.paginationManager.RespondWithError(event, "You do not have permission to recheck users.")
			return
		}
		m.handleRecheck(event, s)

	case constants.ViewUserLogsButtonCustomID:
		if !settings.IsReviewer(userID) {
			m.handler.logger.Error("Non-reviewer attempted to view logs", zap.Uint64("user_id", userID))
			m.handler.paginationManager.RespondWithError(event, "You do not have permission to view activity logs.")
			return
		}
		m.handleViewUserLogs(event, s)

	case constants.SwitchReviewModeCustomID:
		if !settings.IsReviewer(userID) {
			m.handler.logger.Error("Non-reviewer attempted to switch review mode", zap.Uint64("user_id", userID))
			m.handler.paginationManager.RespondWithError(event, "You do not have permission to switch review modes.")
			return
		}
		m.handleSwitchReviewMode(event, s)
	}
}

// handleButton handles the buttons for the review menu.
func (m *Menu) handleButton(event *events.ComponentInteractionCreate, s *session.Session, customID string) {
	if m.checkLastViewed(event, s) {
		return
	}

	switch customID {
	case constants.BackButtonCustomID:
		m.handler.dashboardHandler.ShowDashboard(event, s, "")
	case constants.ConfirmButtonCustomID:
		m.handleConfirmUser(event, s)
	case constants.ClearButtonCustomID:
		m.handleClearUser(event, s)
	case constants.SkipButtonCustomID:
		m.handleSkipUser(event, s)
	}
}

// handleModal handles the modal for the review menu.
func (m *Menu) handleModal(event *events.ModalSubmitInteractionCreate, s *session.Session) {
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
func (m *Menu) handleRecheck(event *events.ComponentInteractionCreate, s *session.Session) {
	var user *models.FlaggedUser
	s.GetInterface(constants.SessionKeyTarget, &user)

	// Check if user is already in queue to prevent duplicate entries
	status, _, _, err := m.handler.queueManager.GetQueueInfo(context.Background(), user.ID)
	if err == nil && status != "" {
		m.handler.statusMenu.ShowStatusMenu(event, s)
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
		m.handler.logger.Error("Failed to create modal", zap.Error(err))
		m.handler.paginationManager.RespondWithError(event, "Failed to open the recheck reason form. Please try again.")
	}
}

// handleRecheckModalSubmit processes the custom recheck reason from the modal
// and performs the recheck with the provided reason.
func (m *Menu) handleRecheckModalSubmit(event *events.ModalSubmitInteractionCreate, s *session.Session) {
	var user *models.FlaggedUser
	s.GetInterface(constants.SessionKeyTarget, &user)
	var settings *models.UserSetting
	s.GetInterface(constants.SessionKeyUserSettings, &settings)

	// Get and validate the recheck reason
	reason := event.Data.Text(constants.RecheckReasonInputCustomID)
	if reason == "" {
		m.ShowReviewMenu(event, s, "Recheck reason cannot be empty. Please try again.")
		return
	}

	// Determine priority based on review mode
	priority := queue.HighPriority
	if settings.ReviewMode == models.TrainingReviewMode {
		priority = queue.LowPriority
	}

	// Add to queue with reviewer information
	err := m.handler.queueManager.AddToQueue(context.Background(), &queue.Item{
		UserID:      user.ID,
		Priority:    priority,
		Reason:      reason,
		AddedBy:     uint64(event.User().ID),
		AddedAt:     time.Now(),
		Status:      queue.StatusPending,
		CheckExists: true,
	})
	if err != nil {
		m.handler.paginationManager.RespondWithError(event, "Failed to add user to queue")
		return
	}

	// Store queue position information for status display
	err = m.handler.queueManager.SetQueueInfo(
		context.Background(),
		user.ID,
		queue.StatusPending,
		priority,
		m.handler.queueManager.GetQueueLength(context.Background(), priority),
	)
	if err != nil {
		m.handler.paginationManager.RespondWithError(event, "Failed to update queue info")
		return
	}

	// Track the queued user in session for status updates
	s.Set(constants.SessionKeyQueueUser, user.ID)

	m.handler.statusMenu.ShowStatusMenu(event, s)
}

// handleViewUserLogs handles the shortcut to view user logs.
// It stores the user ID in session for log filtering and shows the logs menu.
func (m *Menu) handleViewUserLogs(event *events.ComponentInteractionCreate, s *session.Session) {
	var user *models.FlaggedUser
	s.GetInterface(constants.SessionKeyTarget, &user)
	if user == nil {
		m.handler.paginationManager.RespondWithError(event, "No user selected to view logs.")
		return
	}

	// Set the user ID filter
	m.handler.logHandler.ResetFilters(s)
	s.Set(constants.SessionKeyUserIDFilter, user.ID)

	// Show the logs menu
	m.handler.logHandler.ShowLogMenu(event, s)
}

// handleConfirmUser moves a user to the confirmed state and logs the action.
// After confirming, it loads a new user for review.
func (m *Menu) handleConfirmUser(event interfaces.CommonEvent, s *session.Session) {
	var user *models.FlaggedUser
	s.GetInterface(constants.SessionKeyTarget, &user)

	var settings *models.UserSetting
	s.GetInterface(constants.SessionKeyUserSettings, &settings)

	var actionMsg string
	if settings.ReviewMode == models.TrainingReviewMode {
		// Training mode - increment downvotes
		if err := m.handler.db.Users().UpdateTrainingVotes(context.Background(), user.ID, false); err != nil {
			m.handler.paginationManager.RespondWithError(event, "Failed to update downvotes. Please try again.")
			return
		}
		user.Downvotes++
		actionMsg = "downvoted"

		// Log the training downvote action
		go m.handler.db.UserActivity().LogActivity(context.Background(), &models.UserActivityLog{
			ActivityTarget: models.ActivityTarget{
				UserID: user.ID,
			},
			ReviewerID:        uint64(event.User().ID),
			ActivityType:      models.ActivityTypeUserTrainingDownvote,
			ActivityTimestamp: time.Now(),
			Details: map[string]interface{}{
				"upvotes":   user.Upvotes,
				"downvotes": user.Downvotes,
			},
		})
	} else {
		// Standard mode - confirm user
		if err := m.handler.db.Users().ConfirmUser(context.Background(), user); err != nil {
			m.handler.logger.Error("Failed to confirm user", zap.Error(err))
			m.handler.paginationManager.RespondWithError(event, "Failed to confirm the user. Please try again.")
			return
		}
		actionMsg = "confirmed"

		// Log the confirm action
		go m.handler.db.UserActivity().LogActivity(context.Background(), &models.UserActivityLog{
			ActivityTarget: models.ActivityTarget{
				UserID: user.ID,
			},
			ReviewerID:        uint64(event.User().ID),
			ActivityType:      models.ActivityTypeUserConfirmed,
			ActivityTimestamp: time.Now(),
			Details:           map[string]interface{}{"reason": user.Reason},
		})
	}

	// Get the number of flagged users left to review
	flaggedCount, err := m.handler.db.Users().GetFlaggedUsersCount(context.Background())
	if err != nil {
		m.handler.logger.Error("Failed to get flagged users count", zap.Error(err))
	}

	// Clear current user and load next one
	s.Delete(constants.SessionKeyTarget)
	m.ShowReviewMenu(event, s, fmt.Sprintf("User %s. %d users left to review.", actionMsg, flaggedCount))
}

// handleClearUser removes a user from the flagged state and logs the action.
// After clearing, it loads a new user for review.
func (m *Menu) handleClearUser(event interfaces.CommonEvent, s *session.Session) {
	var user *models.FlaggedUser
	s.GetInterface(constants.SessionKeyTarget, &user)

	var settings *models.UserSetting
	s.GetInterface(constants.SessionKeyUserSettings, &settings)

	var actionMsg string
	if settings.ReviewMode == models.TrainingReviewMode {
		// Training mode - increment upvotes
		if err := m.handler.db.Users().UpdateTrainingVotes(context.Background(), user.ID, true); err != nil {
			m.handler.paginationManager.RespondWithError(event, "Failed to update upvotes. Please try again.")
			return
		}
		user.Upvotes++
		actionMsg = "upvoted"

		// Log the training upvote action
		go m.handler.db.UserActivity().LogActivity(context.Background(), &models.UserActivityLog{
			ActivityTarget: models.ActivityTarget{
				UserID: user.ID,
			},
			ReviewerID:        uint64(event.User().ID),
			ActivityType:      models.ActivityTypeUserTrainingUpvote,
			ActivityTimestamp: time.Now(),
			Details: map[string]interface{}{
				"upvotes":   user.Upvotes,
				"downvotes": user.Downvotes,
			},
		})
	} else {
		// Standard mode - clear user
		if err := m.handler.db.Users().ClearUser(context.Background(), user); err != nil {
			m.handler.logger.Error("Failed to clear user", zap.Error(err))
			m.handler.paginationManager.RespondWithError(event, "Failed to clear the user. Please try again.")
			return
		}
		actionMsg = "cleared"

		// Log the clear action
		go m.handler.db.UserActivity().LogActivity(context.Background(), &models.UserActivityLog{
			ActivityTarget: models.ActivityTarget{
				UserID: user.ID,
			},
			ReviewerID:        uint64(event.User().ID),
			ActivityType:      models.ActivityTypeUserCleared,
			ActivityTimestamp: time.Now(),
			Details:           make(map[string]interface{}),
		})
	}

	// Get the number of flagged users left to review
	flaggedCount, err := m.handler.db.Users().GetFlaggedUsersCount(context.Background())
	if err != nil {
		m.handler.logger.Error("Failed to get flagged users count", zap.Error(err))
	}

	// Clear current user and load next one
	s.Delete(constants.SessionKeyTarget)
	m.ShowReviewMenu(event, s, fmt.Sprintf("User %s. %d users left to review.", actionMsg, flaggedCount))
}

// handleSkipUser logs the skip action and moves to the next user without
// changing the current user's status.
func (m *Menu) handleSkipUser(event interfaces.CommonEvent, s *session.Session) {
	var user *models.FlaggedUser
	s.GetInterface(constants.SessionKeyTarget, &user)

	// Log the skip action asynchronously
	go m.handler.db.UserActivity().LogActivity(context.Background(), &models.UserActivityLog{
		ActivityTarget: models.ActivityTarget{
			UserID: user.ID,
		},
		ReviewerID:        uint64(event.User().ID),
		ActivityType:      models.ActivityTypeUserSkipped,
		ActivityTimestamp: time.Now(),
		Details:           make(map[string]interface{}),
	})

	// Get the number of flagged users left to review
	flaggedCount, err := m.handler.db.Users().GetFlaggedUsersCount(context.Background())
	if err != nil {
		m.handler.logger.Error("Failed to get flagged users count", zap.Error(err))
	}

	// Clear current user and load next one
	s.Delete(constants.SessionKeyTarget)
	m.ShowReviewMenu(event, s, fmt.Sprintf("Skipped user. %d users left to review.", flaggedCount))
}

// handleConfirmWithReason opens a modal for entering a custom confirm reason.
// The modal pre-fills with the current reason if one exists.
func (m *Menu) handleConfirmWithReason(event *events.ComponentInteractionCreate, s *session.Session) {
	var user *models.FlaggedUser
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
		m.handler.logger.Error("Failed to create modal", zap.Error(err))
		m.handler.paginationManager.RespondWithError(event, "Failed to open the confirm reason form. Please try again.")
	}
}

// handleConfirmWithReasonModalSubmit processes the custom confirm reason from the modal
// and performs the confirm with the provided reason.
func (m *Menu) handleConfirmWithReasonModalSubmit(event *events.ModalSubmitInteractionCreate, s *session.Session) {
	var user *models.FlaggedUser
	s.GetInterface(constants.SessionKeyTarget, &user)

	// Get and validate the confirm reason
	reason := event.Data.Text(constants.ConfirmReasonInputCustomID)
	if reason == "" {
		m.handler.paginationManager.RespondWithError(event, "Confirm reason cannot be empty. Please try again.")
		return
	}

	// Update user's reason with the custom input
	user.Reason = reason

	// Update user status in database
	if err := m.handler.db.Users().ConfirmUser(context.Background(), user); err != nil {
		m.handler.logger.Error("Failed to confirm user", zap.Error(err))
		m.handler.paginationManager.RespondWithError(event, "Failed to confirm the user. Please try again.")
		return
	}

	// Log the custom confirm action asynchronously
	go m.handler.db.UserActivity().LogActivity(context.Background(), &models.UserActivityLog{
		ActivityTarget: models.ActivityTarget{
			UserID: user.ID,
		},
		ReviewerID:        uint64(event.User().ID),
		ActivityType:      models.ActivityTypeUserConfirmedCustom,
		ActivityTimestamp: time.Now(),
		Details:           map[string]interface{}{"reason": user.Reason},
	})

	// Clear current user and load next one
	s.Delete(constants.SessionKeyTarget)
	m.ShowReviewMenu(event, s, "User confirmed.")
}

// handleSwitchReviewMode switches between training and standard review modes.
func (m *Menu) handleSwitchReviewMode(event *events.ComponentInteractionCreate, s *session.Session) {
	var settings *models.UserSetting
	s.GetInterface(constants.SessionKeyUserSettings, &settings)

	// Toggle between modes
	if settings.ReviewMode == models.TrainingReviewMode {
		settings.ReviewMode = models.StandardReviewMode
	} else {
		settings.ReviewMode = models.TrainingReviewMode
	}

	// Save the updated setting
	if err := m.handler.db.Settings().SaveUserSettings(context.Background(), settings); err != nil {
		m.handler.logger.Error("Failed to save review mode setting", zap.Error(err))
		m.handler.paginationManager.RespondWithError(event, "Failed to switch review mode. Please try again.")
		return
	}

	// Update session and refresh the menu
	s.Set(constants.SessionKeyUserSettings, settings)
	m.ShowReviewMenu(event, s, "Switched to "+models.FormatReviewMode(settings.ReviewMode))
}

// fetchNewTarget gets a new user to review based on the current sort order.
// It logs the view action and stores the user in the session.
func (m *Menu) fetchNewTarget(s *session.Session, reviewerID uint64) (*models.FlaggedUser, error) {
	// Retrieve user settings from session
	var settings *models.UserSetting
	s.GetInterface(constants.SessionKeyUserSettings, &settings)

	// Get the sort order from user settings
	sortBy := settings.UserDefaultSort

	// Get the next user to review
	user, err := m.handler.db.Users().GetFlaggedUserToReview(context.Background(), sortBy)
	if err != nil {
		return nil, err
	}

	// Store the user in session for the message builder
	s.Set(constants.SessionKeyTarget, user)

	// Log the view action asynchronously
	go m.handler.db.UserActivity().LogActivity(context.Background(), &models.UserActivityLog{
		ActivityTarget: models.ActivityTarget{
			UserID: user.ID,
		},
		ReviewerID:        reviewerID,
		ActivityType:      models.ActivityTypeUserViewed,
		ActivityTimestamp: time.Now(),
		Details:           make(map[string]interface{}),
	})

	return user, nil
}

// checkLastViewed checks if the current target user has timed out and needs to be refreshed.
// Clears the current user and loads a new one if the timeout is detected.
func (m *Menu) checkLastViewed(event interfaces.CommonEvent, s *session.Session) bool {
	var user *models.FlaggedUser
	s.GetInterface(constants.SessionKeyTarget, &user)

	// Check if more than 10 minutes have passed since last view
	if user != nil && time.Since(user.LastViewed) > 10*time.Minute {
		// Clear current user and load new one
		s.Delete(constants.SessionKeyTarget)
		m.ShowReviewMenu(event, s, "Previous review timed out after 10 minutes of inactivity. Showing new user.")
		return true
	}

	return false
}
