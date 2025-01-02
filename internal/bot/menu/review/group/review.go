package group

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	apiTypes "github.com/jaxron/roapi.go/pkg/api/types"
	builder "github.com/rotector/rotector/internal/bot/builder/review/group"
	"github.com/rotector/rotector/internal/bot/constants"
	"github.com/rotector/rotector/internal/bot/core/pagination"
	"github.com/rotector/rotector/internal/bot/core/session"
	"github.com/rotector/rotector/internal/bot/interfaces"
	"github.com/rotector/rotector/internal/common/storage/database/types"
	"go.uber.org/zap"
)

// ReviewMenu handles the main review interface where moderators can view and take
// action on flagged groups.
type ReviewMenu struct {
	layout *Layout
	page   *pagination.Page
}

// NewMenu creates a Menu and sets up its page with message builders and
// interaction handlers. The page is configured to show group information
// and handle review actions.
func NewReviewMenu(layout *Layout) *ReviewMenu {
	m := &ReviewMenu{layout: layout}
	m.page = &pagination.Page{
		Name: "Group Review Menu",
		Message: func(s *session.Session) *discord.MessageUpdateBuilder {
			return builder.NewReviewBuilder(s, layout.db).Build()
		},
		SelectHandlerFunc: m.handleSelectMenu,
		ButtonHandlerFunc: m.handleButton,
		ModalHandlerFunc:  m.handleModal,
	}
	return m
}

// Show prepares and displays the review interface by loading
// group data and review settings into the session.
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

	var group *types.ReviewGroup
	s.GetInterface(constants.SessionKeyGroupTarget, &group)

	// If no group is set in session, fetch a new one
	if group == nil {
		var err error
		group, err = m.fetchNewTarget(s, uint64(event.User().ID))
		if err != nil {
			if errors.Is(err, types.ErrNoGroupsToReview) {
				m.layout.paginationManager.Refresh(event, s, "No groups to review. Please check back later.")
				return
			}
			m.layout.logger.Error("Failed to fetch a new group", zap.Error(err))
			m.layout.paginationManager.RespondWithError(event, "Failed to fetch a new group. Please try again.")
			return
		}
	}

	// Fetch latest group info from API
	groupInfo, err := m.layout.roAPI.Groups().GetGroupInfo(context.Background(), group.ID)
	if err != nil {
		m.layout.logger.Error("Failed to fetch group info",
			zap.Error(err),
			zap.Uint64("groupID", group.ID))
		m.layout.paginationManager.RespondWithError(event, "Failed to fetch latest group information. Please try again.")
		return
	}

	// Store group info in session
	s.Set(constants.SessionKeyGroupInfo, groupInfo)

	m.layout.paginationManager.NavigateTo(event, s, m.page, content)
}

