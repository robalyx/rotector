package group

import (
	"context"
	"fmt"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	builder "github.com/rotector/rotector/internal/bot/builder/review/group"
	"github.com/rotector/rotector/internal/bot/constants"
	"github.com/rotector/rotector/internal/bot/core/pagination"
	"github.com/rotector/rotector/internal/bot/core/session"
	"github.com/rotector/rotector/internal/bot/interfaces"
	"github.com/rotector/rotector/internal/common/storage/database/models"
	"go.uber.org/zap"
)

// Menu handles the main review interface where moderators can view and take
// action on flagged groups. It works with the review builder to create paginated
// views of group information and manages review actions.
type Menu struct {
	handler *Handler
	page    *pagination.Page
}

// NewMenu creates a Menu and sets up its page with message builders and
// interaction handlers. The page is configured to show group information
// and handle review actions.
func NewMenu(h *Handler) *Menu {
	m := Menu{handler: h}
	m.page = &pagination.Page{
		Name: "Group Review Menu",
		Message: func(s *session.Session) *discord.MessageUpdateBuilder {
			return builder.NewReviewBuilder(s, h.db).Build()
		},
		SelectHandlerFunc: m.handleSelectMenu,
		ButtonHandlerFunc: m.handleButton,
		ModalHandlerFunc:  m.handleModal,
	}
	return &m
}

// ShowReviewMenu prepares and displays the review interface by loading
// group data and review settings into the session.
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

	var group *models.FlaggedGroup
	s.GetInterface(constants.SessionKeyGroupTarget, &group)

	// If no group is set in session, fetch a new one
	if group == nil {
		var err error
		group, err = m.fetchNewTarget(s, uint64(event.User().ID))
		if err != nil {
			m.handler.logger.Error("Failed to fetch a new group", zap.Error(err))
			m.handler.paginationManager.RespondWithError(event, "Failed to fetch a new group. Please try again.")
			return
		}
	}

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

	// Update user's group sort preference
	settings.GroupDefaultSort = option
	if err := m.handler.db.Settings().SaveUserSettings(context.Background(), settings); err != nil {
		m.handler.logger.Error("Failed to save user settings", zap.Error(err))
		m.handler.paginationManager.RespondWithError(event, "Failed to save sort order. Please try again.")
		return
	}

	m.ShowReviewMenu(event, s, "Changed sort order. Will take effect for the next group.")
}

// handleActionSelection processes action menu selections.
func (m *Menu) handleActionSelection(event *events.ComponentInteractionCreate, s *session.Session, option string) {
	// Get bot settings to check reviewer status
	var settings *models.BotSetting
	s.GetInterface(constants.SessionKeyBotSettings, &settings)
	userID := uint64(event.User().ID)

	switch option {
	case constants.GroupViewLogsButtonCustomID:
		m.handleViewGroupLogs(event, s)

	case constants.GroupConfirmWithReasonButtonCustomID:
		if !settings.IsReviewer(userID) {
			m.handler.logger.Error("Non-reviewer attempted to use confirm with reason", zap.Uint64("user_id", userID))
			m.handler.paginationManager.RespondWithError(event, "You do not have permission to confirm groups with custom reasons.")
			return
		}
		m.handleConfirmWithReason(event, s)

	case constants.SwitchReviewModeCustomID:
		if !settings.IsReviewer(userID) {
			m.handler.logger.Error("Non-reviewer attempted to switch review mode", zap.Uint64("user_id", userID))
			m.handler.paginationManager.RespondWithError(event, "You do not have permission to switch review modes.")
			return
		}
		m.handleSwitchReviewMode(event, s)
	}
}

// fetchNewTarget gets a new group to review based on the current sort order.
// It logs the view action and stores the group in the session.
func (m *Menu) fetchNewTarget(s *session.Session, reviewerID uint64) (*models.FlaggedGroup, error) {
	// Retrieve user settings from session
	var settings *models.UserSetting
	s.GetInterface(constants.SessionKeyUserSettings, &settings)

	// Get the sort order from user settings
	sortBy := settings.GroupDefaultSort

	// Get the next group to review
	group, err := m.handler.db.Groups().GetFlaggedGroupToReview(context.Background(), sortBy)
	if err != nil {
		return nil, err
	}

	// Store the group in session for the message builder
	s.Set(constants.SessionKeyGroupTarget, group)

	// Log the view action asynchronously
	go m.handler.db.UserActivity().LogActivity(context.Background(), &models.UserActivityLog{
		ActivityTarget: models.ActivityTarget{
			UserID:  0,
			GroupID: group.ID,
		},
		ReviewerID:        reviewerID,
		ActivityType:      models.ActivityTypeGroupViewed,
		ActivityTimestamp: time.Now(),
		Details:           make(map[string]interface{}),
	})

	return group, nil
}

