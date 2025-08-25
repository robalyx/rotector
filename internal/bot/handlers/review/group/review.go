package group

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/robalyx/rotector/internal/ai"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/interaction"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/bot/handlers/log"
	"github.com/robalyx/rotector/internal/bot/handlers/review/shared"
	view "github.com/robalyx/rotector/internal/bot/views/review/group"
	viewShared "github.com/robalyx/rotector/internal/bot/views/review/shared"
	"github.com/robalyx/rotector/internal/database/types"
	"github.com/robalyx/rotector/internal/database/types/enum"
	"github.com/robalyx/rotector/pkg/utils"
	"go.uber.org/zap"
)

var ErrBreakRequired = errors.New("break required")

// ReviewMenu handles the display and interaction logic for the review interface.
type ReviewMenu struct {
	shared.BaseReviewMenu

	layout *Layout
	page   *interaction.Page
}

// NewReviewMenu creates a new review menu.
func NewReviewMenu(layout *Layout) *ReviewMenu {
	m := &ReviewMenu{
		BaseReviewMenu: *shared.NewBaseReviewMenu(layout.logger, layout.captcha, layout.db),
		layout:         layout,
	}
	m.page = &interaction.Page{
		Name: constants.GroupReviewPageName,
		Message: func(s *session.Session) *discord.MessageUpdateBuilder {
			return view.NewReviewBuilder(s, layout.db).Build()
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
		var err error

		group, err = m.fetchNewTarget(ctx, s)
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
	}

	// Fetch latest group info from API
	groupInfo, err := m.layout.roAPI.Groups().GetGroupInfo(ctx.Context(), group.ID)
	if err != nil {
		m.layout.logger.Error("Failed to fetch group info",
			zap.Error(err),
			zap.Int64("groupID", group.ID))
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
	if m.CheckCaptchaRequired(ctx, s) {
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
				zap.Uint64("userID", userID),
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
	case constants.AddCommentButtonCustomID:
		m.HandleAddComment(ctx, s)
	case constants.DeleteCommentButtonCustomID:
		m.HandleDeleteComment(ctx, s, viewShared.TargetTypeGroup)
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
	if m.CheckCaptchaRequired(ctx, s) {
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
		m.handleMixGroup(ctx, s)
	}
}

// handleModal handles modal submissions for the review menu.
func (m *ReviewMenu) handleModal(ctx *interaction.Context, s *session.Session) {
	if m.CheckCaptchaRequired(ctx, s) {
		return
	}

	switch ctx.Event().CustomID() {
	case constants.AddReasonModalCustomID:
		group := session.GroupTarget.Get(s)
		shared.HandleReasonModalSubmit(
			ctx, s, group.Reasons, enum.GroupReasonTypeString,
			func(r types.Reasons[enum.GroupReasonType]) {
				group.Reasons = r
				session.GroupTarget.Set(s, group)
			},
			func(c float64) {
				group.Confidence = c
				session.GroupTarget.Set(s, group)
			},
		)
	case constants.AddCommentModalCustomID:
		m.HandleCommentModalSubmit(ctx, s, viewShared.TargetTypeGroup)
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

	// Get flagged members details with a limit of 15
	limit := 15

	var flaggedMembers map[int64]*types.ReviewUser

	if len(memberIDs) > 0 {
		// Only fetch up to the limit
		fetchIDs := memberIDs
		if len(fetchIDs) > limit {
			fetchIDs = fetchIDs[:limit]
		}

		var err error

		flaggedMembers, err = m.layout.db.Model().User().GetUsersByIDs(
			ctx.Context(),
			fetchIDs,
			types.UserFieldBasic|types.UserFieldReasons|types.UserFieldConfidence,
		)
		if err != nil {
			m.layout.logger.Error("Failed to get flagged members data", zap.Error(err))
		}
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

	// Format shout information (if recent)
	shoutInfo := "No shout available"

	if group.Shout != nil {
		// Only include shout if it's less than 30 days old
		if time.Since(group.Shout.Created) <= 30*24*time.Hour {
			shoutInfo = fmt.Sprintf("Posted by: %s\nContent: %s\nPosted at: %s",
				group.Shout.Poster.Username,
				group.Shout.Body,
				group.Shout.Created.Format(time.RFC3339))
		}
	}

	// Create group context
	groupContext := ai.Context{
		Type: ai.ContextTypeGroup,
		Content: fmt.Sprintf(`Group Information:

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
- Last Updated: %s

Recent Shout:
%s

Flagged Members (showing %d of %d total flagged):
%s`,
			group.Name,
			group.ID,
			group.Description,
			group.Owner.Username,
			group.Owner.UserID,
			groupInfo.MemberCount,
			strings.Join(group.Reasons.Messages(), "; "),
			group.Confidence,
			group.Status.String(),
			group.LastUpdated.Format(time.RFC3339),
			shoutInfo,
			len(flaggedMembers), len(memberIDs),
			strings.Join(membersInfo, "\n")),
	}

	// Append to existing chat context
	chatContext := session.ChatContext.Get(s)
	chatContext = append(chatContext, groupContext)
	session.ChatContext.Set(s, chatContext)

	// Navigate to chat
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
		// Log the skip action before clearing the group
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

		// Navigate to next group or fetch new one
		m.UpdateCounters(s)
		m.navigateAfterAction(ctx, s, "Skipped group.")

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
	m.layout.logger.Info("Fetching group", zap.Int64("groupID", targetGroupID))

	group, err := m.layout.db.Model().Group().GetGroupByID(
		ctx.Context(),
		strconv.FormatInt(targetGroupID, 10),
		types.GroupFieldAll,
	)
	if err != nil {
		if errors.Is(err, types.ErrGroupNotFound) {
			// Remove the missing group from history
			session.RemoveFromReviewHistory(s, session.GroupReviewHistoryType, index)

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

	// Store as current group and reload
	session.GroupTarget.Set(s, group)
	session.GroupFlaggedMembersCount.Set(s, flaggedCount)
	session.OriginalGroupReasons.Set(s, group.Reasons)
	session.UnsavedGroupReasons.Delete(s)
	session.ReasonsChanged.Delete(s)

	direction := map[bool]string{true: "next", false: "previous"}[isNext]
	ctx.Reload(fmt.Sprintf("Navigated to %s group.", direction))

	// Log the view action
	m.layout.db.Model().Activity().Log(ctx.Context(), &types.ActivityLog{
		ActivityTarget: types.ActivityTarget{
			GroupID: group.ID,
		},
		ReviewerID:        uint64(ctx.Event().User().ID),
		ActivityType:      enum.ActivityTypeGroupViewed,
		ActivityTimestamp: time.Now(),
		Details:           map[string]any{},
	})
}

// handleConfirmGroup moves a group to the confirmed state and logs the action.
func (m *ReviewMenu) handleConfirmGroup(ctx *interaction.Context, s *session.Session) {
	group := session.GroupTarget.Get(s)
	reviewerID := uint64(ctx.Event().User().ID)

	// Ensure user is an admin
	isAdmin := s.BotSettings().IsAdmin(reviewerID)
	if !isAdmin {
		m.layout.logger.Error("Non-admin attempted to confirm group",
			zap.Uint64("userID", reviewerID))
		ctx.Error("You do not have permission to confirm groups.")

		return
	}

	// Confirm the group
	if err := m.layout.db.Service().Group().ConfirmGroup(ctx.Context(), group, reviewerID); err != nil {
		m.layout.logger.Error("Failed to confirm group", zap.Error(err))
		ctx.Error("Failed to confirm the group. Please try again.")

		return
	}

	// Navigate to next group in history or fetch new one
	m.UpdateCounters(s)
	m.navigateAfterAction(ctx, s, "Group confirmed.")

	// Add the confirmed group to the D1 database
	if err := m.layout.d1Client.GroupFlags.AddConfirmed(ctx.Context(), group); err != nil {
		m.layout.logger.Error("Failed to add confirmed group to D1 database",
			zap.Error(err),
			zap.Int64("groupID", group.ID))
	}

	// Log the confirm action
	m.layout.db.Model().Activity().Log(ctx.Context(), &types.ActivityLog{
		ActivityTarget: types.ActivityTarget{
			GroupID: group.ID,
		},
		ReviewerID:        reviewerID,
		ActivityType:      enum.ActivityTypeGroupConfirmed,
		ActivityTimestamp: time.Now(),
		Details: map[string]any{
			"reasons": group.Reasons.Messages(),
		},
	})
}

// handleMixGroup marks a group as mixed and logs the action.
func (m *ReviewMenu) handleMixGroup(ctx *interaction.Context, s *session.Session) {
	group := session.GroupTarget.Get(s)
	reviewerID := uint64(ctx.Event().User().ID)

	// Ensure user is an admin
	isAdmin := s.BotSettings().IsAdmin(reviewerID)
	if !isAdmin {
		m.layout.logger.Error("Non-admin attempted to mark group as mixed",
			zap.Uint64("userID", reviewerID))
		ctx.Error("You do not have permission to mark groups as mixed.")

		return
	}

	// Mark the group as mixed
	if err := m.layout.db.Service().Group().MixGroup(ctx.Context(), group, reviewerID); err != nil {
		m.layout.logger.Error("Failed to mark group as mixed", zap.Error(err))
		ctx.Error("Failed to mark the group as mixed. Please try again.")

		return
	}

	// Navigate to next group in history or fetch new one
	m.UpdateCounters(s)
	m.navigateAfterAction(ctx, s, "Group marked as mixed.")

	// Add the mixed group to the D1 database
	if err := m.layout.d1Client.GroupFlags.AddMixed(ctx.Context(), group); err != nil {
		m.layout.logger.Error("Failed to add mixed group to D1 database",
			zap.Error(err),
			zap.Int64("groupID", group.ID))
	}

	// Log the mix action
	m.layout.db.Model().Activity().Log(ctx.Context(), &types.ActivityLog{
		ActivityTarget: types.ActivityTarget{
			GroupID: group.ID,
		},
		ReviewerID:        reviewerID,
		ActivityType:      enum.ActivityTypeGroupMixed,
		ActivityTimestamp: time.Now(),
		Details:           map[string]any{},
	})
}

// navigateAfterAction handles navigation after confirming or clearing a group.
// It tries to navigate to the next group in history, or fetches a new group if at the end.
func (m *ReviewMenu) navigateAfterAction(ctx *interaction.Context, s *session.Session, message string) {
	// Get the review history and current index
	history := session.GroupReviewHistory.Get(s)
	index := session.GroupReviewHistoryIndex.Get(s)

	// Clear current group data
	session.GroupTarget.Delete(s)
	session.UnsavedGroupReasons.Delete(s)

	// Check if there's a next group in history
	if index < len(history)-1 {
		// Navigate to next group in history
		index++
		session.GroupReviewHistoryIndex.Set(s, index)

		// Fetch the group data
		targetGroupID := history[index]

		group, err := m.layout.db.Model().Group().GetGroupByID(
			ctx.Context(),
			strconv.FormatInt(targetGroupID, 10),
			types.GroupFieldAll,
		)
		if err != nil {
			if errors.Is(err, types.ErrGroupNotFound) {
				// Remove the missing group from history and try again
				session.RemoveFromReviewHistory(s, session.GroupReviewHistoryType, index)

				// Try navigating again
				m.navigateAfterAction(ctx, s, message)

				return
			}

			m.layout.logger.Error("Failed to fetch next group from history", zap.Error(err))
			ctx.Error("Failed to load next group. Please try again.")

			return
		}

		// Get flagged users from tracking
		flaggedCount, err := m.layout.db.Model().Tracking().GetFlaggedUsersCount(ctx.Context(), group.ID)
		if err != nil {
			m.layout.logger.Error("Failed to fetch flagged users", zap.Error(err))
			ctx.Error("Failed to load flagged users. Please try again.")

			return
		}

		// Set as current group and reload
		session.GroupTarget.Set(s, group)
		session.GroupFlaggedMembersCount.Set(s, flaggedCount)
		session.OriginalGroupReasons.Set(s, group.Reasons)
		session.UnsavedGroupReasons.Delete(s)
		session.ReasonsChanged.Delete(s)

		ctx.Reload(message)

		// Log the view action
		m.layout.db.Model().Activity().Log(ctx.Context(), &types.ActivityLog{
			ActivityTarget: types.ActivityTarget{
				GroupID: group.ID,
			},
			ReviewerID:        uint64(ctx.Event().User().ID),
			ActivityType:      enum.ActivityTypeGroupViewed,
			ActivityTimestamp: time.Now(),
			Details:           map[string]any{},
		})

		return
	}

	// No next group in history, reload to fetch a new one
	ctx.Reload(message)
}

// handleReasonSelection processes reason management dropdown selections.
func (m *ReviewMenu) handleReasonSelection(ctx *interaction.Context, s *session.Session, option string) {
	// Check if user is a reviewer
	if !s.BotSettings().IsReviewer(uint64(ctx.Event().User().ID)) {
		m.layout.logger.Error("Non-reviewer attempted to manage reasons",
			zap.Uint64("userID", uint64(ctx.Event().User().ID)))
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
		session.ReasonsChanged.Delete(s)
		session.UnsavedGroupReasons.Delete(s)

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

	shared.HandleEditReason(
		ctx,
		s,
		m.layout.logger,
		reasonType,
		group.Reasons,
		func(r types.Reasons[enum.GroupReasonType]) {
			group.Reasons = r
			session.GroupTarget.Set(s, group)
		},
	)
}

// fetchNewTarget gets a new group to review based on the current sort order.
func (m *ReviewMenu) fetchNewTarget(ctx *interaction.Context, s *session.Session) (*types.ReviewGroup, error) {
	if m.CheckBreakRequired(ctx, s) {
		return nil, ErrBreakRequired
	}

	// Get the next group to review
	reviewerID := uint64(ctx.Event().User().ID)
	defaultSort := session.UserGroupDefaultSort.Get(s)
	reviewTargetMode := session.UserReviewTargetMode.Get(s)

	group, err := m.layout.db.Service().Group().GetGroupToReview(
		ctx.Context(), defaultSort, reviewTargetMode, reviewerID,
	)
	if err != nil {
		return nil, err
	}

	// Get flagged users from tracking
	flaggedCount, err := m.layout.db.Model().Tracking().GetFlaggedUsersCount(ctx.Context(), group.ID)
	if err != nil {
		return nil, err
	}

	// Store info in session
	session.GroupTarget.Set(s, group)
	session.GroupFlaggedMembersCount.Set(s, flaggedCount)
	session.OriginalGroupReasons.Set(s, group.Reasons)
	session.UnsavedGroupReasons.Delete(s)
	session.ReasonsChanged.Delete(s)

	// Add current group to history and set index to point to it
	session.AddToReviewHistory(s, session.GroupReviewHistoryType, group.ID)

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

	return group, nil
}
