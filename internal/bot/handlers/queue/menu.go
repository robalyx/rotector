package queue

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/google/uuid"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/interaction"
	"github.com/robalyx/rotector/internal/bot/core/session"
	view "github.com/robalyx/rotector/internal/bot/views/queue"
	"github.com/robalyx/rotector/internal/database/types"
	"github.com/robalyx/rotector/internal/database/types/enum"
	"github.com/robalyx/rotector/internal/queue"
	"github.com/robalyx/rotector/pkg/utils"
	"go.uber.org/zap"
)

var (
	// ErrEmptyLine indicates an empty line was provided.
	ErrEmptyLine = errors.New("empty line")
	// ErrInvalidUserID indicates an invalid user ID was provided.
	ErrInvalidUserID = errors.New("invalid user ID")
)

// Menu handles queue operations and their interactions.
type Menu struct {
	layout *Layout
	page   *interaction.Page
}

// NewMenu creates a new queue menu.
func NewMenu(layout *Layout) *Menu {
	m := &Menu{layout: layout}
	m.page = &interaction.Page{
		Name: constants.QueuePageName,
		Message: func(s *session.Session) *discord.MessageUpdateBuilder {
			return view.NewBuilder(s).Build()
		},
		ShowHandlerFunc:   m.Show,
		ButtonHandlerFunc: m.handleButton,
		ModalHandlerFunc:  m.handleModal,
	}
	return m
}

// Show prepares and displays the queue interface.
func (m *Menu) Show(ctx *interaction.Context, s *session.Session) {
	// Get queue stats
	stats, err := m.layout.d1Client.GetQueueStats(ctx.Context())
	if err != nil {
		m.layout.logger.Error("Failed to get queue stats", zap.Error(err))
		ctx.Error("Failed to get queue statistics. Please try again.")
		return
	}

	// Store stats in session
	session.QueueStats.Set(s, stats)

	// Get queued user status if one exists
	if userID := session.QueuedUserID.Get(s); userID > 0 {
		status, err := m.layout.d1Client.GetQueueStatus(ctx.Context(), userID)
		if err != nil {
			m.layout.logger.Error("Failed to get queue status", zap.Error(err))
			ctx.Error("Failed to get queue status. Please try again.")
			return
		}

		// Store status in session
		session.QueuedUserProcessing.Set(s, status.Processing)
		session.QueuedUserProcessed.Set(s, status.Processed)
		session.QueuedUserFlagged.Set(s, status.Flagged)
	}
}

// handleQueueUser opens a modal for entering a Roblox user ID to queue.
func (m *Menu) handleQueueUser(ctx *interaction.Context) {
	modal := discord.NewModalCreateBuilder().
		SetCustomID(constants.QueueUserModalCustomID).
		SetTitle("Queue Roblox Users").
		AddActionRow(
			discord.NewTextInput(constants.QueueUserInputCustomID, discord.TextInputStyleParagraph, "User IDs").
				WithRequired(true).
				WithPlaceholder("Enter Roblox user IDs to queue (one per line)").
				WithMaxLength(512),
		)

	ctx.Modal(modal)
}

// handleManualUserReview opens a modal for entering a Roblox user ID to manually review.
func (m *Menu) handleManualUserReview(ctx *interaction.Context) {
	modal := discord.NewModalCreateBuilder().
		SetCustomID(constants.ManualUserReviewModalCustomID).
		SetTitle("Manual User Review").
		AddActionRow(
			discord.NewTextInput(constants.ManualUserReviewInputCustomID, discord.TextInputStyleShort, "User ID").
				WithRequired(true).
				WithPlaceholder("Enter the Roblox user ID to review..."),
		)

	ctx.Modal(modal)
}

// handleManualGroupReview opens a modal for entering a Roblox group ID to manually review.
func (m *Menu) handleManualGroupReview(ctx *interaction.Context) {
	modal := discord.NewModalCreateBuilder().
		SetCustomID(constants.ManualGroupReviewModalCustomID).
		SetTitle("Manual Group Review").
		AddActionRow(
			discord.NewTextInput(constants.ManualGroupReviewInputCustomID, discord.TextInputStyleShort, "Group ID").
				WithRequired(true).
				WithPlaceholder("Enter the Roblox group ID to review..."),
		)

	ctx.Modal(modal)
}