// handleButton processes button clicks.
func (m *Menu) handleButton(event *events.ComponentInteractionCreate, s *session.Session, customID string) {
	switch customID {
	case constants.BackButtonCustomID:
		m.handler.dashboardHandler.ShowDashboard(event, s, "")
	case constants.GroupConfirmButtonCustomID:
		m.handleConfirmGroup(event, s)
	case constants.GroupClearButtonCustomID:
		m.handleClearGroup(event, s)
	case constants.GroupSkipButtonCustomID:
		m.handleSkipGroup(event, s)
	}
}

// handleModal processes modal submissions.
func (m *Menu) handleModal(event *events.ModalSubmitInteractionCreate, s *session.Session) {
	switch event.Data.CustomID {
	case constants.GroupConfirmWithReasonModalCustomID:
		m.handleConfirmWithReasonModalSubmit(event, s)
	}
}

// handleViewGroupLogs handles the shortcut to view group logs.
// It stores the group ID in session for log filtering and shows the logs menu.
func (m *Menu) handleViewGroupLogs(event *events.ComponentInteractionCreate, s *session.Session) {
	var group *models.FlaggedGroup
	s.GetInterface(constants.SessionKeyGroupTarget, &group)
	if group == nil {
		m.handler.paginationManager.RespondWithError(event, "No group selected to view logs.")
		return
	}

	// Set the group ID filter
	m.handler.logHandler.ResetFilters(s)
	s.Set(constants.SessionKeyGroupIDFilter, group.ID)

	// Show the logs menu
	m.handler.logHandler.ShowLogMenu(event, s)
}

