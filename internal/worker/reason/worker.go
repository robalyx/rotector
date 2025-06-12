package reason

import (
	"context"
	"fmt"
	"time"

	"github.com/robalyx/rotector/internal/ai"
	"github.com/robalyx/rotector/internal/database"
	"github.com/robalyx/rotector/internal/database/types"
	"github.com/robalyx/rotector/internal/database/types/enum"
	"github.com/robalyx/rotector/internal/progress"
	"github.com/robalyx/rotector/internal/setup"
	"github.com/robalyx/rotector/pkg/utils"
	"go.uber.org/zap"
)

// Placeholder message that needs to be updated.
const (
	FriendPlaceholder = "User has flagged friends in their friend network."
	GroupPlaceholder  = "Member of multiple inappropriate groups."
	OutfitPlaceholder = "User has outfits with inappropriate themes."
)

// Processor defines the structure for processing different types of reasons.
type Processor struct {
	reasonType  enum.UserReasonType
	placeholder string
	stepMessage string
	processFunc func(ctx context.Context, users []*types.ReviewUser) (map[uint64]string, error)
}

// Worker updates existing user reasons with detailed AI-generated analysis.
type Worker struct {
	db                   database.Client
	bar                  *progress.Bar
	friendReasonAnalyzer *ai.FriendReasonAnalyzer
	groupReasonAnalyzer  *ai.GroupReasonAnalyzer
	outfitReasonAnalyzer *ai.OutfitReasonAnalyzer
	logger               *zap.Logger
	batchSize            int
	batchDelay           time.Duration
	processors           []Processor
}

// New creates a new reason worker.
func New(app *setup.App, bar *progress.Bar, logger *zap.Logger) *Worker {
	w := &Worker{
		db:                   app.DB,
		bar:                  bar,
		friendReasonAnalyzer: ai.NewFriendReasonAnalyzer(app, logger),
		groupReasonAnalyzer:  ai.NewGroupReasonAnalyzer(app, logger),
		outfitReasonAnalyzer: ai.NewOutfitReasonAnalyzer(app, logger),
		logger:               logger.Named("reason_worker"),
		batchSize:            100,
		batchDelay:           3 * time.Second,
	}

	// Initialize processors
	w.processors = []Processor{
		{
			reasonType:  enum.UserReasonTypeFriend,
			placeholder: FriendPlaceholder,
			stepMessage: "Updating friend reasons",
			processFunc: func(ctx context.Context, users []*types.ReviewUser) (map[uint64]string, error) {
				confirmedFriendsMap, flaggedFriendsMap := w.prepareFriendMaps(ctx, users)
				reasons := w.friendReasonAnalyzer.GenerateFriendReasons(ctx, users, confirmedFriendsMap, flaggedFriendsMap)
				return reasons, nil
			},
		},
		{
			reasonType:  enum.UserReasonTypeGroup,
			placeholder: GroupPlaceholder,
			stepMessage: "Updating group reasons",
			processFunc: func(ctx context.Context, users []*types.ReviewUser) (map[uint64]string, error) {
				confirmedGroupsMap, flaggedGroupsMap := w.prepareGroupMaps(ctx, users)
				reasons := w.groupReasonAnalyzer.GenerateGroupReasons(ctx, users, confirmedGroupsMap, flaggedGroupsMap)
				return reasons, nil
			},
		},
		{
			reasonType:  enum.UserReasonTypeOutfit,
			placeholder: OutfitPlaceholder,
			stepMessage: "Updating outfit reasons",
			processFunc: func(ctx context.Context, users []*types.ReviewUser) (map[uint64]string, error) {
				reasons := w.outfitReasonAnalyzer.GenerateOutfitReasons(ctx, users)
				return reasons, nil
			},
		},
	}

	return w
}