// handleModal processes modal submissions.
func (m *Menu) handleModal(ctx *interaction.Context, s *session.Session) {
	switch ctx.Event().CustomID() {
	case constants.QueueUserModalCustomID:
		m.handleQueueUserModalSubmit(ctx, s)
	case constants.ManualUserReviewModalCustomID:
		m.handleManualUserReviewModalSubmit(ctx, s)
	case constants.ManualGroupReviewModalCustomID:
		m.handleManualGroupReviewModalSubmit(ctx, s)
	}
}

// handleQueueUserModalSubmit processes the user IDs input and queues the users.
func (m *Menu) handleQueueUserModalSubmit(ctx *interaction.Context, s *session.Session) {
	// Get the user IDs input
	input := ctx.Event().ModalData().Text(constants.QueueUserInputCustomID)
	lines := strings.Split(strings.TrimSpace(input), "\n")

	if len(lines) == 0 {
		ctx.Cancel("Please provide at least one user ID.")
		return
	}

	// Process and validate all lines first
	userIDs := make([]uint64, 0, len(lines))
	invalidInputs := make([]string, 0)
	processedLines := make(map[string]struct{})

	for _, line := range lines {
		// Skip duplicate lines
		if _, exists := processedLines[line]; exists {
			continue
		}
		processedLines[line] = struct{}{}

		userID, err := m.processUserIDInput(line)
		if err != nil {
			invalidInputs = append(invalidInputs, line)
			continue
		}

		userIDs = append(userIDs, userID)
	}

	if len(userIDs) == 0 {
		if len(invalidInputs) > 0 {
			ctx.Cancel("Invalid user IDs or URLs: " + strings.Join(invalidInputs, ", "))
		} else {
			ctx.Cancel("No valid user IDs provided.")
		}
		return
	}

	// Queue users in batches if needed
	var totalQueuedCount int
	var lastUserID uint64
	var allFailedIDs []string

	for i := 0; i < len(userIDs); i += queue.MaxQueueBatchSize {
		end := min(i+queue.MaxQueueBatchSize, len(userIDs))
		batch := userIDs[i:end]

		queuedCount, batchLastUserID, failedIDs, activityLogs := m.queueUserBatch(ctx, batch)
		totalQueuedCount += queuedCount
		if queuedCount > 0 {
			lastUserID = batchLastUserID
		}
		allFailedIDs = append(allFailedIDs, failedIDs...)

		// Log activities in batch
		if len(activityLogs) > 0 {
			m.layout.db.Model().Activity().LogBatch(ctx.Context(), activityLogs)
		}
	}

	// Store queued user ID in session only if exactly one user was queued
	if totalQueuedCount == 1 {
		session.QueuedUserID.Set(s, lastUserID)
		session.QueuedUserTimestamp.Set(s, time.Now())
	} else {
		// Clear any existing queued user tracking
		session.QueuedUserID.Delete(s)
		session.QueuedUserTimestamp.Delete(s)
	}

	// Get updated queue stats
	stats, err := m.layout.d1Client.GetQueueStats(ctx.Context())
	if err != nil {
		m.layout.logger.Error("Failed to get queue stats", zap.Error(err))
	} else {
		session.QueueStats.Set(s, stats)
	}

	// Build response message
	var msg strings.Builder
	if totalQueuedCount > 0 {
		msg.WriteString(fmt.Sprintf("Successfully queued %d user(s) for processing. ", totalQueuedCount))
	}
	if len(invalidInputs) > 0 {
		msg.WriteString(fmt.Sprintf("Invalid entries: %s ", strings.Join(invalidInputs, ", ")))
	}
	if len(allFailedIDs) > 0 {
		msg.WriteString(fmt.Sprintf("Failed to queue: %s ", strings.Join(allFailedIDs, ", ")))
	}

	ctx.Reload(msg.String())
}