// handleSelectMenu processes select menu interactions.
func (m *ReviewMenu) handleSelectMenu(event *events.ComponentInteractionCreate, s *session.Session, customID string, option string) {
	if m.checkLastViewed(event, s) || m.checkCaptchaRequired(event, s) {
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

	// Update user's group sort preference
	settings.GroupDefaultSort = types.ReviewSortBy(option)
	if err := m.layout.db.Settings().SaveUserSettings(context.Background(), settings); err != nil {
		m.layout.logger.Error("Failed to save user settings", zap.Error(err))
		m.layout.paginationManager.RespondWithError(event, "Failed to save sort order. Please try again.")
		return
	}
	s.Set(constants.SessionKeyUserSettings, settings)

	m.Show(event, s, "Changed sort order. Will take effect for the next group.")
}

// handleActionSelection processes action menu selections.
func (m *ReviewMenu) handleActionSelection(event *events.ComponentInteractionCreate, s *session.Session, option string) {
	// Get bot settings to check reviewer status
	var settings *types.BotSetting
	s.GetInterface(constants.SessionKeyBotSettings, &settings)
	userID := uint64(event.User().ID)

	switch option {
	case constants.GroupViewMembersButtonCustomID:
		m.layout.membersMenu.Show(event, s, 0)
	case constants.OpenAIChatButtonCustomID:
		if !settings.IsReviewer(userID) {
			m.layout.logger.Error("Non-reviewer attempted to open AI chat", zap.Uint64("user_id", userID))
			m.layout.paginationManager.RespondWithError(event, "You do not have permission to open the AI chat.")
			return
		}
		m.handleOpenAIChat(event, s)
	case constants.GroupViewLogsButtonCustomID:
		if !settings.IsReviewer(userID) {
			m.layout.logger.Error("Non-reviewer attempted to view logs", zap.Uint64("user_id", userID))
			m.layout.paginationManager.RespondWithError(event, "You do not have permission to view activity logs.")
			return
		}
		m.handleViewGroupLogs(event, s)
	case constants.GroupConfirmWithReasonButtonCustomID:
		if !settings.IsReviewer(userID) {
			m.layout.logger.Error("Non-reviewer attempted to use confirm with reason", zap.Uint64("user_id", userID))
			m.layout.paginationManager.RespondWithError(event, "You do not have permission to confirm groups with custom reasons.")
			return
		}
		if s.GetBool(constants.SessionKeyIsLookupMode) {
			m.layout.paginationManager.RespondWithError(event, "Cannot confirm groups in lookup mode.")
			return
		}
		m.handleConfirmWithReason(event, s)
	case constants.ReviewModeOption:
		if !settings.IsReviewer(userID) {
			m.layout.logger.Error("Non-reviewer attempted to change review mode", zap.Uint64("user_id", userID))
			m.layout.paginationManager.RespondWithError(event, "You do not have permission to change review mode.")
			return
		}
		if s.GetBool(constants.SessionKeyIsLookupMode) {
			m.layout.paginationManager.RespondWithError(event, "Cannot change review mode in lookup mode.")
			return
		}
		m.layout.settingLayout.ShowUpdate(event, s, constants.UserSettingPrefix, constants.ReviewModeOption)
	case constants.ReviewTargetModeOption:
		if s.GetBool(constants.SessionKeyIsLookupMode) {
			m.layout.paginationManager.RespondWithError(event, "Cannot change review target in lookup mode.")
			return
		}
		m.layout.settingLayout.ShowUpdate(event, s, constants.UserSettingPrefix, constants.ReviewTargetModeOption)
	}
}

// fetchNewTarget gets a new group to review based on the current sort order.
func (m *ReviewMenu) fetchNewTarget(s *session.Session, reviewerID uint64) (*types.ReviewGroup, error) {
	var settings *types.UserSetting
	s.GetInterface(constants.SessionKeyUserSettings, &settings)

	// Get the next group to review
	group, err := m.layout.db.Groups().GetGroupToReview(context.Background(), settings.GroupDefaultSort, settings.ReviewTargetMode)
	if err != nil {
		return nil, err
	}

	// Get flagged users from tracking
	flaggedUsers, err := m.layout.db.Tracking().GetFlaggedUsers(context.Background(), group.ID)
	if err != nil {
		return nil, err
	}

	// Store the group and flagged users in session
	s.Set(constants.SessionKeyGroupTarget, group)
	s.Set(constants.SessionKeyGroupMemberIDs, flaggedUsers)

	// Log the view action
	go m.layout.db.UserActivity().Log(context.Background(), &types.UserActivityLog{
		ActivityTarget: types.ActivityTarget{
			GroupID: group.ID,
		},
		ReviewerID:        reviewerID,
		ActivityType:      types.ActivityTypeGroupViewed,
		ActivityTimestamp: time.Now(),
		Details:           make(map[string]interface{}),
	})

	return group, nil
}

// handleButton processes button clicks.
func (m *ReviewMenu) handleButton(event *events.ComponentInteractionCreate, s *session.Session, customID string) {
	if m.checkLastViewed(event, s) || m.checkCaptchaRequired(event, s) {
		return
	}

	// Prevent actions in lookup mode except for back button
	isLookupMode := s.GetBool(constants.SessionKeyIsLookupMode)
	if isLookupMode && customID != constants.BackButtonCustomID {
		m.layout.paginationManager.RespondWithError(event, "Actions are not available in lookup mode.")
		return
	}

	switch customID {
	case constants.BackButtonCustomID:
		if isLookupMode {
			s.Delete(constants.SessionKeyGroupTarget)
			s.Delete(constants.SessionKeyIsLookupMode)
		}
		m.layout.paginationManager.NavigateBack(event, s, "")
	case constants.GroupConfirmButtonCustomID:
		m.handleConfirmGroup(event, s)
	case constants.GroupClearButtonCustomID:
		m.handleClearGroup(event, s)
	case constants.GroupSkipButtonCustomID:
		m.handleSkipGroup(event, s)
	}
}

// handleModal processes modal submissions.
func (m *ReviewMenu) handleModal(event *events.ModalSubmitInteractionCreate, s *session.Session) {
	if m.checkLastViewed(event, s) || m.checkCaptchaRequired(event, s) {
		return
	}

	// Prevent actions in lookup mode
	if s.GetBool(constants.SessionKeyIsLookupMode) {
		m.layout.paginationManager.RespondWithError(event, "Actions are not available in lookup mode.")
		return
	}

	switch event.Data.CustomID {
	case constants.GroupConfirmWithReasonModalCustomID:
		m.handleConfirmWithReasonModalSubmit(event, s)
	}
}

// handleViewGroupLogs handles the shortcut to view group logs.
// It stores the group ID in session for log filtering and shows the logs menu.
func (m *ReviewMenu) handleViewGroupLogs(event *events.ComponentInteractionCreate, s *session.Session) {
	var group *types.ReviewGroup
	s.GetInterface(constants.SessionKeyGroupTarget, &group)
	if group == nil {
		m.layout.paginationManager.RespondWithError(event, "No group selected to view logs.")
		return
	}

	// Set the group ID filter
	m.layout.logLayout.ResetFilters(s)
	s.Set(constants.SessionKeyGroupIDFilter, group.ID)

	// Show the logs menu
	m.layout.logLayout.Show(event, s)
}

// handleOpenAIChat handles the button to open the AI chat for the current group.
func (m *ReviewMenu) handleOpenAIChat(event *events.ComponentInteractionCreate, s *session.Session) {
	var group *types.ReviewGroup
	s.GetInterface(constants.SessionKeyGroupTarget, &group)
	var groupInfo *apiTypes.GroupResponse
	s.GetInterface(constants.SessionKeyGroupInfo, &groupInfo)
	var memberIDs []uint64
	s.GetInterface(constants.SessionKeyGroupMemberIDs, &memberIDs)

	// Create context message about the group
	context := fmt.Sprintf(`<context>
Group Information:

Name: %s
Owner ID: %d
Owner Username: %s
Members: %d
Description: %s
Reason Flagged: %s
Confidence: %.2f
Flagged Members: %d</context>`,
		group.Name,
		group.Owner.UserID,
		group.Owner.Username,
		groupInfo.MemberCount,
		group.Description,
		group.Reason,
		group.Confidence,
		len(memberIDs),
	)

	// Update session and navigate to chat
	s.Set(constants.SessionKeyChatContext, context)
	s.Set(constants.SessionKeyPaginationPage, 0)
	m.layout.chatLayout.Show(event, s)
}

// handleConfirmWithReason opens a modal for entering a custom confirm reason.
// The modal pre-fills with the current reason if one exists.
func (m *ReviewMenu) handleConfirmWithReason(event *events.ComponentInteractionCreate, s *session.Session) {
	var group *types.ReviewGroup
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
		m.layout.logger.Error("Failed to create modal", zap.Error(err))
		m.layout.paginationManager.RespondWithError(event, "Failed to open the confirm reason form. Please try again.")
	}
}

