package reason

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/robalyx/rotector/internal/database"
	"github.com/robalyx/rotector/internal/database/types"
	"github.com/robalyx/rotector/internal/database/types/enum"
	"github.com/robalyx/rotector/internal/progress"
	"github.com/robalyx/rotector/internal/roblox/checker"
	"github.com/robalyx/rotector/internal/setup"
	"github.com/robalyx/rotector/pkg/utils"
	"go.uber.org/zap"
)

var ErrUnsupportedReason = errors.New("unsupported reason type")

// Worker checks all users from the database and runs them through friend and group
// checkers for users that don't have the respective reasons.
type Worker struct {
	db            database.Client
	bar           *progress.Bar
	friendChecker *checker.FriendChecker
	groupChecker  *checker.GroupChecker
	logger        *zap.Logger
	batchSize     int
	batchDelay    time.Duration
}

// New creates a new reason worker.
func New(app *setup.App, bar *progress.Bar, logger *zap.Logger) *Worker {
	return &Worker{
		db:            app.DB,
		bar:           bar,
		friendChecker: checker.NewFriendChecker(app, logger),
		groupChecker:  checker.NewGroupChecker(app, logger),
		logger:        logger.Named("reason_worker"),
		batchSize:     200,
		batchDelay:    1 * time.Second,
	}
}

// Start begins the reason worker's operation.
func (w *Worker) Start(ctx context.Context) {
	w.logger.Info("Reason Worker started")

	w.bar.SetTotal(100)
	w.bar.SetStepMessage("Starting reason check process", 0)

	// Process friend reasons
	w.bar.SetStepMessage("Processing users missing friend reasons", 25)

	friendCount, err := w.processUsersWithoutReason(ctx, enum.UserReasonTypeFriend)
	if err != nil {
		w.logger.Error("Failed to process users without friend reasons", zap.Error(err))
	} else {
		w.logger.Info("Processed users without friend reasons", zap.Int("count", friendCount))
	}

	// Check if context was cancelled
	if utils.ContextGuardWithLog(ctx, w.logger, "Context cancelled, stopping reason worker") {
		w.bar.SetStepMessage("Cancelled", 100)
		return
	}

	// Process group reasons
	w.bar.SetStepMessage("Processing users missing group reasons", 75)

	groupCount, err := w.processUsersWithoutReason(ctx, enum.UserReasonTypeGroup)
	if err != nil {
		w.logger.Error("Failed to process users without group reasons", zap.Error(err))
	} else {
		w.logger.Info("Processed users without group reasons", zap.Int("count", groupCount))
	}

	w.bar.SetStepMessage("Completed", 100)
	w.logger.Info("Reason Worker completed",
		zap.Int("friendChecks", friendCount),
		zap.Int("groupChecks", groupCount))

	// Wait for shutdown signal
	w.logger.Info("Reason worker finished processing, waiting for shutdown signal")
	w.bar.SetStepMessage("Waiting for shutdown", 100)
	<-ctx.Done()
	w.logger.Info("Reason worker shutting down gracefully")
}

// processUsersWithoutReason processes all users that don't have a specific reason type.
func (w *Worker) processUsersWithoutReason(ctx context.Context, reasonType enum.UserReasonType) (int, error) {
	totalProcessed := 0
	cursorID := uint64(0)

	for !utils.ContextGuardWithLog(ctx, w.logger, "Context cancelled during batch processing") {
		// Get batch of users without this reason type
		users, err := w.db.Model().User().GetUsersWithoutReason(ctx, reasonType, w.batchSize, cursorID)
		if err != nil {
			return totalProcessed, fmt.Errorf("failed to get users without %s reason: %w", reasonType.String(), err)
		}

		// If no more users, we're done
		if len(users) == 0 {
			break
		}

		// Get users with relationships for this batch
		usersWithRelationships, err := w.getUsersWithRelationships(ctx, users)
		if err != nil {
			w.logger.Error("Failed to get users with relationships for batch",
				zap.String("reasonType", reasonType.String()),
				zap.Error(err))

			continue
		}

		// Process the batch
		processed, err := w.processBatch(ctx, usersWithRelationships, reasonType)
		if err != nil {
			w.logger.Error("Failed to process batch",
				zap.String("reasonType", reasonType.String()),
				zap.Error(err))
		} else {
			totalProcessed += processed
			w.logger.Debug("Processed batch",
				zap.String("reasonType", reasonType.String()),
				zap.Int("batchSize", len(users)),
				zap.Int("processed", processed),
				zap.Int("totalProcessed", totalProcessed))
		}

		// Move to next batch
		cursorID = users[len(users)-1].ID

		// Add delay between batches
		utils.ContextSleep(ctx, w.batchDelay)
	}

	return totalProcessed, nil
}