// Start begins the reason worker's operation.
func (w *Worker) Start(ctx context.Context) {
	w.logger.Info("Reason Worker started")

	w.bar.SetTotal(100)
	w.bar.SetStepMessage("Starting reason update process", 0)

	totalCounts := make([]int, len(w.processors))
	stepSize := 100 / len(w.processors)

	for i, processor := range w.processors {
		// Check if context was cancelled
		if utils.ContextGuardWithLog(ctx, w.logger, "Context cancelled, stopping reason worker") {
			w.bar.SetStepMessage("Cancelled", 100)
			return
		}

		progress := int64((i + 1) * stepSize)
		w.bar.SetStepMessage(processor.stepMessage, progress)

		count, err := w.updateReasons(ctx, processor)
		if err != nil {
			w.logger.Error("Failed to update reasons",
				zap.String("type", processor.reasonType.String()),
				zap.Error(err))
		} else {
			w.logger.Info("Updated reasons",
				zap.String("type", processor.reasonType.String()),
				zap.Int("count", count))
		}
		totalCounts[i] = count
	}

	w.bar.SetStepMessage("Completed", 100)
	w.logger.Info("Reason Worker completed",
		zap.Int("friendUpdates", totalCounts[0]),
		zap.Int("groupUpdates", totalCounts[1]),
		zap.Int("outfitUpdates", totalCounts[2]))

	// Wait for shutdown signal instead of blocking indefinitely
	w.logger.Info("Reason worker finished processing, waiting for shutdown signal")
	w.bar.SetStepMessage("Waiting for shutdown", 100)
	<-ctx.Done()
	w.logger.Info("Reason worker shutting down gracefully")
}

// updateReasons updates users with the specified placeholder messages using the provided processor.
func (w *Worker) updateReasons(ctx context.Context, processor Processor) (int, error) {
	users, err := w.db.Model().User().GetUsersWithReasonMessage(ctx, processor.reasonType, processor.placeholder)
	if err != nil {
		return 0, fmt.Errorf("failed to get users with %s placeholder: %w", processor.reasonType.String(), err)
	}

	if len(users) == 0 {
		w.logger.Info("No users found with placeholder message",
			zap.String("type", processor.reasonType.String()))
		return 0, nil
	}

	w.logger.Info("Found users with placeholder",
		zap.String("type", processor.reasonType.String()),
		zap.Int("count", len(users)))

	return w.processReasons(ctx, users, processor)
}

// processReasons processes reason updates in batches using the provided processor.
func (w *Worker) processReasons(ctx context.Context, users []*types.ReviewUser, processor Processor) (int, error) {
	totalUpdated := 0

	for i := 0; i < len(users); i += w.batchSize {
		end := min(i+w.batchSize, len(users))
		batch := users[i:end]

		// Get users with relationships
		usersWithRelationships, err := w.getUsersWithRelationships(ctx, batch, processor.reasonType.String())
		if err != nil {
			w.logger.Error("Failed to get users with relationships for batch",
				zap.String("type", processor.reasonType.String()),
				zap.Error(err))
			continue
		}

		// Generate new reasons using the processor's function
		reasons, err := processor.processFunc(ctx, usersWithRelationships)
		if err != nil {
			w.logger.Error("Failed to process reasons",
				zap.String("type", processor.reasonType.String()),
				zap.Error(err))
			continue
		}

		// Update database
		updated := w.updateUserReasons(ctx, reasons, processor.reasonType)

		totalUpdated += updated
		w.logger.Info("Updated reasons batch",
			zap.String("type", processor.reasonType.String()),
			zap.Int("batchSize", len(batch)),
			zap.Int("updated", updated),
			zap.Int("totalUpdated", totalUpdated))

		// Add delay between batches
		if i+w.batchSize < len(users) {
			utils.ContextSleep(ctx, w.batchDelay)
		}
	}

	return totalUpdated, nil
}

// getUsersWithRelationships gets users with relationships for a batch and converts to slice.
func (w *Worker) getUsersWithRelationships(ctx context.Context, batch []*types.ReviewUser, batchType string) ([]*types.ReviewUser, error) {
	// Get user IDs for the batch
	userIDs := make([]uint64, len(batch))
	for i, user := range batch {
		userIDs[i] = user.ID
	}

	// Get users with relationships
	usersMap, err := w.db.Service().User().GetUsersByIDs(ctx, userIDs, types.UserFieldAll)
	if err != nil {
		return nil, fmt.Errorf("failed to get users with relationships for %s batch: %w", batchType, err)
	}

	// Convert map to slice maintaining order
	usersWithRelationships := make([]*types.ReviewUser, 0, len(batch))
	for _, userID := range userIDs {
		if user, exists := usersMap[userID]; exists {
			usersWithRelationships = append(usersWithRelationships, user)
		}
	}

	return usersWithRelationships, nil
}