// handleConfirmGroup moves a group to the confirmed state and logs the action.
func (m *ReviewMenu) handleConfirmGroup(event interfaces.CommonEvent, s *session.Session) {
	var settings *types.UserSetting
	s.GetInterface(constants.SessionKeyUserSettings, &settings)
	var botSettings *types.BotSetting
	s.GetInterface(constants.SessionKeyBotSettings, &botSettings)
	var group *types.ReviewGroup
	s.GetInterface(constants.SessionKeyGroupTarget, &group)

	var actionMsg string
	if settings.ReviewMode == types.TrainingReviewMode {
		// Training mode - increment downvotes
		if err := m.layout.db.Groups().UpdateTrainingVotes(context.Background(), group.ID, false); err != nil {
			m.layout.logger.Error("Failed to update downvotes", zap.Error(err))
			m.layout.paginationManager.RespondWithError(event, "Failed to update downvotes. Please try again.")
			return
		}
		group.Reputation.Downvotes++
		actionMsg = "downvoted"

		// Log the training downvote action
		go m.layout.db.UserActivity().Log(context.Background(), &types.UserActivityLog{
			ActivityTarget: types.ActivityTarget{
				GroupID: group.ID,
			},
			ReviewerID:        uint64(event.User().ID),
			ActivityType:      types.ActivityTypeGroupTrainingDownvote,
			ActivityTimestamp: time.Now(),
			Details: map[string]interface{}{
				"upvotes":   group.Reputation.Upvotes,
				"downvotes": group.Reputation.Downvotes,
			},
		})
	} else {
		// Standard mode - check permissions and confirm group
		if !botSettings.IsReviewer(uint64(event.User().ID)) {
			m.layout.logger.Error("Non-reviewer attempted to confirm group",
				zap.Uint64("user_id", uint64(event.User().ID)))
			m.layout.paginationManager.RespondWithError(event, "You do not have permission to confirm groups.")
			return
		}

		if err := m.layout.db.Groups().ConfirmGroup(context.Background(), group); err != nil {
			m.layout.logger.Error("Failed to confirm group", zap.Error(err))
			m.layout.paginationManager.RespondWithError(event, "Failed to confirm the group. Please try again.")
			return
		}
		actionMsg = "confirmed"

		// Log the confirm action
		go m.layout.db.UserActivity().Log(context.Background(), &types.UserActivityLog{
			ActivityTarget: types.ActivityTarget{
				GroupID: group.ID,
			},
			ReviewerID:        uint64(event.User().ID),
			ActivityType:      types.ActivityTypeGroupConfirmed,
			ActivityTimestamp: time.Now(),
			Details:           map[string]interface{}{"reason": group.Reason},
		})
	}

	// Clear current group and load next one
	s.Delete(constants.SessionKeyGroupTarget)
	m.Show(event, s, fmt.Sprintf("Group %s.", actionMsg))
	m.updateCounters(s)
}

