package group

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	builder "github.com/robalyx/rotector/internal/bot/builder/review/group"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/pagination"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/bot/interfaces"
	"github.com/robalyx/rotector/internal/bot/menu/log"
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

// NewMenu creates a new review menu.
func NewReviewMenu(layout *Layout) *ReviewMenu {
	m := &ReviewMenu{layout: layout}
	m.page = &pagination.Page{
		Name: constants.GroupReviewPageName,
		Message: func(s *session.Session) *discord.MessageUpdateBuilder {
			return builder.NewReviewBuilder(s, layout.db).Build()
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

	// If no group is set in session, fetch a new one
	group := session.GroupTarget.Get(s)
	if group == nil {
		var isBanned bool
		var err error
		group, isBanned, err = m.fetchNewTarget(event, s, r)
		if err != nil {
			if errors.Is(err, types.ErrNoGroupsToReview) {
				r.Cancel(event, s, "No groups to review. Please check back later.")
				return
			}
			if errors.Is(err, ErrBreakRequired) {
				return
			}
			m.layout.logger.Error("Failed to fetch a new group", zap.Error(err))
			r.Error(event, "Failed to fetch a new group. Please try again.")
			return
		}

		if isBanned {
			r.Show(event, s, constants.BanPageName, "You have been banned for suspicious voting patterns.")
			return
		}
	}

	// Fetch latest group info from API
	groupInfo, err := m.layout.roAPI.Groups().GetGroupInfo(context.Background(), group.ID)
	if err != nil {
		m.layout.logger.Error("Failed to fetch group info",
			zap.Error(err),
			zap.Uint64("groupID", group.ID))
		r.Error(event, "Failed to fetch latest group information. Please try again.")
		return
	}

	// Store group info in session
	session.GroupInfo.Set(s, groupInfo)

	// Fetch review logs for the group
	logs, nextCursor, err := m.layout.db.Model().Activity().GetLogs(
		context.Background(),
		types.ActivityFilter{
			GroupID:      group.ID,
			ReviewerID:   0,
			ActivityType: enum.ActivityTypeAll,
			StartDate:    time.Time{},
			EndDate:      time.Time{},
		},
		nil,
		constants.ReviewHistoryLimit,
	)
	if err != nil {
		m.layout.logger.Error("Failed to fetch review logs", zap.Error(err))
		logs = []*types.ActivityLog{} // Continue without logs - not critical
	}

	// Store logs in session
	session.ReviewLogs.Set(s, logs)
	session.ReviewLogsHasMore.Set(s, nextCursor != nil)
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

	// Update user's group sort preference
	session.UserGroupDefaultSort.Set(s, sortBy)

	r.Reload(event, s, "Changed sort order. Will take effect for the next group.")
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
		constants.GroupViewLogsButtonCustomID,
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
	case constants.GroupViewMembersButtonCustomID:
		session.PaginationPage.Set(s, 0)
		r.Show(event, s, constants.GroupMembersPageName, "")
	case constants.OpenAIChatButtonCustomID:
		m.handleOpenAIChat(event, s, r)
	case constants.GroupViewLogsButtonCustomID:
		m.handleViewGroupLogs(event, s, r)
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

// handleButton processes button clicks.
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
		m.handleConfirmGroup(event, s, r)
	case constants.ClearButtonCustomID:
		m.handleClearGroup(event, s, r)
	case constants.SkipButtonCustomID:
		m.handleSkipGroup(event, s, r)
	}
}

// handleModal handles modal submissions for the review menu.
func (m *ReviewMenu) handleModal(event *events.ModalSubmitInteractionCreate, s *session.Session, r *pagination.Respond) {
	if m.checkCaptchaRequired(event, s, r) {
		return
	}

	switch event.Data.CustomID {
	case constants.AddReasonModalCustomID:
		m.handleReasonModalSubmit(event, s, r)
	}
}

// handleOpenAIChat handles the button to open the AI chat for the current group.
func (m *ReviewMenu) handleOpenAIChat(event *events.ComponentInteractionCreate, s *session.Session, r *pagination.Respond) {
	group := session.GroupTarget.Get(s)
	groupInfo := session.GroupInfo.Get(s)
	memberIDs := session.GroupMemberIDs.Get(s)

	// Get flagged members details with a limit of 20
	limit := 20
	if len(memberIDs) > limit {
		memberIDs = memberIDs[:limit]
	}

	flaggedMembers, err := m.layout.db.Model().User().GetUsersByIDs(
		context.Background(),
		memberIDs,
		types.UserFieldBasic|types.UserFieldReasons|types.UserFieldConfidence,
	)
	if err != nil {
		m.layout.logger.Error("Failed to get flagged members data", zap.Error(err))
	}

	// Build flagged members information
	membersInfo := make([]string, 0, len(flaggedMembers))
	for _, member := range flaggedMembers {
		messages := member.Reasons.Messages()
		membersInfo = append(membersInfo, fmt.Sprintf("- %s (ID: %d) | Status: %s | Reasons: %s | Confidence: %.2f",
			member.Name,
			member.ID,
			member.Status.String(),
			strings.Join(messages, "; "),
			member.Confidence))
	}

	// Add note if there are more members
	totalMembers := len(memberIDs)
	if totalMembers > limit {
		membersInfo = append(membersInfo, fmt.Sprintf("\n... and %d more flagged members", totalMembers-limit))
	}

	// Format shout information
	shoutInfo := "No shout available"
	if group.Shout != nil {
		shoutInfo = fmt.Sprintf("Posted by: %s\nContent: %s\nPosted at: %s",
			group.Shout.Poster.Username,
			group.Shout.Body,
			group.Shout.Created.Format(time.RFC3339))
	}

	// Create context message about the group
	context := fmt.Sprintf(`<context>
Group Information:

Basic Info:
- Name: %s
- ID: %d
- Description: %s
- Owner: %s (ID: %d)
- Total Members: %d
- Reasons: %s
- Confidence: %.2f

Status Information:
- Current Status: %s
- Reputation: %d Reports, %d Safe Votes
- Last Updated: %s

Shout Information:
%s

Flagged Members (%d total, showing first %d):
%s</context>`,
		group.Name,
		group.ID,
		group.Description,
		group.Owner.Username,
		group.Owner.UserID,
		groupInfo.MemberCount,
		group.Reasons.Messages(),
		group.Confidence,
		group.Status.String(),
		group.Reputation.Downvotes,
		group.Reputation.Upvotes,
		group.LastUpdated.Format(time.RFC3339),
		strings.ReplaceAll(shoutInfo, "\n", " "),
		totalMembers,
		limit,
		strings.Join(membersInfo, " "),
	)

	// Update session and navigate to chat
	session.ChatContext.Set(s, context)
	session.PaginationPage.Set(s, 0)
	r.Show(event, s, constants.ChatPageName, "")
}

// handleViewGroupLogs handles the shortcut to view group logs.
func (m *ReviewMenu) handleViewGroupLogs(
	event *events.ComponentInteractionCreate, s *session.Session, r *pagination.Respond,
) {
	// Get current group
	group := session.GroupTarget.Get(s)

	// Reset logs and filters
	log.ResetLogs(s)
	log.ResetFilters(s)
	session.LogFilterGroupID.Set(s, group.ID)

	// Show the logs menu
	r.Show(event, s, constants.LogPageName, "")
}

// handleConfirmGroup moves a group to the confirmed state and logs the action.
func (m *ReviewMenu) handleConfirmGroup(event interfaces.CommonEvent, s *session.Session, r *pagination.Respond) {
	group := session.GroupTarget.Get(s)

	var actionMsg string
	if session.UserReviewMode.Get(s) == enum.ReviewModeTraining {
		// Training mode - increment downvotes
		if err := m.layout.db.Service().Reputation().UpdateGroupVotes(
			context.Background(), group.ID, uint64(event.User().ID), false,
		); err != nil {
			m.layout.logger.Error("Failed to update downvotes", zap.Error(err))
			r.Error(event, "Failed to update downvotes. Please try again.")
			return
		}
		group.Reputation.Downvotes++
		actionMsg = "downvoted"

		// Log the training downvote action
		go m.layout.db.Model().Activity().Log(context.Background(), &types.ActivityLog{
			ActivityTarget: types.ActivityTarget{
				GroupID: group.ID,
			},
			ReviewerID:        uint64(event.User().ID),
			ActivityType:      enum.ActivityTypeGroupTrainingDownvote,
			ActivityTimestamp: time.Now(),
			Details: map[string]any{
				"upvotes":   group.Reputation.Upvotes,
				"downvotes": group.Reputation.Downvotes,
			},
		})
	} else {
		// Standard mode - check permissions and confirm group
		if !s.BotSettings().IsReviewer(uint64(event.User().ID)) {
			m.layout.logger.Error("Non-reviewer attempted to confirm group",
				zap.Uint64("user_id", uint64(event.User().ID)))
			r.Error(event, "You do not have permission to confirm groups.")
			return
		}

		// Calculate vote percentages
		totalVotes := float64(group.Reputation.Upvotes + group.Reputation.Downvotes)
		if totalVotes >= constants.MinimumVotesRequired {
			upvotePercentage := float64(group.Reputation.Upvotes) / totalVotes

			// If there's a strong consensus for clearing, prevent confirmation
			if upvotePercentage >= constants.VoteConsensusThreshold {
				r.Cancel(event, s, fmt.Sprintf("Cannot confirm - %.0f%% of %d votes indicate this group is safe",
					upvotePercentage*100, int(totalVotes)))
				return
			}
		}

		// Confirm the group
		if err := m.layout.db.Service().Group().ConfirmGroup(context.Background(), group); err != nil {
			m.layout.logger.Error("Failed to confirm group", zap.Error(err))
			r.Error(event, "Failed to confirm the group. Please try again.")
			return
		}
		actionMsg = "confirmed"

		// Log reason changes if any were made
		if session.ReasonsChanged.Get(s) {
			originalReasons := session.OriginalGroupReasons.Get(s)
			go m.layout.db.Model().Activity().Log(context.Background(), &types.ActivityLog{
				ActivityTarget: types.ActivityTarget{
					GroupID: group.ID,
				},
				ReviewerID:        uint64(event.User().ID),
				ActivityType:      enum.ActivityTypeGroupReasonUpdated,
				ActivityTimestamp: time.Now(),
				Details: map[string]any{
					"originalReasons": originalReasons.Messages(),
					"updatedReasons":  group.Reasons.Messages(),
				},
			})
		}

		// Log the confirm action
		go m.layout.db.Model().Activity().Log(context.Background(), &types.ActivityLog{
			ActivityTarget: types.ActivityTarget{
				GroupID: group.ID,
			},
			ReviewerID:        uint64(event.User().ID),
			ActivityType:      enum.ActivityTypeGroupConfirmed,
			ActivityTimestamp: time.Now(),
			Details: map[string]any{
				"reasons": group.Reasons.Messages(),
			},
		})
	}

	// Clear current group and load next one
	session.GroupTarget.Delete(s)
	r.Reload(event, s, fmt.Sprintf("Group %s.", actionMsg))
	m.updateCounters(s)
}

// handleClearGroup removes a group from the flagged state and logs the action.
func (m *ReviewMenu) handleClearGroup(event interfaces.CommonEvent, s *session.Session, r *pagination.Respond) {
	group := session.GroupTarget.Get(s)

	var actionMsg string
	if session.UserReviewMode.Get(s) == enum.ReviewModeTraining {
		// Training mode - increment upvotes
		if err := m.layout.db.Service().Reputation().UpdateGroupVotes(
			context.Background(), group.ID, uint64(event.User().ID), true,
		); err != nil {
			m.layout.logger.Error("Failed to update upvotes", zap.Error(err))
			r.Error(event, "Failed to update upvotes. Please try again.")
			return
		}
		group.Reputation.Upvotes++
		actionMsg = "upvoted"

		// Log the training upvote action
		go m.layout.db.Model().Activity().Log(context.Background(), &types.ActivityLog{
			ActivityTarget: types.ActivityTarget{
				GroupID: group.ID,
			},
			ReviewerID:        uint64(event.User().ID),
			ActivityType:      enum.ActivityTypeGroupTrainingUpvote,
			ActivityTimestamp: time.Now(),
			Details: map[string]any{
				"upvotes":   group.Reputation.Upvotes,
				"downvotes": group.Reputation.Downvotes,
			},
		})
	} else {
		// Standard mode - check permissions and clear group
		if !s.BotSettings().IsReviewer(uint64(event.User().ID)) {
			m.layout.logger.Error("Non-reviewer attempted to clear group",
				zap.Uint64("user_id", uint64(event.User().ID)))
			r.Error(event, "You do not have permission to clear groups.")
			return
		}

		// Calculate vote percentages
		totalVotes := float64(group.Reputation.Upvotes + group.Reputation.Downvotes)
		if totalVotes >= constants.MinimumVotesRequired {
			downvotePercentage := float64(group.Reputation.Downvotes) / totalVotes

			// If there's a strong consensus for confirming, prevent clearing
			if downvotePercentage >= constants.VoteConsensusThreshold {
				r.Cancel(event, s, fmt.Sprintf("Cannot clear - %.0f%% of %d votes indicate this group is suspicious",
					downvotePercentage*100, int(totalVotes)))
				return
			}
		}

		// Log reason changes if any were made
		if session.ReasonsChanged.Get(s) {
			originalReasons := session.OriginalGroupReasons.Get(s)
			go m.layout.db.Model().Activity().Log(context.Background(), &types.ActivityLog{
				ActivityTarget: types.ActivityTarget{
					GroupID: group.ID,
				},
				ReviewerID:        uint64(event.User().ID),
				ActivityType:      enum.ActivityTypeGroupReasonUpdated,
				ActivityTimestamp: time.Now(),
				Details: map[string]any{
					"originalReasons": originalReasons.Messages(),
					"updatedReasons":  group.Reasons.Messages(),
				},
			})
		}

		// Clear the group
		if err := m.layout.db.Service().Group().ClearGroup(context.Background(), group); err != nil {
			m.layout.logger.Error("Failed to clear group", zap.Error(err))
			r.Error(event, "Failed to clear the group. Please try again.")
			return
		}
		actionMsg = "cleared"

		// Log the clear action
		go m.layout.db.Model().Activity().Log(context.Background(), &types.ActivityLog{
			ActivityTarget: types.ActivityTarget{
				GroupID: group.ID,
			},
			ReviewerID:        uint64(event.User().ID),
			ActivityType:      enum.ActivityTypeGroupCleared,
			ActivityTimestamp: time.Now(),
			Details:           map[string]any{},
		})
	}

	// Clear current group and load next one
	session.GroupTarget.Delete(s)
	r.Reload(event, s, fmt.Sprintf("Group %s.", actionMsg))
	m.updateCounters(s)
}

// handleSkipGroup logs the skip action and moves to the next group.
func (m *ReviewMenu) handleSkipGroup(event interfaces.CommonEvent, s *session.Session, r *pagination.Respond) {
	// Clear current group and load next one
	session.GroupTarget.Delete(s)
	r.Reload(event, s, "Skipped group.")
	m.updateCounters(s)

	// Log the skip action
	group := session.GroupTarget.Get(s)
	if group != nil {
		m.layout.db.Model().Activity().Log(context.Background(), &types.ActivityLog{
			ActivityTarget: types.ActivityTarget{
				GroupID: group.ID,
			},
			ReviewerID:        uint64(event.User().ID),
			ActivityType:      enum.ActivityTypeGroupSkipped,
			ActivityTimestamp: time.Now(),
			Details:           map[string]any{},
		})
	}
}

// handleReasonModalSubmit processes the reason message from the modal.
func (m *ReviewMenu) handleReasonModalSubmit(
	event *events.ModalSubmitInteractionCreate, s *session.Session, r *pagination.Respond,
) {
	// Get the reason type from session
	reasonTypeStr := session.SelectedReasonType.Get(s)
	reasonType, err := enum.GroupReasonTypeString(reasonTypeStr)
	if err != nil {
		r.Error(event, "Invalid reason type: "+reasonTypeStr)
		return
	}

	// Get current group
	group := session.GroupTarget.Get(s)

	// Initialize reasons map if nil
	if group.Reasons == nil {
		group.Reasons = make(types.Reasons[enum.GroupReasonType])
	}

	// Get the reason message from the modal
	reasonMessage := event.Data.Text(constants.AddReasonInputCustomID)
	confidenceStr := event.Data.Text(constants.AddReasonConfidenceInputCustomID)
	evidenceText := event.Data.Text(constants.AddReasonEvidenceInputCustomID)

	// Get existing reason if editing
	var existingReason *types.Reason
	if existing, exists := group.Reasons[reasonType]; exists {
		existingReason = existing
	}

	// Create or update reason
	var reason types.Reason
	if existingReason != nil {
		// Check if reasons field is empty
		if reasonMessage == "" {
			delete(group.Reasons, reasonType)
			group.Confidence = utils.CalculateConfidence(group.Reasons)

			// Update session
			session.GroupTarget.Set(s, group)
			session.SelectedReasonType.Delete(s)
			session.ReasonsChanged.Set(s, true)

			r.Reload(event, s, fmt.Sprintf("Successfully removed %s reason", reasonType.String()))
			return
		}

		// Check if confidence is empty
		if confidenceStr == "" {
			r.Cancel(event, s, "Confidence is required when updating a reason.")
			return
		}

		// Parse confidence
		confidence, err := strconv.ParseFloat(confidenceStr, 64)
		if err != nil || confidence < 0.01 || confidence > 1.0 {
			r.Cancel(event, s, "Invalid confidence value. Please enter a number between 0.01 and 1.00.")
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
			r.Cancel(event, s, "Reason message and confidence are required for new reasons.")
			return
		}

		// Parse confidence
		confidence, err := strconv.ParseFloat(confidenceStr, 64)
		if err != nil || confidence < 0.01 || confidence > 1.0 {
			r.Cancel(event, s, "Invalid confidence value. Please enter a number between 0.01 and 1.00.")
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
	group.Reasons[reasonType] = &reason

	// Recalculate overall confidence
	group.Confidence = utils.CalculateConfidence(group.Reasons)

	// Update session
	session.GroupTarget.Set(s, group)
	session.SelectedReasonType.Delete(s)
	session.ReasonsChanged.Set(s, true)

	action := "added"
	if existingReason != nil {
		action = "updated"
	}
	r.Reload(event, s, fmt.Sprintf("Successfully %s %s reason", action, reasonType.String()))
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

	// Get current group
	group := session.GroupTarget.Get(s)

	// Handle refresh option
	if option == constants.RefreshButtonCustomID {
		// Restore original reasons
		originalReasons := session.OriginalGroupReasons.Get(s)
		group.Reasons = originalReasons
		group.Confidence = utils.CalculateConfidence(group.Reasons)

		// Update session
		session.GroupTarget.Set(s, group)
		session.ReasonsChanged.Set(s, false)

		r.Reload(event, s, "Successfully restored original reasons")
		return
	}

	// Parse reason type
	option = strings.TrimSuffix(option, constants.ModalOpenSuffix)
	reasonType, err := enum.GroupReasonTypeString(option)
	if err != nil {
		r.Error(event, "Invalid reason type: "+option)
		return
	}

	// Initialize reasons map if nil
	if group.Reasons == nil {
		group.Reasons = make(types.Reasons[enum.GroupReasonType])
	}

	// Store the selected reason type in session
	session.SelectedReasonType.Set(s, option)

	// Check if we're editing an existing reason
	var existingReason *types.Reason
	if existing, exists := group.Reasons[reasonType]; exists {
		existingReason = existing
	}

	// Show modal to user
	r.Modal(event, s, m.buildReasonModal(reasonType, existingReason))
}

// buildReasonModal creates a modal for adding or editing a reason.
func (m *ReviewMenu) buildReasonModal(
	reasonType enum.GroupReasonType, existingReason *types.Reason,
) *discord.ModalCreateBuilder {
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
			WithPlaceholder("Enter the reason for flagging this group")
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

// fetchNewTarget gets a new group to review based on the current sort order.
func (m *ReviewMenu) fetchNewTarget(
	event interfaces.CommonEvent, s *session.Session, r *pagination.Respond,
) (*types.ReviewGroup, bool, error) {
	if m.checkBreakRequired(event, s, r) {
		return nil, false, ErrBreakRequired
	}

	// Check if user is banned for low accuracy
	isBanned, err := m.layout.db.Service().Vote().CheckVoteAccuracy(context.Background(), uint64(event.User().ID))
	if err != nil {
		m.layout.logger.Error("Failed to check vote accuracy",
			zap.Error(err),
			zap.Uint64("user_id", uint64(event.User().ID)))
		// Continue anyway - not a big requirement
	}

	// Get the next group to review
	reviewerID := uint64(event.User().ID)
	defaultSort := session.UserGroupDefaultSort.Get(s)
	reviewTargetMode := session.UserReviewTargetMode.Get(s)

	group, err := m.layout.db.Service().Group().GetGroupToReview(
		context.Background(), defaultSort, reviewTargetMode, reviewerID,
	)
	if err != nil {
		return nil, isBanned, err
	}

	// Get flagged users from tracking
	flaggedUsers, err := m.layout.db.Model().Tracking().GetFlaggedUsers(context.Background(), group.ID)
	if err != nil {
		return nil, isBanned, err
	}

	// Store the group, flagged users, and original reasons in session
	session.GroupTarget.Set(s, group)
	session.GroupMemberIDs.Set(s, flaggedUsers)
	session.OriginalGroupReasons.Set(s, group.Reasons)
	session.ReasonsChanged.Set(s, false)

	// Log the view action
	go m.layout.db.Model().Activity().Log(context.Background(), &types.ActivityLog{
		ActivityTarget: types.ActivityTarget{
			GroupID: group.ID,
		},
		ReviewerID:        reviewerID,
		ActivityType:      enum.ActivityTypeGroupViewed,
		ActivityTimestamp: time.Now(),
		Details:           map[string]any{},
	})

	return group, isBanned, nil
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