// prepareFriendMaps prepares confirmed and flagged friend maps for analysis.
func (w *Worker) prepareFriendMaps(
	ctx context.Context, users []*types.ReviewUser,
) (map[uint64]map[uint64]*types.ReviewUser, map[uint64]map[uint64]*types.ReviewUser) {
	confirmedFriendsMap := make(map[uint64]map[uint64]*types.ReviewUser)
	flaggedFriendsMap := make(map[uint64]map[uint64]*types.ReviewUser)

	// Collect all unique friend IDs
	uniqueFriendIDs := make(map[uint64]struct{})
	for _, user := range users {
		for _, friend := range user.Friends {
			uniqueFriendIDs[friend.ID] = struct{}{}
		}
	}

	// Convert to slice
	friendIDs := make([]uint64, 0, len(uniqueFriendIDs))
	for friendID := range uniqueFriendIDs {
		friendIDs = append(friendIDs, friendID)
	}

	// Get existing friends
	existingFriends, err := w.db.Model().User().GetUsersByIDs(
		ctx, friendIDs, types.UserFieldBasic|types.UserFieldReasons,
	)
	if err != nil {
		w.logger.Error("Failed to fetch existing friends", zap.Error(err))
		return confirmedFriendsMap, flaggedFriendsMap
	}

	// Build maps
	for _, user := range users {
		confirmedFriends := make(map[uint64]*types.ReviewUser)
		flaggedFriends := make(map[uint64]*types.ReviewUser)

		for _, friend := range user.Friends {
			if reviewUser, exists := existingFriends[friend.ID]; exists {
				switch reviewUser.Status {
				case enum.UserTypeConfirmed:
					confirmedFriends[friend.ID] = reviewUser
				case enum.UserTypeFlagged:
					flaggedFriends[friend.ID] = reviewUser
				case enum.UserTypeCleared:
				}
			}
		}

		confirmedFriendsMap[user.ID] = confirmedFriends
		flaggedFriendsMap[user.ID] = flaggedFriends
	}

	return confirmedFriendsMap, flaggedFriendsMap
}

// prepareGroupMaps prepares confirmed and flagged group maps for analysis.
func (w *Worker) prepareGroupMaps(
	ctx context.Context, users []*types.ReviewUser,
) (map[uint64]map[uint64]*types.ReviewGroup, map[uint64]map[uint64]*types.ReviewGroup) {
	confirmedGroupsMap := make(map[uint64]map[uint64]*types.ReviewGroup)
	flaggedGroupsMap := make(map[uint64]map[uint64]*types.ReviewGroup)

	// Collect all unique group IDs
	uniqueGroupIDs := make(map[uint64]struct{})
	for _, user := range users {
		for _, group := range user.Groups {
			uniqueGroupIDs[group.Group.ID] = struct{}{}
		}
	}

	// Convert to slice
	groupIDs := make([]uint64, 0, len(uniqueGroupIDs))
	for groupID := range uniqueGroupIDs {
		groupIDs = append(groupIDs, groupID)
	}

	// Get existing groups
	existingGroups, err := w.db.Model().Group().GetGroupsByIDs(
		ctx, groupIDs, types.GroupFieldBasic|types.GroupFieldReasons,
	)
	if err != nil {
		w.logger.Error("Failed to fetch existing groups", zap.Error(err))
		return confirmedGroupsMap, flaggedGroupsMap
	}

	// Build maps
	for _, user := range users {
		confirmedGroups := make(map[uint64]*types.ReviewGroup)
		flaggedGroups := make(map[uint64]*types.ReviewGroup)

		for _, group := range user.Groups {
			if reviewGroup, exists := existingGroups[group.Group.ID]; exists {
				switch reviewGroup.Status {
				case enum.GroupTypeConfirmed:
					confirmedGroups[group.Group.ID] = reviewGroup
				case enum.GroupTypeFlagged:
					flaggedGroups[group.Group.ID] = reviewGroup
				case enum.GroupTypeCleared:
				}
			}
		}

		confirmedGroupsMap[user.ID] = confirmedGroups
		flaggedGroupsMap[user.ID] = flaggedGroups
	}

	return confirmedGroupsMap, flaggedGroupsMap
}

// updateUserReasons updates user reasons in the database.
func (w *Worker) updateUserReasons(ctx context.Context, reasons map[uint64]string, reasonType enum.UserReasonType) int {
	if len(reasons) == 0 {
		return 0
	}

	updated := 0

	for userID, newReason := range reasons {
		if newReason == "" {
			continue
		}

		err := w.db.Model().User().UpdateUserReason(ctx, userID, reasonType, newReason)
		if err != nil {
			w.logger.Error("Failed to update user reason",
				zap.Uint64("userID", userID),
				zap.String("reasonType", reasonType.String()),
				zap.Error(err))
			continue
		}
		updated++
	}

	return updated
}