// handleClearGroup removes a group from the flagged state and logs the action.
func (m *ReviewMenu) handleClearGroup(event interfaces.CommonEvent, s *session.Session) {
	var settings *types.UserSetting
	s.GetInterface(constants.SessionKeyUserSettings, &settings)
	var botSettings *types.BotSetting
	s.GetInterface(constants.SessionKeyBotSettings, &botSettings)
	var group *types.ReviewGroup
	s.GetInterface(constants.SessionKeyGroupTarget, &group)

	var actionMsg string
	if settings.ReviewMode == types.TrainingReviewMode {
		// Training mode - increment upvotes
		if err := m.layout.db.Groups().UpdateTrainingVotes(context.Background(), group.ID, true); err != nil {
			m.layout.logger.Error("Failed to update upvotes", zap.Error(err))
			m.layout.paginationManager.RespondWithError(event, "Failed to update upvotes. Please try again.")
			return
		}
		group.Reputation.Upvotes++
		actionMsg = "upvoted"

		// Log the training upvote action
		go m.layout.db.UserActivity().Log(context.Background(), &types.UserActivityLog{
			ActivityTarget: types.ActivityTarget{
				GroupID: group.ID,
			},
			ReviewerID:        uint64(event.User().ID),
			ActivityType:      types.ActivityTypeGroupTrainingUpvote,
			ActivityTimestamp: time.Now(),
			Details: map[string]interface{}{
				"upvotes":   group.Reputation.Upvotes,
				"downvotes": group.Reputation.Downvotes,
			},
		})
	} else {
		// Standard mode - check permissions and clear group
		if !botSettings.IsReviewer(uint64(event.User().ID)) {
			m.layout.logger.Error("Non-reviewer attempted to clear group",
				zap.Uint64("user_id", uint64(event.User().ID)))
			m.layout.paginationManager.RespondWithError(event, "You do not have permission to clear groups.")
			return
		}

		if err := m.layout.db.Groups().ClearGroup(context.Background(), group); err != nil {
			m.layout.logger.Error("Failed to clear group", zap.Error(err))
			m.layout.paginationManager.RespondWithError(event, "Failed to clear the group. Please try again.")
			return
		}
		actionMsg = "cleared"

		// Log the clear action
		go m.layout.db.UserActivity().Log(context.Background(), &types.UserActivityLog{
			ActivityTarget: types.ActivityTarget{
				GroupID: group.ID,
			},
			ReviewerID:        uint64(event.User().ID),
			ActivityType:      types.ActivityTypeGroupCleared,
			ActivityTimestamp: time.Now(),
			Details:           make(map[string]interface{}),
		})
	}

	// Clear current group and load next one
	s.Delete(constants.SessionKeyGroupTarget)
	m.Show(event, s, fmt.Sprintf("Group %s.", actionMsg))
	m.updateCounters(s)
}

