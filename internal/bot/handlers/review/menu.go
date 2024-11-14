package review

import (
	"context"
	"fmt"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/rotector/rotector/internal/bot/constants"
	"github.com/rotector/rotector/internal/bot/handlers/review/builders"
	"github.com/rotector/rotector/internal/bot/interfaces"
	"github.com/rotector/rotector/internal/bot/pagination"
	"github.com/rotector/rotector/internal/bot/session"
	"github.com/rotector/rotector/internal/common/database"
	"github.com/rotector/rotector/internal/common/queue"
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
			return builders.NewReviewEmbed(s, translator, h.db).Build()
		},
		SelectHandlerFunc: m.handleSelectMenu,
		ButtonHandlerFunc: m.handleButton,
		ModalHandlerFunc:  m.handleModal,
	}
	return &m
}

// ShowReviewMenu prepares and displays the review interface.
func (m *Menu) ShowReviewMenu(event interfaces.CommonEvent, s *session.Session, content string) {
	var user *database.FlaggedUser
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

	// Check friend status and get friend data by looking up each friend in the database
	flaggedFriends := make(map[uint64]*database.User)
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

	m.handler.paginationManager.NavigateTo(event, s, m.page, content)
}

// handleSelectMenu handles the select menu for the review menu.
func (m *Menu) handleSelectMenu(event *events.ComponentInteractionCreate, s *session.Session, customID string, option string) {
	if m.checkLastViewed(event, s) {
		return
	}

	switch customID {
	case constants.SortOrderSelectMenuCustomID:
		// Retrieve user settings from session
		var settings *database.UserSetting
		s.GetInterface(constants.SessionKeyUserSettings, &settings)

		// Update user's default sort preference
		settings.DefaultSort = option
		if err := m.handler.db.Settings().SaveUserSettings(context.Background(), settings); err != nil {
			m.handler.logger.Error("Failed to save user settings", zap.Error(err))
			m.handler.paginationManager.RespondWithError(event, "Failed to save sort order. Please try again.")
			return
		}

		m.ShowReviewMenu(event, s, "Changed sort order. Will take effect for the next user.")
	case constants.ActionSelectMenuCustomID:
		switch option {
		case constants.ConfirmWithReasonButtonCustomID:
			m.handleConfirmWithReason(event, s)
		case constants.RecheckButtonCustomID:
			m.handleRecheck(event, s)
		case constants.ViewUserLogsButtonCustomID:
			m.handleViewUserLogs(event, s)
		case constants.OpenOutfitsMenuButtonCustomID:
			m.handler.outfitsMenu.ShowOutfitsMenu(event, s, 0)
		case constants.OpenFriendsMenuButtonCustomID:
			m.handler.friendsMenu.ShowFriendsMenu(event, s, 0)
		case constants.OpenGroupsMenuButtonCustomID:
			m.handler.groupsMenu.ShowGroupsMenu(event, s, 0)
		}
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

	if event.Data.CustomID == constants.ConfirmWithReasonModalCustomID {
		m.handleConfirmWithReasonModalSubmit(event, s)
	}
}

// handleRecheck adds the user to the high priority queue for re-processing.
// If the user is already in queue, it shows the status menu instead.
func (m *Menu) handleRecheck(event *events.ComponentInteractionCreate, s *session.Session) {
	var user *database.FlaggedUser
	s.GetInterface(constants.SessionKeyTarget, &user)

	// Check if user is already in queue to prevent duplicate entries
	status, _, _, err := m.handler.queueManager.GetQueueInfo(context.Background(), user.ID)
	if err == nil && status != "" {
		m.handler.statusMenu.ShowStatusMenu(event, s)
		return
	}

	// Add to high priority queue with reviewer information
	err = m.handler.queueManager.AddToQueue(context.Background(), &queue.Item{
		UserID:      user.ID,
		Priority:    queue.HighPriority,
		Reason:      fmt.Sprintf("Re-queue requested by reviewer %d", event.User().ID),
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
		queue.HighPriority,
		m.handler.queueManager.GetQueueLength(context.Background(), queue.HighPriority),
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
	var user *database.FlaggedUser
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
	var user *database.FlaggedUser
	s.GetInterface(constants.SessionKeyTarget, &user)

	// Update user status in database
	if err := m.handler.db.Users().ConfirmUser(context.Background(), user); err != nil {
		m.handler.logger.Error("Failed to confirm user", zap.Error(err))
		m.handler.paginationManager.RespondWithError(event, "Failed to confirm the user. Please try again.")
		return
	}

	// Log the confirm action asynchronously
	go m.handler.db.UserActivity().LogActivity(context.Background(), &database.UserActivityLog{
		UserID:            user.ID,
		ReviewerID:        uint64(event.User().ID),
		ActivityType:      database.ActivityTypeConfirmed,
		ActivityTimestamp: time.Now(),
		Details:           map[string]interface{}{"reason": user.Reason},
	})

	// Get the number of flagged users left to review
	flaggedCount, err := m.handler.db.Users().GetFlaggedUsersCount(context.Background())
	if err != nil {
		m.handler.logger.Error("Failed to get flagged users count", zap.Error(err))
	}

	// Clear current user and load next one
	s.Delete(constants.SessionKeyTarget)
	m.ShowReviewMenu(event, s, fmt.Sprintf("User confirmed. %d users left to review.", flaggedCount))
}

// handleClearUser removes a user from the flagged state and logs the action.
// After clearing, it loads a new user for review.
func (m *Menu) handleClearUser(event interfaces.CommonEvent, s *session.Session) {
	var user *database.FlaggedUser
	s.GetInterface(constants.SessionKeyTarget, &user)

	// Update user status in database
	if err := m.handler.db.Users().ClearUser(context.Background(), user); err != nil {
		m.handler.logger.Error("Failed to reject user", zap.Error(err))
		m.handler.paginationManager.RespondWithError(event, "Failed to reject the user. Please try again.")
		return
	}

	// Log the clear action asynchronously
	go m.handler.db.UserActivity().LogActivity(context.Background(), &database.UserActivityLog{
		UserID:            user.ID,
		ReviewerID:        uint64(event.User().ID),
		ActivityType:      database.ActivityTypeCleared,
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
	m.ShowReviewMenu(event, s, fmt.Sprintf("User cleared. %d users left to review.", flaggedCount))
}

// handleSkipUser logs the skip action and moves to the next user without
// changing the current user's status.
func (m *Menu) handleSkipUser(event interfaces.CommonEvent, s *session.Session) {
	var user *database.FlaggedUser
	s.GetInterface(constants.SessionKeyTarget, &user)

	// Log the skip action asynchronously
	go m.handler.db.UserActivity().LogActivity(context.Background(), &database.UserActivityLog{
		UserID:            user.ID,
		ReviewerID:        uint64(event.User().ID),
		ActivityType:      database.ActivityTypeSkipped,
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
	var user *database.FlaggedUser
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
	var user *database.FlaggedUser
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
	go m.handler.db.UserActivity().LogActivity(context.Background(), &database.UserActivityLog{
		UserID:            user.ID,
		ReviewerID:        uint64(event.User().ID),
		ActivityType:      database.ActivityTypeConfirmedCustom,
		ActivityTimestamp: time.Now(),
		Details:           map[string]interface{}{"reason": user.Reason},
	})

	// Clear current user and load next one
	s.Delete(constants.SessionKeyTarget)
	m.ShowReviewMenu(event, s, "User confirmed.")
}

// fetchNewTarget gets a new user to review based on the current sort order.
// It logs the view action and stores the user in the session.
func (m *Menu) fetchNewTarget(s *session.Session, reviewerID uint64) (*database.FlaggedUser, error) {
	// Retrieve user settings from session
	var settings *database.UserSetting
	s.GetInterface(constants.SessionKeyUserSettings, &settings)

	// Get the sort order from user settings
	sortBy := settings.DefaultSort

	// Get the next user to review
	user, err := m.handler.db.Users().GetFlaggedUserToReview(context.Background(), sortBy)
	if err != nil {
		return nil, err
	}

	// Store the user in session for the message builder
	s.Set(constants.SessionKeyTarget, user)

	// Log the view action asynchronously
	go m.handler.db.UserActivity().LogActivity(context.Background(), &database.UserActivityLog{
		UserID:            user.ID,
		ReviewerID:        reviewerID,
		ActivityType:      database.ActivityTypeViewed,
		ActivityTimestamp: time.Now(),
		Details:           make(map[string]interface{}),
	})

	return user, nil
}

// checkLastViewed checks if the current target user has timed out and needs to be refreshed.
// Clears the current user and loads a new one if the timeout is detected.
func (m *Menu) checkLastViewed(event interfaces.CommonEvent, s *session.Session) bool {
	var user *database.FlaggedUser
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