// handleManualUserReviewModalSubmit processes the user ID input and adds the user to the review system.
func (m *Menu) handleManualUserReviewModalSubmit(ctx *interaction.Context, s *session.Session) {
	// Get the user ID input
	userIDStr := ctx.Event().ModalData().Text(constants.ManualUserReviewInputCustomID)

	// Parse profile URL if provided
	parsedURL, err := utils.ExtractUserIDFromURL(userIDStr)
	if err == nil {
		userIDStr = parsedURL
	}

	// Parse user ID
	userID, err := strconv.ParseUint(userIDStr, 10, 64)
	if err != nil {
		ctx.Cancel("Please provide a valid user ID.")
		return
	}

	// Check if user exists in database first
	user, err := m.layout.db.Service().User().GetUserByID(ctx.Context(), userIDStr, types.UserFieldAll)
	if err == nil {
		// Store user in session and show review menu
		session.AddToReviewHistory(s, session.UserReviewHistoryType, user.ID)

		session.UserTarget.Set(s, user)
		session.OriginalUserReasons.Set(s, user.Reasons)
		session.ReasonsChanged.Set(s, false)
		ctx.Show(constants.UserReviewPageName, "")
		return
	}

	// Fetch user info from Roblox API
	users := m.layout.userFetcher.FetchInfos(ctx.Context(), []uint64{userID})
	if len(users) == 0 {
		ctx.Cancel("Failed to fetch user information. The user may be banned or not exist.")
		return
	}

	// Use the fetched user information
	user = users[0]
	user.Status = enum.UserTypeFlagged
	user.UUID = uuid.New()
	user.Reasons = make(types.Reasons[enum.UserReasonType])

	// Save the new user to the database
	if err := m.layout.db.Service().User().SaveUsers(ctx.Context(), map[uint64]*types.ReviewUser{
		user.ID: user,
	}); err != nil {
		m.layout.logger.Error("Failed to save new user", zap.Error(err))
		ctx.Error("Failed to save user information. Please try again.")
		return
	}

	// Store user in session and show review menu
	session.AddToReviewHistory(s, session.UserReviewHistoryType, user.ID)

	session.UserTarget.Set(s, user)
	session.OriginalUserReasons.Set(s, user.Reasons)
	session.ReasonsChanged.Set(s, false)
	ctx.Show(constants.UserReviewPageName, "")

	// Log the manual review queue action
	m.layout.db.Model().Activity().Log(ctx.Context(), &types.ActivityLog{
		ActivityTarget: types.ActivityTarget{
			UserID: userID,
		},
		ReviewerID:        uint64(ctx.Event().User().ID),
		ActivityType:      enum.ActivityTypeUserQueued,
		ActivityTimestamp: time.Now(),
		Details:           map[string]any{},
	})
}

// handleManualGroupReviewModalSubmit processes the group ID input and adds the group to the review system.
func (m *Menu) handleManualGroupReviewModalSubmit(ctx *interaction.Context, s *session.Session) {
	// Get the group ID input
	groupIDStr := ctx.Event().ModalData().Text(constants.ManualGroupReviewInputCustomID)

	// Parse profile URL if provided
	parsedURL, err := utils.ExtractGroupIDFromURL(groupIDStr)
	if err == nil {
		groupIDStr = parsedURL
	}

	// Parse group ID
	groupID, err := strconv.ParseUint(groupIDStr, 10, 64)
	if err != nil {
		ctx.Cancel("Please provide a valid group ID.")
		return
	}

	// Check if group exists in database first
	group, err := m.layout.db.Service().Group().GetGroupByID(ctx.Context(), groupIDStr, types.GroupFieldAll)
	if err == nil {
		// Store group in session and show review menu
		session.AddToReviewHistory(s, session.GroupReviewHistoryType, group.ID)

		session.GroupTarget.Set(s, group)
		session.OriginalGroupReasons.Set(s, group.Reasons)
		session.ReasonsChanged.Set(s, false)
		ctx.Show(constants.GroupReviewPageName, "")
		return
	}

	// Fetch group info from Roblox API
	groups := m.layout.groupFetcher.FetchGroupInfos(ctx.Context(), []uint64{groupID})
	if len(groups) == 0 {
		ctx.Cancel("Failed to fetch group information. The group may be locked or not exist.")
		return
	}

	// Use the fetched group information
	groupInfo := groups[0]
	group = &types.ReviewGroup{
		Group: &types.Group{
			ID:          groupID,
			UUID:        uuid.New(),
			Name:        groupInfo.Name,
			Description: groupInfo.Description,
			Owner:       groupInfo.Owner,
			Shout:       groupInfo.Shout,
			Status:      enum.GroupTypeFlagged,
			LastScanned: time.Unix(0, 0),
			LastUpdated: time.Now(),
			LastViewed:  time.Unix(0, 0),
		},
		Reasons: make(types.Reasons[enum.GroupReasonType]),
	}

	// Add thumbnail URL to the group
	groupWithThumbnail := m.layout.thumbnailFetcher.AddGroupImageURLs(ctx.Context(), map[uint64]*types.ReviewGroup{
		group.ID: group,
	})
	if len(groupWithThumbnail) > 0 {
		group = groupWithThumbnail[group.ID]
	}

	// Save the new group to the database
	if err := m.layout.db.Service().Group().SaveGroups(ctx.Context(), map[uint64]*types.ReviewGroup{
		group.ID: group,
	}); err != nil {
		m.layout.logger.Error("Failed to save new group", zap.Error(err))
		ctx.Error("Failed to save group information. Please try again.")
		return
	}

	// Get flagged users count from tracking
	flaggedCount, err := m.layout.db.Model().Tracking().GetFlaggedUsersCount(ctx.Context(), groupID)
	if err != nil {
		m.layout.logger.Error("Failed to fetch flagged users count", zap.Error(err))
		ctx.Error("Failed to load flagged users count. Please try again.")
		return
	}

	// Store group and related info in session
	session.AddToReviewHistory(s, session.GroupReviewHistoryType, group.ID)

	session.GroupTarget.Set(s, group)
	session.GroupFlaggedMembersCount.Set(s, flaggedCount)
	session.OriginalGroupReasons.Set(s, group.Reasons)
	session.ReasonsChanged.Set(s, false)

	ctx.Show(constants.GroupReviewPageName, "")

	// Log the manual review action
	m.layout.db.Model().Activity().Log(ctx.Context(), &types.ActivityLog{
		ActivityTarget: types.ActivityTarget{
			GroupID: groupID,
		},
		ReviewerID:        uint64(ctx.Event().User().ID),
		ActivityType:      enum.ActivityTypeGroupQueued,
		ActivityTimestamp: time.Now(),
		Details:           map[string]any{},
	})
}