// handleSkipGroup logs the skip action and moves to the next group.
func (m *ReviewMenu) handleSkipGroup(event interfaces.CommonEvent, s *session.Session) {
	var settings *types.UserSetting
	s.GetInterface(constants.SessionKeyUserSettings, &settings)

	// Check if skipping is allowed
	if msg := settings.SkipUsage.CanSkip(); msg != "" {
		m.layout.paginationManager.NavigateTo(event, s, m.page, msg)
		return
	}

	// Clear current group and load next one
	s.Delete(constants.SessionKeyGroupTarget)
	m.Show(event, s, "Skipped group.")

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
	var group *types.ReviewGroup
	s.GetInterface(constants.SessionKeyGroupTarget, &group)

	m.layout.db.UserActivity().Log(context.Background(), &types.UserActivityLog{
		ActivityTarget: types.ActivityTarget{
			GroupID: group.ID,
		},
		ReviewerID:        uint64(event.User().ID),
		ActivityType:      types.ActivityTypeGroupSkipped,
		ActivityTimestamp: time.Now(),
		Details:           make(map[string]interface{}),
	})
}

// handleConfirmWithReasonModalSubmit processes the custom confirm reason from the modal.
func (m *ReviewMenu) handleConfirmWithReasonModalSubmit(event *events.ModalSubmitInteractionCreate, s *session.Session) {
	var group *types.ReviewGroup
	s.GetInterface(constants.SessionKeyGroupTarget, &group)

	// Get and validate the confirm reason
	reason := event.Data.Text(constants.GroupConfirmReasonInputCustomID)
	if reason == "" {
		m.layout.paginationManager.NavigateTo(event, s, m.page, "Confirm reason cannot be empty. Please try again.")
		return
	}

	// Update group's reason with the custom input
	group.Reason = reason

	// Update group status in database
	if err := m.layout.db.Groups().ConfirmGroup(context.Background(), group); err != nil {
		m.layout.logger.Error("Failed to confirm group", zap.Error(err))
		m.layout.paginationManager.RespondWithError(event, "Failed to confirm the group. Please try again.")
		return
	}

	// Clear current group and load next one
	s.Delete(constants.SessionKeyGroupTarget)
	m.Show(event, s, "Group confirmed.")
	m.updateCounters(s)

	// Log the custom confirm action
	m.layout.db.UserActivity().Log(context.Background(), &types.UserActivityLog{
		ActivityTarget: types.ActivityTarget{
			GroupID: group.ID,
		},
		ReviewerID:        uint64(event.User().ID),
		ActivityType:      types.ActivityTypeGroupConfirmedCustom,
		ActivityTimestamp: time.Now(),
		Details:           map[string]interface{}{"reason": group.Reason},
	})
}

// checkLastViewed checks if the current target group has timed out and needs to be refreshed.
// Clears the current group and loads a new one if the timeout is detected.
func (m *ReviewMenu) checkLastViewed(event interfaces.CommonEvent, s *session.Session) bool {
	var group *types.ReviewGroup
	s.GetInterface(constants.SessionKeyGroupTarget, &group)

	isLookupMode := s.GetBool(constants.SessionKeyIsLookupMode)

	// Check if more than 5 minutes have passed since last view
	if group != nil && time.Since(group.LastViewed) > 5*time.Minute && !isLookupMode {
		// Clear current group and load new one
		s.Delete(constants.SessionKeyGroupTarget)
		m.Show(event, s, "Previous review timed out after 5 minutes of inactivity. Showing new group.")
		return true
	}

	return false
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
