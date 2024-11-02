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

// Menu handles the review process for users.
type Menu struct {
	handler *Handler
	page    *pagination.Page
}

// NewMenu creates a new Menu instance.
func NewMenu(h *Handler) *Menu {
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

// ShowReviewMenuAndFetchUser displays the review menu and fetches a new user.
func (m *Menu) ShowReviewMenuAndFetchUser(event interfaces.CommonEvent, s *session.Session, content string) {
	// Fetch a new user
	sortBy := s.GetString(constants.SessionKeySortBy)
	user, err := m.handler.db.Users().GetFlaggedUserToReview(sortBy)
	if err != nil {
		m.handler.logger.Error("Failed to fetch a new user", zap.Error(err))
		m.handler.paginationManager.RespondWithError(event, "Failed to fetch a new user. Please try again.")
		return
	}
	s.Set(constants.SessionKeyTarget, user)

	// Display the review menu
	m.ShowReviewMenu(event, s, content)

	// Log the activity
	go m.handler.db.UserActivity().LogActivity(&database.UserActivityLog{
		UserID:            user.ID,
		ReviewerID:        uint64(event.User().ID),
		ActivityType:      database.ActivityTypeViewed,
		ActivityTimestamp: time.Now(),
		Details:           make(map[string]interface{}),
	})
}

// ShowReviewMenu displays the review menu.
func (m *Menu) ShowReviewMenu(event interfaces.CommonEvent, s *session.Session, content string) {
	var user *database.FlaggedUser
	s.GetInterface(constants.SessionKeyTarget, &user)

	// Get flagged friends
	flaggedFriends := make(map[uint64]string)
	if len(user.Friends) > 0 {
		// Extract friend IDs
		friendIDs := make([]uint64, len(user.Friends))
		for i, friend := range user.Friends {
			friendIDs[i] = friend.ID
		}

		// Check which users already exist in the database
		existingUsers, err := m.handler.db.Users().CheckExistingUsers(friendIDs)
		if err != nil {
			m.handler.logger.Error("Failed to check existing friends", zap.Error(err))
			return
		}

		// Get flagged friends
		for friendID, status := range existingUsers {
			if status == database.UserTypeConfirmed || status == database.UserTypeFlagged {
				flaggedFriends[friendID] = status
			}
		}
	}

	// Get user settings
	settings, err := m.handler.db.Settings().GetUserSettings(uint64(event.User().ID))
	if err != nil {
		m.handler.logger.Error("Failed to get user settings", zap.Error(err))
		m.handler.paginationManager.RespondWithError(event, "Failed to get user settings. Please try again.")
		return
	}

	// Set the data for the page
	s.Set(constants.SessionKeyFlaggedFriends, flaggedFriends)
	s.Set(constants.SessionKeyStreamerMode, settings.StreamerMode)

	m.handler.paginationManager.NavigateTo(event, s, m.page, content)
}

// handleSelectMenu handles the select menu for the review menu.
func (m *Menu) handleSelectMenu(event *events.ComponentInteractionCreate, s *session.Session, customID string, option string) {
	switch customID {
	case constants.SortOrderSelectMenuCustomID:
		s.Set(constants.SessionKeySortBy, option)
		m.ShowReviewMenuAndFetchUser(event, s, "Changed sort order")
	case constants.ActionSelectMenuCustomID:
		switch option {
		case constants.BanWithReasonButtonCustomID:
			m.handleBanWithReason(event, s)
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
	switch customID {
	case constants.BackButtonCustomID:
		m.handler.dashboardHandler.ShowDashboard(event, s, "")
	case constants.RecheckButtonCustomID:
		m.handleRecheck(event, s)
	case constants.BanButtonCustomID:
		m.handleBanUser(event, s)
	case constants.ClearButtonCustomID:
		m.handleClearUser(event, s)
	case constants.SkipButtonCustomID:
		m.handleSkipUser(event, s)
	}
}

// handleModal handles the modal for the review menu.
func (m *Menu) handleModal(event *events.ModalSubmitInteractionCreate, s *session.Session) {
	if event.Data.CustomID == constants.BanWithReasonModalCustomID {
		m.handleBanWithReasonModalSubmit(event, s)
	}
}

// handleRecheck handles the recheck button interaction.
func (m *Menu) handleRecheck(event *events.ComponentInteractionCreate, s *session.Session) {
	var user *database.FlaggedUser
	s.GetInterface(constants.SessionKeyTarget, &user)

	// Check if user is already in queue
	status, _, _, err := m.handler.queueManager.GetQueueInfo(context.Background(), user.ID)
	if err == nil && status != "" {
		// User is already in queue, show status menu
		m.handler.statusMenu.ShowStatusMenu(event, s)
		return
	}

	// Add to high priority queue
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

	// Update queue info
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

	// Set the queue user in the session
	s.Set(constants.SessionKeyQueueUser, user.ID)

	// Show status menu
	m.handler.statusMenu.ShowStatusMenu(event, s)
}

// handleBanUser handles the ban user button interaction.
func (m *Menu) handleBanUser(event interfaces.CommonEvent, s *session.Session) {
	var user *database.FlaggedUser
	s.GetInterface(constants.SessionKeyTarget, &user)

	// Move the user to confirmed
	if err := m.handler.db.Users().ConfirmUser(user); err != nil {
		m.handler.logger.Error("Failed to confirm user", zap.Error(err))
		m.handler.paginationManager.RespondWithError(event, "Failed to ban the user. Please try again.")
		return
	}

	// Log the activity
	go m.handler.db.UserActivity().LogActivity(&database.UserActivityLog{
		UserID:            user.ID,
		ReviewerID:        uint64(event.User().ID),
		ActivityType:      database.ActivityTypeBanned,
		ActivityTimestamp: time.Now(),
		Details:           map[string]interface{}{"reason": user.Reason},
	})

	m.ShowReviewMenuAndFetchUser(event, s, "User banned.")
}

// handleClearUser handles the clear user button interaction.
func (m *Menu) handleClearUser(event interfaces.CommonEvent, s *session.Session) {
	var user *database.FlaggedUser
	s.GetInterface(constants.SessionKeyTarget, &user)

	// Clear the user
	if err := m.handler.db.Users().ClearUser(user); err != nil {
		m.handler.logger.Error("Failed to reject user", zap.Error(err))
		m.handler.paginationManager.RespondWithError(event, "Failed to reject the user. Please try again.")
		return
	}

	// Log the activity
	go m.handler.db.UserActivity().LogActivity(&database.UserActivityLog{
		UserID:            user.ID,
		ReviewerID:        uint64(event.User().ID),
		ActivityType:      database.ActivityTypeCleared,
		ActivityTimestamp: time.Now(),
		Details:           make(map[string]interface{}),
	})

	m.ShowReviewMenuAndFetchUser(event, s, "User cleared.")
}

// handleSkipUser handles the skip user button interaction.
func (m *Menu) handleSkipUser(event interfaces.CommonEvent, s *session.Session) {
	var user *database.FlaggedUser
	s.GetInterface(constants.SessionKeyTarget, &user)

	// Log the activity
	go m.handler.db.UserActivity().LogActivity(&database.UserActivityLog{
		UserID:            user.ID,
		ReviewerID:        uint64(event.User().ID),
		ActivityType:      database.ActivityTypeSkipped,
		ActivityTimestamp: time.Now(),
		Details:           make(map[string]interface{}),
	})

	m.ShowReviewMenuAndFetchUser(event, s, "Skipped user.")
}

// handleBanWithReason processes the ban with a modal for a custom reason.
func (m *Menu) handleBanWithReason(event *events.ComponentInteractionCreate, s *session.Session) {
	var user *database.FlaggedUser
	s.GetInterface(constants.SessionKeyTarget, &user)

	// Create the modal
	modal := discord.NewModalCreateBuilder().
		SetCustomID(constants.BanWithReasonModalCustomID).
		SetTitle("Ban User with Reason").
		AddActionRow(
			discord.NewTextInput("ban_reason", discord.TextInputStyleParagraph, "Ban Reason").
				WithRequired(true).
				WithPlaceholder("Enter the reason for banning this user...").
				WithValue(user.Reason), // Pre-fill with the original reason if available
		).
		Build()

	// Send the modal
	if err := event.Modal(modal); err != nil {
		m.handler.logger.Error("Failed to create modal", zap.Error(err))
		m.handler.paginationManager.RespondWithError(event, "Failed to open the ban reason form. Please try again.")
	}
}

// handleBanWithReasonModalSubmit processes the modal submit interaction.
func (m *Menu) handleBanWithReasonModalSubmit(event *events.ModalSubmitInteractionCreate, s *session.Session) {
	var user *database.FlaggedUser
	s.GetInterface(constants.SessionKeyTarget, &user)

	// Get the ban reason from the modal
	reason := event.Data.Text("ban_reason")
	if reason == "" {
		m.handler.paginationManager.RespondWithError(event, "Ban reason cannot be empty. Please try again.")
		return
	}

	// Update the user's reason with the custom input
	user.Reason = reason

	// Move the user to confirmed
	if err := m.handler.db.Users().ConfirmUser(user); err != nil {
		m.handler.logger.Error("Failed to confirm user", zap.Error(err))
		m.handler.paginationManager.RespondWithError(event, "Failed to confirm the user. Please try again.")
		return
	}

	// Log the activity
	go m.handler.db.UserActivity().LogActivity(&database.UserActivityLog{
		UserID:            user.ID,
		ReviewerID:        uint64(event.User().ID),
		ActivityType:      database.ActivityTypeBannedCustom,
		ActivityTimestamp: time.Now(),
		Details:           map[string]interface{}{"reason": user.Reason},
	})

	// Show the review menu and fetch a new user
	m.ShowReviewMenuAndFetchUser(event, s, "User banned.")
}