// handleButton processes button interactions.
func (m *Menu) handleButton(ctx *interaction.Context, s *session.Session, customID string) {
	switch customID {
	case constants.RefreshButtonCustomID:
		m.handleRefresh(ctx, s)
	case constants.AbortButtonCustomID:
		m.handleAbort(ctx, s)
	case constants.QueueUserButtonCustomID:
		m.handleQueueUser(ctx)
	case constants.ManualUserReviewButtonCustomID:
		m.handleManualUserReview(ctx)
	case constants.ManualGroupReviewButtonCustomID:
		if !s.BotSettings().IsAdmin(uint64(ctx.Event().User().ID)) {
			m.layout.logger.Error("Non-admin attempted restricted action",
				zap.Uint64("user_id", uint64(ctx.Event().User().ID)),
				zap.String("action", customID))
			ctx.Error("You do not have permission to perform this action.")
			return
		}
		m.handleManualGroupReview(ctx)
	case constants.ReviewQueuedUserButtonCustomID:
		m.handleReviewQueuedUser(ctx, s)
	case constants.BackButtonCustomID:
		ctx.NavigateBack("")
	}
}

// handleRefresh updates the queue statistics.
func (m *Menu) handleRefresh(ctx *interaction.Context, s *session.Session) {
	// Get updated queue stats
	stats, err := m.layout.d1Client.GetQueueStats(ctx.Context())
	if err != nil {
		m.layout.logger.Error("Failed to get queue stats", zap.Error(err))
		ctx.Error("Failed to get queue statistics. Please try again.")
		return
	}
	session.QueueStats.Set(s, stats)
	ctx.Reload("Refreshed queue statistics.")
}

