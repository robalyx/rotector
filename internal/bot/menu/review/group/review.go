package group

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/disgoorg/disgo/discord"
	builder "github.com/robalyx/rotector/internal/bot/builder/review/group"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/interaction"
	"github.com/robalyx/rotector/internal/bot/core/session"
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
	page   *interaction.Page
}

// NewMenu creates a new review menu.
func NewReviewMenu(layout *Layout) *ReviewMenu {
	m := &ReviewMenu{layout: layout}
	m.page = &interaction.Page{
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
func (m *ReviewMenu) Show(ctx *interaction.Context, s *session.Session) {
	// If no group is set in session, fetch a new one
	group := session.GroupTarget.Get(s)
	if group == nil {
		var isBanned bool
		var err error
		group, isBanned, err = m.fetchNewTarget(ctx, s)
		if err != nil {
			if errors.Is(err, types.ErrNoGroupsToReview) {
				ctx.Show(constants.DashboardPageName, "No groups to review. Please check back later.")
				return
			}
			if errors.Is(err, ErrBreakRequired) {
				return
			}
			m.layout.logger.Error("Failed to fetch a new group", zap.Error(err))
			ctx.Error("Failed to fetch a new group. Please try again.")
			return
		}

		if isBanned {
			ctx.Show(constants.BanPageName, "You have been banned for suspicious voting patterns.")
			return
		}
	}

	// Fetch latest group info from API
	groupInfo, err := m.layout.roAPI.Groups().GetGroupInfo(ctx.Context(), group.ID)
	if err != nil {
		m.layout.logger.Error("Failed to fetch group info",
			zap.Error(err),
			zap.Uint64("groupID", group.ID))
		ctx.Error("Failed to fetch latest group information. Please try again.")
		return
	}

	// Store group info in session
	session.GroupInfo.Set(s, groupInfo)

	// Fetch review logs for the group
	logs, nextCursor, err := m.layout.db.Model().Activity().GetLogs(
		ctx.Context(),
		types.ActivityFilter{
			GroupID:      group.ID,
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

	// Fetch comments for the group
	comments, err := m.layout.db.Model().Comment().GetGroupComments(ctx.Context(), group.ID)
	if err != nil {
		m.layout.logger.Error("Failed to fetch group comments", zap.Error(err))
		comments = []*types.Comment{} // Continue without comments - not critical
	}
	session.ReviewComments.Set(s, comments)
}

// handleSelectMenu processes select menu interactions.
func (m *ReviewMenu) handleSelectMenu(ctx *interaction.Context, s *session.Session, customID, option string) {
	if m.checkCaptchaRequired(ctx, s) {
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

	// Update user's group sort preference
	session.UserGroupDefaultSort.Set(s, sortBy)

	ctx.Reload("Changed sort order. Will take effect for the next group.")
}

// handleActionSelection processes action menu selections.
func (m *ReviewMenu) handleActionSelection(ctx *interaction.Context, s *session.Session, option string) {
	userID := uint64(ctx.Event().User().ID)
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
			ctx.Error("You do not have permission to perform this action.")
			return
		}
	}

	// Process selected option
	switch option {
	case constants.GroupViewMembersButtonCustomID:
		session.PaginationPage.Set(s, 0)
		ctx.Show(constants.GroupMembersPageName, "")
	case constants.ViewCommentsButtonCustomID:
		session.PaginationPage.Set(s, 0)
		ctx.Show(constants.GroupCommentsPageName, "")
	case constants.OpenAIChatButtonCustomID:
		m.handleOpenAIChat(ctx, s)
	case constants.GroupViewLogsButtonCustomID:
		m.handleViewGroupLogs(ctx, s)
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
	if m.checkCaptchaRequired(ctx, s) {
		return
	}

	switch customID {
	case constants.BackButtonCustomID:
		ctx.NavigateBack("")
	case constants.PrevReviewButtonCustomID:
		m.handleNavigateGroup(ctx, s, false)
	case constants.NextReviewButtonCustomID:
		m.handleNavigateGroup(ctx, s, true)
	case constants.ConfirmButtonCustomID:
		m.handleConfirmGroup(ctx, s)
	case constants.ClearButtonCustomID:
		m.handleClearGroup(ctx, s)
	}
}

// handleModal handles modal submissions for the review menu.
func (m *ReviewMenu) handleModal(ctx *interaction.Context, s *session.Session) {
	if m.checkCaptchaRequired(ctx, s) {
		return
	}

	switch ctx.Event().CustomID() {
	case constants.AddReasonModalCustomID:
		m.handleReasonModalSubmit(ctx, s)
	}
}

// handleOpenAIChat handles the button to open the AI chat for the current group.
func (m *ReviewMenu) handleOpenAIChat(ctx *interaction.Context, s *session.Session) {
	group := session.GroupTarget.Get(s)
	groupInfo := session.GroupInfo.Get(s)

	// Get flagged users from tracking
	memberIDs, err := m.layout.db.Model().Tracking().GetFlaggedUsers(ctx.Context(), group.ID)
	if err != nil {
		m.layout.logger.Error("Failed to fetch flagged users", zap.Error(err))
		ctx.Error("Failed to load flagged users. Please try again.")
		return
	}

	// Get flagged members details with a limit of 20
	limit := 20
	if len(memberIDs) > limit {
		memberIDs = memberIDs[:limit]
	}

	flaggedMembers, err := m.layout.db.Model().User().GetUsersByIDs(
		ctx.Context(),
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
	ctx.Show(constants.ChatPageName, "")
}

// handleViewGroupLogs handles the shortcut to view group logs.
func (m *ReviewMenu) handleViewGroupLogs(ctx *interaction.Context, s *session.Session) {
	// Get current group
	group := session.GroupTarget.Get(s)

	// Reset logs and filters
	log.ResetLogs(s)
	log.ResetFilters(s)
	session.LogFilterGroupID.Set(s, group.ID)

	// Show the logs menu
	ctx.Show(constants.LogPageName, "")
}

// handleNavigateGroup handles navigation to previous or next group based on the button pressed.
func (m *ReviewMenu) handleNavigateGroup(ctx *interaction.Context, s *session.Session, isNext bool) {
	// Get the review history and current index
	history := session.GroupReviewHistory.Get(s)
	index := session.GroupReviewHistoryIndex.Get(s)

	// If navigating next and we're at the end of history, treat it as a skip
	if isNext && (index >= len(history)-1 || len(history) == 0) {
		// Clear current group and load next one
		session.GroupTarget.Delete(s)
		ctx.Reload("Skipped group.")
		m.updateCounters(s)

		// Log the skip action
		group := session.GroupTarget.Get(s)
		if group != nil {
			m.layout.db.Model().Activity().Log(ctx.Context(), &types.ActivityLog{
				ActivityTarget: types.ActivityTarget{
					GroupID: group.ID,
				},
				ReviewerID:        uint64(ctx.Event().User().ID),
				ActivityType:      enum.ActivityTypeGroupSkipped,
				ActivityTimestamp: time.Now(),
				Details:           map[string]any{},
			})
		}
		return
	}

	// For previous navigation or when there's history to navigate
	if isNext {
		if index >= len(history)-1 {
			ctx.Cancel("No next group to navigate to.")
			return
		}
		index++
	} else {
		if index <= 0 || len(history) == 0 {
			ctx.Cancel("No previous group to navigate to.")
			return
		}
		index--
	}

	// Update index in session
	session.GroupReviewHistoryIndex.Set(s, index)

	// Fetch the group data
	targetGroupID := history[index]
	m.layout.logger.Info("Fetching group", zap.Uint64("group_id", targetGroupID))
	group, err := m.layout.db.Service().Group().GetGroupByID(
		ctx.Context(),
		strconv.FormatUint(targetGroupID, 10),
		types.GroupFieldAll,
	)
	if err != nil {
		if errors.Is(err, types.ErrGroupNotFound) {
			// Remove the missing group from history
			history = slices.Delete(history, index, index+1)
			session.GroupReviewHistory.Set(s, history)

			// Adjust index if needed
			if index >= len(history) {
				index = len(history) - 1
			}
			session.GroupReviewHistoryIndex.Set(s, index)

			// Try again with updated history
			m.handleNavigateGroup(ctx, s, isNext)
			return
		}

		direction := map[bool]string{true: "next", false: "previous"}[isNext]
		m.layout.logger.Error(fmt.Sprintf("Failed to fetch %s group", direction), zap.Error(err))
		ctx.Error(fmt.Sprintf("Failed to load %s group. Please try again.", direction))
		return
	}

	// Get flagged users from tracking
	flaggedCount, err := m.layout.db.Model().Tracking().GetFlaggedUsersCount(ctx.Context(), group.ID)
	if err != nil {
		m.layout.logger.Error("Failed to fetch flagged users", zap.Error(err))
		ctx.Error("Failed to load flagged users. Please try again.")
		return
	}

	// Store in session
	session.GroupTarget.Set(s, group)
	session.GroupFlaggedMembersCount.Set(s, flaggedCount)
	session.OriginalGroupReasons.Set(s, group.Reasons)
	session.ReasonsChanged.Set(s, false)

	// Log the view action
	go m.layout.db.Model().Activity().Log(ctx.Context(), &types.ActivityLog{
		ActivityTarget: types.ActivityTarget{
			GroupID: group.ID,
		},
		ReviewerID:        uint64(ctx.Event().User().ID),
		ActivityType:      enum.ActivityTypeGroupViewed,
		ActivityTimestamp: time.Now(),
		Details:           map[string]any{},
	})

	direction := map[bool]string{true: "next", false: "previous"}[isNext]
	ctx.Reload(fmt.Sprintf("Navigated to %s group.", direction))
}

// handleConfirmGroup moves a group to the confirmed state and logs the action.
func (m *ReviewMenu) handleConfirmGroup(ctx *interaction.Context, s *session.Session) {
	group := session.GroupTarget.Get(s)
	isAdmin := s.BotSettings().IsAdmin(uint64(ctx.Event().User().ID))

	var actionMsg string
	if session.UserReviewMode.Get(s) == enum.ReviewModeTraining || !isAdmin {
		// Training mode - increment downvotes
		if err := m.layout.db.Service().Reputation().UpdateGroupVotes(
			ctx.Context(), group.ID, uint64(ctx.Event().User().ID), false,
		); err != nil {
			m.layout.logger.Error("Failed to update downvotes", zap.Error(err))
			ctx.Error("Failed to update downvotes. Please try again.")
			return
		}
		group.Reputation.Downvotes++
		actionMsg = "downvoted"

		// Log the training downvote action
		go m.layout.db.Model().Activity().Log(ctx.Context(), &types.ActivityLog{
			ActivityTarget: types.ActivityTarget{
				GroupID: group.ID,
			},
			ReviewerID:        uint64(ctx.Event().User().ID),
			ActivityType:      enum.ActivityTypeGroupTrainingDownvote,
			ActivityTimestamp: time.Now(),
			Details: map[string]any{
				"upvotes":   group.Reputation.Upvotes,
				"downvotes": group.Reputation.Downvotes,
			},
		})
	} else {
		// Standard mode - check permissions and confirm group
		if !s.BotSettings().IsReviewer(uint64(ctx.Event().User().ID)) {
			m.layout.logger.Error("Non-reviewer attempted to confirm group",
				zap.Uint64("user_id", uint64(ctx.Event().User().ID)))
			ctx.Error("You do not have permission to confirm groups.")
			return
		}

		// Calculate vote percentages
		totalVotes := float64(group.Reputation.Upvotes + group.Reputation.Downvotes)
		if totalVotes >= constants.MinimumVotesRequired {
			upvotePercentage := float64(group.Reputation.Upvotes) / totalVotes

			// If there's a strong consensus for clearing, prevent confirmation
			if upvotePercentage >= constants.VoteConsensusThreshold {
				ctx.Cancel(fmt.Sprintf("Cannot confirm - %.0f%% of %d votes indicate this group is safe",
					upvotePercentage*100, int(totalVotes)))
				return
			}
		}

		// Confirm the group
		if err := m.layout.db.Service().Group().ConfirmGroup(ctx.Context(), group); err != nil {
			m.layout.logger.Error("Failed to confirm group", zap.Error(err))
			ctx.Error("Failed to confirm the group. Please try again.")
			return
		}
		actionMsg = "confirmed"

		// Log reason changes if any were made
		if session.ReasonsChanged.Get(s) {
			originalReasons := session.OriginalGroupReasons.Get(s)
			go m.layout.db.Model().Activity().Log(ctx.Context(), &types.ActivityLog{
				ActivityTarget: types.ActivityTarget{
					GroupID: group.ID,
				},
				ReviewerID:        uint64(ctx.Event().User().ID),
				ActivityType:      enum.ActivityTypeGroupReasonUpdated,
				ActivityTimestamp: time.Now(),
				Details: map[string]any{
					"originalReasons": originalReasons.Messages(),
					"updatedReasons":  group.Reasons.Messages(),
				},
			})
		}

		// Log the confirm action
		go m.layout.db.Model().Activity().Log(ctx.Context(), &types.ActivityLog{
			ActivityTarget: types.ActivityTarget{
				GroupID: group.ID,
			},
			ReviewerID:        uint64(ctx.Event().User().ID),
			ActivityType:      enum.ActivityTypeGroupConfirmed,
			ActivityTimestamp: time.Now(),
			Details: map[string]any{
				"reasons": group.Reasons.Messages(),
			},
		})
	}

	// Clear current group and load next one
	session.GroupTarget.Delete(s)
	ctx.Reload(fmt.Sprintf("Group %s.", actionMsg))
	m.updateCounters(s)
}

// handleClearGroup removes a group from the flagged state and logs the action.
func (m *ReviewMenu) handleClearGroup(ctx *interaction.Context, s *session.Session) {
	group := session.GroupTarget.Get(s)
	isAdmin := s.BotSettings().IsAdmin(uint64(ctx.Event().User().ID))

	var actionMsg string
	if session.UserReviewMode.Get(s) == enum.ReviewModeTraining || !isAdmin {
		// Training mode - increment upvotes
		if err := m.layout.db.Service().Reputation().UpdateGroupVotes(
			ctx.Context(), group.ID, uint64(ctx.Event().User().ID), true,
		); err != nil {
			m.layout.logger.Error("Failed to update upvotes", zap.Error(err))
			ctx.Error("Failed to update upvotes. Please try again.")
			return
		}
		group.Reputation.Upvotes++
		actionMsg = "upvoted"

		// Log the training upvote action
		go m.layout.db.Model().Activity().Log(ctx.Context(), &types.ActivityLog{
			ActivityTarget: types.ActivityTarget{
				GroupID: group.ID,
			},
			ReviewerID:        uint64(ctx.Event().User().ID),
			ActivityType:      enum.ActivityTypeGroupTrainingUpvote,
			ActivityTimestamp: time.Now(),
			Details: map[string]any{
				"upvotes":   group.Reputation.Upvotes,
				"downvotes": group.Reputation.Downvotes,
			},
		})
	} else {
		// Standard mode - check permissions and clear group
		if !s.BotSettings().IsReviewer(uint64(ctx.Event().User().ID)) {
			m.layout.logger.Error("Non-reviewer attempted to clear group",
				zap.Uint64("user_id", uint64(ctx.Event().User().ID)))
			ctx.Error("You do not have permission to clear groups.")
			return
		}

		// Calculate vote percentages
		totalVotes := float64(group.Reputation.Upvotes + group.Reputation.Downvotes)
		if totalVotes >= constants.MinimumVotesRequired {
			downvotePercentage := float64(group.Reputation.Downvotes) / totalVotes

			// If there's a strong consensus for confirming, prevent clearing
			if downvotePercentage >= constants.VoteConsensusThreshold {
				ctx.Cancel(fmt.Sprintf("Cannot clear - %.0f%% of %d votes indicate this group is suspicious",
					downvotePercentage*100, int(totalVotes)))
				return
			}
		}

		// Log reason changes if any were made
		if session.ReasonsChanged.Get(s) {
			originalReasons := session.OriginalGroupReasons.Get(s)
			go m.layout.db.Model().Activity().Log(ctx.Context(), &types.ActivityLog{
				ActivityTarget: types.ActivityTarget{
					GroupID: group.ID,
				},
				ReviewerID:        uint64(ctx.Event().User().ID),
				ActivityType:      enum.ActivityTypeGroupReasonUpdated,
				ActivityTimestamp: time.Now(),
				Details: map[string]any{
					"originalReasons": originalReasons.Messages(),
					"updatedReasons":  group.Reasons.Messages(),
				},
			})
		}

		// Clear the group
		if err := m.layout.db.Service().Group().ClearGroup(ctx.Context(), group); err != nil {
			m.layout.logger.Error("Failed to clear group", zap.Error(err))
			ctx.Error("Failed to clear the group. Please try again.")
			return
		}
		actionMsg = "cleared"

		// Log the clear action
		go m.layout.db.Model().Activity().Log(ctx.Context(), &types.ActivityLog{
			ActivityTarget: types.ActivityTarget{
				GroupID: group.ID,
			},
			ReviewerID:        uint64(ctx.Event().User().ID),
			ActivityType:      enum.ActivityTypeGroupCleared,
			ActivityTimestamp: time.Now(),
			Details:           map[string]any{},
		})
	}

	// Clear current group and load next one
	session.GroupTarget.Delete(s)
	ctx.Reload(fmt.Sprintf("Group %s.", actionMsg))
	m.updateCounters(s)
}

// handleReasonModalSubmit processes the reason message from the modal.
func (m *ReviewMenu) handleReasonModalSubmit(ctx *interaction.Context, s *session.Session) {
	// Get the reason type from session
	reasonTypeStr := session.SelectedReasonType.Get(s)
	reasonType, err := enum.GroupReasonTypeString(reasonTypeStr)
	if err != nil {
		ctx.Error("Invalid reason type: " + reasonTypeStr)
		return
	}

	// Get current group
	group := session.GroupTarget.Get(s)

	// Initialize reasons map if nil
	if group.Reasons == nil {
		group.Reasons = make(types.Reasons[enum.GroupReasonType])
	}

	// Get the reason message from the modal
	data := ctx.Event().ModalData()
	reasonMessage := data.Text(constants.AddReasonInputCustomID)
	confidenceStr := data.Text(constants.AddReasonConfidenceInputCustomID)
	evidenceText := data.Text(constants.AddReasonEvidenceInputCustomID)

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

		ctx.Reload("Successfully restored original reasons")
		return
	}

	// Parse reason type
	option = strings.TrimSuffix(option, constants.ModalOpenSuffix)
	reasonType, err := enum.GroupReasonTypeString(option)
	if err != nil {
		ctx.Error("Invalid reason type: " + option)
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
	ctx.Modal(m.buildReasonModal(reasonType, existingReason))
}

// buildReasonModal creates a modal for adding or editing a reason.
func (m *ReviewMenu) buildReasonModal(reasonType enum.GroupReasonType, existingReason *types.Reason) *discord.ModalCreateBuilder {
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
func (m *ReviewMenu) fetchNewTarget(ctx *interaction.Context, s *session.Session) (*types.ReviewGroup, bool, error) {
	if m.checkBreakRequired(ctx, s) {
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

	// Get the next group to review
	reviewerID := uint64(ctx.Event().User().ID)
	defaultSort := session.UserGroupDefaultSort.Get(s)
	reviewTargetMode := session.UserReviewTargetMode.Get(s)

	group, err := m.layout.db.Service().Group().GetGroupToReview(
		ctx.Context(), defaultSort, reviewTargetMode, reviewerID,
	)
	if err != nil {
		return nil, isBanned, err
	}

	// Get flagged users from tracking
	flaggedCount, err := m.layout.db.Model().Tracking().GetFlaggedUsersCount(ctx.Context(), group.ID)
	if err != nil {
		return nil, isBanned, err
	}

	// Store the group, flagged users, and original reasons in session
	session.GroupTarget.Set(s, group)
	session.GroupFlaggedMembersCount.Set(s, flaggedCount)
	session.OriginalGroupReasons.Set(s, group.Reasons)
	session.ReasonsChanged.Set(s, false)

	// Add current group to history and set index to point to it
	history := session.GroupReviewHistory.Get(s)
	history = append(history, group.ID)

	// Trim history if it exceeds the maximum size
	if len(history) > constants.MaxReviewHistorySize {
		history = history[len(history)-constants.MaxReviewHistorySize:]
	}

	session.GroupReviewHistory.Set(s, history)
	session.GroupReviewHistoryIndex.Set(s, len(history)-1)

	// Log the view action
	go m.layout.db.Model().Activity().Log(ctx.Context(), &types.ActivityLog{
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
func (m *ReviewMenu) checkBreakRequired(ctx *interaction.Context, s *session.Session) bool {
	// Check if user needs a break
	nextReviewTime := session.UserReviewBreakNextReviewTime.Get(s)
	if !nextReviewTime.IsZero() && time.Now().Before(nextReviewTime) {
		// Show timeout menu if break time hasn't passed
		ctx.Show(constants.TimeoutPageName, "")
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
		ctx.Show(constants.TimeoutPageName, "")
		return true
	}

	// Increment review count
	session.UserReviewBreakSessionReviews.Set(s, sessionReviews+1)

	return false
}

// checkCaptchaRequired checks if CAPTCHA verification is needed.
func (m *ReviewMenu) checkCaptchaRequired(ctx *interaction.Context, s *session.Session) bool {
	if m.layout.captcha.IsRequired(s) {
		ctx.Cancel("Please complete CAPTCHA verification to continue.")
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