// handleConfirmWithReason opens a modal for entering a custom confirm reason.
// The modal pre-fills with the current reason if one exists.
func (m *Menu) handleConfirmWithReason(event *events.ComponentInteractionCreate, s *session.Session) {
	var group *models.FlaggedGroup
	s.GetInterface(constants.SessionKeyGroupTarget, &group)

	// Create modal with pre-filled reason field
	modal := discord.NewModalCreateBuilder().
		SetCustomID(constants.GroupConfirmWithReasonModalCustomID).
		SetTitle("Confirm Group with Reason").
		AddActionRow(
			discord.NewTextInput(constants.GroupConfirmReasonInputCustomID, discord.TextInputStyleParagraph, "Confirm Reason").
				WithRequired(true).
				WithPlaceholder("Enter the reason for confirming this group...").
				WithValue(group.Reason),
		).
		Build()

	// Show modal to user
	if err := event.Modal(modal); err != nil {
		m.handler.logger.Error("Failed to create modal", zap.Error(err))
		m.handler.paginationManager.RespondWithError(event, "Failed to open the confirm reason form. Please try again.")
	}
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

// handleConfirmGroup moves a group to the confirmed state and logs the action.
func (m *Menu) handleConfirmGroup(event interfaces.CommonEvent, s *session.Session) {
	var group *models.FlaggedGroup
	s.GetInterface(constants.SessionKeyGroupTarget, &group)

	var settings *models.UserSetting
	s.GetInterface(constants.SessionKeyUserSettings, &settings)

	var actionMsg string
	if settings.ReviewMode == models.TrainingReviewMode {
		// Training mode - increment downvotes
		if err := m.handler.db.Groups().UpdateTrainingVotes(context.Background(), group.ID, false); err != nil {
			m.handler.paginationManager.RespondWithError(event, "Failed to update downvotes. Please try again.")
			return
		}
		group.Downvotes++
		actionMsg = "downvoted"

		// Log the training downvote action
		go m.handler.db.UserActivity().LogActivity(context.Background(), &models.UserActivityLog{
			ActivityTarget: models.ActivityTarget{
				GroupID: group.ID,
			},
			ReviewerID:        uint64(event.User().ID),
			ActivityType:      models.ActivityTypeGroupTrainingDownvote,
			ActivityTimestamp: time.Now(),
			Details: map[string]interface{}{
				"upvotes":   group.Upvotes,
				"downvotes": group.Downvotes,
			},
		})
	} else {
		// Standard mode - confirm group
		if err := m.handler.db.Groups().ConfirmGroup(context.Background(), group); err != nil {
			m.handler.logger.Error("Failed to confirm group", zap.Error(err))
			m.handler.paginationManager.RespondWithError(event, "Failed to confirm the group. Please try again.")
			return
		}
		actionMsg = "confirmed"

		// Log the confirm action
		go m.handler.db.UserActivity().LogActivity(context.Background(), &models.UserActivityLog{
			ActivityTarget: models.ActivityTarget{
				GroupID: group.ID,
			},
			ReviewerID:        uint64(event.User().ID),
			ActivityType:      models.ActivityTypeGroupConfirmed,
			ActivityTimestamp: time.Now(),
			Details:           map[string]interface{}{"reason": group.Reason},
		})
	}

	// Clear current group and load next one
	s.Delete(constants.SessionKeyGroupTarget)
	m.ShowReviewMenu(event, s, fmt.Sprintf("Group %s.", actionMsg))
}

// handleClearGroup removes a group from the flagged state and logs the action.
func (m *Menu) handleClearGroup(event interfaces.CommonEvent, s *session.Session) {
	var group *models.FlaggedGroup
	s.GetInterface(constants.SessionKeyGroupTarget, &group)

	var settings *models.UserSetting
	s.GetInterface(constants.SessionKeyUserSettings, &settings)

	var actionMsg string
	if settings.ReviewMode == models.TrainingReviewMode {
		// Training mode - increment upvotes
		if err := m.handler.db.Groups().UpdateTrainingVotes(context.Background(), group.ID, true); err != nil {
			m.handler.paginationManager.RespondWithError(event, "Failed to update upvotes. Please try again.")
			return
		}
		group.Upvotes++
		actionMsg = "upvoted"

		// Log the training upvote action
		go m.handler.db.UserActivity().LogActivity(context.Background(), &models.UserActivityLog{
			ActivityTarget: models.ActivityTarget{
				GroupID: group.ID,
			},
			ReviewerID:        uint64(event.User().ID),
			ActivityType:      models.ActivityTypeGroupTrainingUpvote,
			ActivityTimestamp: time.Now(),
			Details: map[string]interface{}{
				"upvotes":   group.Upvotes,
				"downvotes": group.Downvotes,
			},
		})
	} else {
		// Standard mode - clear group
		if err := m.handler.db.Groups().ClearGroup(context.Background(), group); err != nil {
			m.handler.logger.Error("Failed to clear group", zap.Error(err))
			m.handler.paginationManager.RespondWithError(event, "Failed to clear the group. Please try again.")
			return
		}
		actionMsg = "cleared"

		// Log the clear action
		go m.handler.db.UserActivity().LogActivity(context.Background(), &models.UserActivityLog{
			ActivityTarget: models.ActivityTarget{
				GroupID: group.ID,
			},
			ReviewerID:        uint64(event.User().ID),
			ActivityType:      models.ActivityTypeGroupCleared,
			ActivityTimestamp: time.Now(),
			Details:           make(map[string]interface{}),
		})
	}

	// Clear current group and load next one
	s.Delete(constants.SessionKeyGroupTarget)
	m.ShowReviewMenu(event, s, fmt.Sprintf("Group %s.", actionMsg))
}

// handleSkipGroup logs the skip action and moves to the next group.
func (m *Menu) handleSkipGroup(event interfaces.CommonEvent, s *session.Session) {
	var group *models.FlaggedGroup
	s.GetInterface(constants.SessionKeyGroupTarget, &group)

	// Log the skip action asynchronously
	go m.handler.db.UserActivity().LogActivity(context.Background(), &models.UserActivityLog{
		ActivityTarget: models.ActivityTarget{
			GroupID: group.ID,
		},
		ReviewerID:        uint64(event.User().ID),
		ActivityType:      models.ActivityTypeGroupSkipped,
		ActivityTimestamp: time.Now(),
		Details:           make(map[string]interface{}),
	})

	// Clear current group and load next one
	s.Delete(constants.SessionKeyGroupTarget)
	m.ShowReviewMenu(event, s, "Skipped group.")
}

// handleConfirmWithReasonModalSubmit processes the custom confirm reason from the modal.
func (m *Menu) handleConfirmWithReasonModalSubmit(event *events.ModalSubmitInteractionCreate, s *session.Session) {
	var group *models.FlaggedGroup
	s.GetInterface(constants.SessionKeyGroupTarget, &group)

	// Get and validate the confirm reason
	reason := event.Data.Text(constants.GroupConfirmReasonInputCustomID)
	if reason == "" {
		m.handler.paginationManager.RespondWithError(event, "Confirm reason cannot be empty. Please try again.")
		return
	}

	// Update group's reason with the custom input
	group.Reason = reason

	// Update group status in database
	if err := m.handler.db.Groups().ConfirmGroup(context.Background(), group); err != nil {
		m.handler.logger.Error("Failed to confirm group", zap.Error(err))
		m.handler.paginationManager.RespondWithError(event, "Failed to confirm the group. Please try again.")
		return
	}

	// Log the custom confirm action asynchronously
	go m.handler.db.UserActivity().LogActivity(context.Background(), &models.UserActivityLog{
		ActivityTarget: models.ActivityTarget{
			GroupID: group.ID,
		},
		ReviewerID:        uint64(event.User().ID),
		ActivityType:      models.ActivityTypeGroupConfirmedCustom,
		ActivityTimestamp: time.Now(),
		Details:           map[string]interface{}{"reason": group.Reason},
	})

	// Clear current group and load next one
	s.Delete(constants.SessionKeyGroupTarget)
	m.ShowReviewMenu(event, s, "Group confirmed.")
}