// handleAbort removes a user from the processing queue.
func (m *Menu) handleAbort(ctx *interaction.Context, s *session.Session) {
	// Get queued user ID from session
	userID := session.QueuedUserID.Get(s)
	if userID == 0 {
		ctx.Cancel("No user is currently queued.")
		return
	}

	// Remove user from queue
	if err := m.layout.d1Client.RemoveFromQueue(ctx.Context(), userID); err != nil {
		switch {
		case errors.Is(err, queue.ErrUserNotFound):
			// Clear session data since user is no longer in queue
			session.QueuedUserID.Delete(s)
			session.QueuedUserTimestamp.Delete(s)
			ctx.Cancel("User is no longer in queue.")
			return
		case errors.Is(err, queue.ErrUserProcessing):
			// Clear session data since we can't abort anymore
			session.QueuedUserID.Delete(s)
			session.QueuedUserTimestamp.Delete(s)
			ctx.Cancel("Cannot abort - user is already being processed or has been processed.")
			return
		default:
			m.layout.logger.Error("Failed to remove user from queue", zap.Error(err))
			ctx.Error("Failed to remove user from queue. Please try again.")
			return
		}
	}

	// Clear queued user from session
	session.QueuedUserID.Delete(s)
	session.QueuedUserTimestamp.Delete(s)

	// Get updated queue stats
	stats, err := m.layout.d1Client.GetQueueStats(ctx.Context())
	if err != nil {
		m.layout.logger.Error("Failed to get queue stats", zap.Error(err))
	} else {
		session.QueueStats.Set(s, stats)
	}

	ctx.Reload(fmt.Sprintf("Successfully removed user %d from queue.", userID))
}

// handleReviewQueuedUser handles reviewing a queued user that has been processed.
func (m *Menu) handleReviewQueuedUser(ctx *interaction.Context, s *session.Session) {
	// Get queued user ID from session
	userID := session.QueuedUserID.Get(s)
	if userID == 0 {
		ctx.Cancel("No user is currently queued.")
		return
	}

	// Check if user exists in database
	user, err := m.layout.db.Service().User().GetUserByID(ctx.Context(), strconv.FormatUint(userID, 10), types.UserFieldAll)
	if err != nil {
		m.layout.logger.Error("Failed to get user", zap.Error(err))
		ctx.Error("Failed to get user information. Please try again.")
		return
	}

	// Store user in session and show review menu
	session.AddToReviewHistory(s, session.UserReviewHistoryType, user.ID)

	session.UserTarget.Set(s, user)
	session.OriginalUserReasons.Set(s, user.Reasons)
	session.ReasonsChanged.Set(s, false)
	ctx.Show(constants.UserReviewPageName, "")
}

// processUserIDInput processes a single line of input and returns the parsed user ID if valid.
func (m *Menu) processUserIDInput(line string) (uint64, error) {
	line = strings.TrimSpace(line)
	if line == "" {
		return 0, ErrEmptyLine
	}

	// Parse profile URL if provided
	userIDStr := line
	parsedURL, err := utils.ExtractUserIDFromURL(line)
	if err == nil {
		userIDStr = parsedURL
	}

	// Parse user ID
	userID, err := strconv.ParseUint(userIDStr, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%w: %s", ErrInvalidUserID, line)
	}

	return userID, nil
}

// queueUserBatch queues a batch of users and returns the results.
func (m *Menu) queueUserBatch(ctx *interaction.Context, batch []uint64) (int, uint64, []string, []*types.ActivityLog) {
	queueErrors, err := m.layout.d1Client.QueueUsers(ctx.Context(), batch)
	if err != nil {
		m.layout.logger.Error("Failed to queue batch", zap.Error(err))
		failedIDs := make([]string, len(batch))
		for i, userID := range batch {
			failedIDs[i] = fmt.Sprintf("%d (error)", userID)
		}
		return 0, 0, failedIDs, nil
	}

	// Process results and create activity logs
	var queuedCount int
	var lastUserID uint64
	var failedIDs []string
	activityLogs := make([]*types.ActivityLog, 0, len(batch))
	now := time.Now()

	for _, userID := range batch {
		if queueErr, failed := queueErrors[userID]; failed {
			if errors.Is(queueErr, queue.ErrUserRecentlyQueued) {
				failedIDs = append(failedIDs, fmt.Sprintf("%d (recently queued)", userID))
			} else {
				failedIDs = append(failedIDs, fmt.Sprintf("%d (error)", userID))
			}
			continue
		}
		lastUserID = userID
		queuedCount++

		// Create activity log for this user
		activityLogs = append(activityLogs, &types.ActivityLog{
			ActivityTarget: types.ActivityTarget{
				UserID: userID,
			},
			ReviewerID:        uint64(ctx.Event().User().ID),
			ActivityType:      enum.ActivityTypeUserQueued,
			ActivityTimestamp: now,
			Details: map[string]any{
				"batch_size": len(batch),
			},
		})
	}

	return queuedCount, lastUserID, failedIDs, activityLogs
}