// processBatch processes a batch of users through the appropriate checker.
func (w *Worker) processBatch(
	ctx context.Context, users []*types.ReviewUser, reasonType enum.UserReasonType,
) (int, error) {
	if len(users) == 0 {
		return 0, nil
	}

	// Create reasons map to track newly flagged users
	reasonsMap := make(map[uint64]types.Reasons[enum.UserReasonType])

	// Prepare maps for processing
	confirmedFriendsMap, flaggedFriendsMap := w.friendChecker.PrepareFriendMaps(ctx, users)
	confirmedGroupsMap, flaggedGroupsMap := w.groupChecker.PrepareGroupMaps(ctx, users)

	switch reasonType {
	case enum.UserReasonTypeFriend:
		// Process through friend checker
		w.friendChecker.ProcessUsers(ctx, &checker.FriendCheckerParams{
			Users:                     users,
			ReasonsMap:                reasonsMap,
			ConfirmedFriendsMap:       confirmedFriendsMap,
			FlaggedFriendsMap:         flaggedFriendsMap,
			ConfirmedGroupsMap:        confirmedGroupsMap,
			FlaggedGroupsMap:          flaggedGroupsMap,
			InappropriateFriendsFlags: nil,
		})
	case enum.UserReasonTypeGroup:
		// Process through group checker
		w.groupChecker.ProcessUsers(ctx, &checker.GroupCheckerParams{
			Users:                     users,
			ReasonsMap:                reasonsMap,
			ConfirmedFriendsMap:       confirmedFriendsMap,
			FlaggedFriendsMap:         flaggedFriendsMap,
			ConfirmedGroupsMap:        confirmedGroupsMap,
			FlaggedGroupsMap:          flaggedGroupsMap,
			InappropriateGroupsFlags:  nil,
		})
	default:
		return 0, fmt.Errorf("%w: %s", ErrUnsupportedReason, reasonType.String())
	}

	// Save newly flagged users
	if len(reasonsMap) > 0 {
		flaggedUsers := make(map[uint64]*types.ReviewUser)

		for _, user := range users {
			if reasons, ok := reasonsMap[user.ID]; ok {
				user.Reasons = reasons
				user.Confidence = utils.CalculateConfidence(reasons)
				flaggedUsers[user.ID] = user
			}
		}

		if err := w.db.Service().User().SaveUsers(ctx, flaggedUsers); err != nil {
			return 0, fmt.Errorf("failed to save flagged users: %w", err)
		}

		return len(flaggedUsers), nil
	}

	return 0, nil
}

// getUsersWithRelationships gets users with their relationships for a batch.
func (w *Worker) getUsersWithRelationships(ctx context.Context, batch []*types.ReviewUser) ([]*types.ReviewUser, error) {
	if len(batch) == 0 {
		return batch, nil
	}

	// Get user IDs for the batch
	userIDs := make([]uint64, len(batch))
	for i, user := range batch {
		userIDs[i] = user.ID
	}

	// Get users with relationships
	usersMap, err := w.db.Service().User().GetUsersByIDs(ctx, userIDs,
		types.UserFieldBasic|types.UserFieldProfile|types.UserFieldReasons|
			types.UserFieldFriends|types.UserFieldGroups|types.UserFieldStats|types.UserFieldTimestamps)
	if err != nil {
		return nil, fmt.Errorf("failed to get users with relationships: %w", err)
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
