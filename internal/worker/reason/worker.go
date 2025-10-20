package reason

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/robalyx/rotector/internal/database"
	"github.com/robalyx/rotector/internal/database/types"
	"github.com/robalyx/rotector/internal/database/types/enum"
	"github.com/robalyx/rotector/internal/roblox/checker"
	"github.com/robalyx/rotector/internal/setup"
	"github.com/robalyx/rotector/internal/tui/components"
	"github.com/robalyx/rotector/internal/worker/core"
	"github.com/robalyx/rotector/pkg/utils"
	"go.uber.org/zap"
)

var ErrUnsupportedReason = errors.New("unsupported reason type")

// Worker checks all users from the database and runs them through friend, group,
// and condo checkers for users that don't have the respective reasons.
type Worker struct {
	db            database.Client
	bar           *components.ProgressBar
	friendChecker *checker.FriendChecker
	groupChecker  *checker.GroupChecker
	condoChecker  *checker.CondoChecker
	reporter      *core.StatusReporter
	logger        *zap.Logger
	batchSize     int
	batchDelay    time.Duration
}

// New creates a new reason worker.
func New(app *setup.App, bar *components.ProgressBar, logger *zap.Logger, instanceID string) *Worker {
	reporter := core.NewStatusReporter(app.StatusClient, "reason", instanceID, logger)

	return &Worker{
		db:            app.DB,
		bar:           bar,
		friendChecker: checker.NewFriendChecker(app, logger),
		groupChecker:  checker.NewGroupChecker(app, logger),
		condoChecker:  checker.NewCondoChecker(app, logger),
		reporter:      reporter,
		logger:        logger.Named("reason_worker"),
		batchSize:     200,
		batchDelay:    1 * time.Second,
	}
}

// Start begins the reason worker's operation.
func (w *Worker) Start(ctx context.Context) {
	w.logger.Info("Reason Worker started")

	// Start status reporting
	w.reporter.Start(ctx)
	defer w.reporter.Stop()

	w.bar.SetTotal(100)
	w.bar.SetStepMessage("Starting reason check process", 0)

	// Process condo reasons
	w.bar.SetStepMessage("Processing users missing condo reasons", 15)

	condoCount, err := w.processUsersWithoutReason(ctx, enum.UserReasonTypeCondo)
	if err != nil {
		w.logger.Error("Failed to process users without condo reasons", zap.Error(err))
	} else {
		w.logger.Info("Processed users without condo reasons", zap.Int("count", condoCount))
	}

	// Process friend reasons
	w.bar.SetStepMessage("Processing users missing friend reasons", 30)

	friendCount, err := w.processUsersWithoutReason(ctx, enum.UserReasonTypeFriend)
	if err != nil {
		w.logger.Error("Failed to process users without friend reasons", zap.Error(err))
	} else {
		w.logger.Info("Processed users without friend reasons", zap.Int("count", friendCount))
	}

	// Process group reasons
	w.bar.SetStepMessage("Processing users missing group reasons", 45)

	groupCount, err := w.processUsersWithoutReason(ctx, enum.UserReasonTypeGroup)
	if err != nil {
		w.logger.Error("Failed to process users without group reasons", zap.Error(err))
	} else {
		w.logger.Info("Processed users without group reasons", zap.Int("count", groupCount))
	}

	// Recalculate condo reason confidences
	w.bar.SetStepMessage("Recalculating condo reason confidences", 60)

	condoRecalcCount, err := w.processUsersWithReason(ctx, enum.UserReasonTypeCondo)
	if err != nil {
		w.logger.Error("Failed to recalculate condo reason confidences", zap.Error(err))
	} else {
		w.logger.Info("Recalculated condo reason confidences", zap.Int("updatedCount", condoRecalcCount))
	}

	// Recalculate friend reason confidences
	w.bar.SetStepMessage("Recalculating friend reason confidences", 75)

	friendRecalcCount, err := w.processUsersWithReason(ctx, enum.UserReasonTypeFriend)
	if err != nil {
		w.logger.Error("Failed to recalculate friend reason confidences", zap.Error(err))
	} else {
		w.logger.Info("Recalculated friend reason confidences", zap.Int("updatedCount", friendRecalcCount))
	}

	// Recalculate group reason confidences
	w.bar.SetStepMessage("Recalculating group reason confidences", 90)

	groupRecalcCount, err := w.processUsersWithReason(ctx, enum.UserReasonTypeGroup)
	if err != nil {
		w.logger.Error("Failed to recalculate group reason confidences", zap.Error(err))
	} else {
		w.logger.Info("Recalculated group reason confidences", zap.Int("updatedCount", groupRecalcCount))
	}

	w.bar.SetStepMessage("Completed", 100)
	w.logger.Info("Reason Worker completed",
		zap.Int("condoChecks", condoCount),
		zap.Int("friendChecks", friendCount),
		zap.Int("groupChecks", groupCount),
		zap.Int("condoRecalculations", condoRecalcCount),
		zap.Int("friendRecalculations", friendRecalcCount),
		zap.Int("groupRecalculations", groupRecalcCount))

	// Wait for shutdown signal
	w.logger.Info("Reason worker finished processing, waiting for shutdown signal")
	w.bar.SetStepMessage("Waiting for shutdown", 100)
	<-ctx.Done()
	w.logger.Info("Reason worker shutting down gracefully")
}

// processUsersWithoutReason processes all users that don't have a specific reason type.
func (w *Worker) processUsersWithoutReason(ctx context.Context, reasonType enum.UserReasonType) (int, error) {
	totalProcessed := 0
	cursorID := int64(0)

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
			w.logger.Info("Processed batch",
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

// processUsersWithReason processes all users that have a specific reason type and
// recalculates their confidence, updating if the new confidence is higher.
func (w *Worker) processUsersWithReason(ctx context.Context, reasonType enum.UserReasonType) (int, error) {
	totalProcessed := 0
	cursorID := int64(0)

	for !utils.ContextGuardWithLog(ctx, w.logger, "Context cancelled during recalculation batch processing") {
		// Get batch of users with this reason type
		users, err := w.db.Model().User().GetUsersWithReason(ctx, reasonType, w.batchSize, cursorID)
		if err != nil {
			return totalProcessed, fmt.Errorf("failed to get users with %s reason: %w", reasonType.String(), err)
		}

		// If no more users, we're done
		if len(users) == 0 {
			break
		}

		// Get users with relationships for this batch
		usersWithRelationships, err := w.getUsersWithRelationships(ctx, users)
		if err != nil {
			w.logger.Error("Failed to get users with relationships for recalculation batch",
				zap.String("reasonType", reasonType.String()),
				zap.Error(err))

			continue
		}

		// Process the batch in recalculation mode
		processed, err := w.processBatchRecalculate(ctx, usersWithRelationships, reasonType)
		if err != nil {
			w.logger.Error("Failed to process recalculation batch",
				zap.String("reasonType", reasonType.String()),
				zap.Error(err))
		} else {
			totalProcessed += processed
			w.logger.Info("Processed recalculation batch",
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
	reasonsMap := make(map[int64]types.Reasons[enum.UserReasonType])

	// Initialize reasons map with existing user reasons to preserve them
	for _, user := range users {
		if user.Reasons != nil {
			reasonsMap[user.ID] = user.Reasons
		}
	}

	// Prepare maps for processing
	confirmedFriendsMap, flaggedFriendsMap := w.friendChecker.PrepareFriendMaps(ctx, users)
	confirmedGroupsMap, flaggedGroupsMap, mixedGroupsMap := w.groupChecker.PrepareGroupMaps(ctx, users)

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
			Users:                    users,
			ReasonsMap:               reasonsMap,
			ConfirmedFriendsMap:      confirmedFriendsMap,
			FlaggedFriendsMap:        flaggedFriendsMap,
			ConfirmedGroupsMap:       confirmedGroupsMap,
			FlaggedGroupsMap:         flaggedGroupsMap,
			MixedGroupsMap:           mixedGroupsMap,
			InappropriateGroupsFlags: nil,
		})
	case enum.UserReasonTypeCondo:
		// Process through condo checker
		if err := w.condoChecker.ProcessUsers(ctx, &checker.CondoCheckerParams{
			Users:              users,
			ReasonsMap:         reasonsMap,
			ConfirmedGroupsMap: confirmedGroupsMap,
		}); err != nil {
			return 0, fmt.Errorf("failed to process condo checker: %w", err)
		}
	default:
		return 0, fmt.Errorf("%w: %s", ErrUnsupportedReason, reasonType.String())
	}

	// Save newly flagged users
	if len(reasonsMap) > 0 {
		flaggedUsers := make(map[int64]*types.ReviewUser)

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

// processBatchRecalculate processes a batch of users to recalculate confidences,
// updating users only if the new confidence is higher than the existing one.
func (w *Worker) processBatchRecalculate(
	ctx context.Context, users []*types.ReviewUser, reasonType enum.UserReasonType,
) (int, error) {
	if len(users) == 0 {
		return 0, nil
	}

	// Create reasons map to track potential updates
	reasonsMap := make(map[int64]types.Reasons[enum.UserReasonType])

	// Initialize reasons map with existing user reasons
	for _, user := range users {
		if user.Reasons != nil {
			reasonsMap[user.ID] = user.Reasons
		}
	}

	// Prepare maps for processing
	confirmedFriendsMap, flaggedFriendsMap := w.friendChecker.PrepareFriendMaps(ctx, users)
	confirmedGroupsMap, flaggedGroupsMap, mixedGroupsMap := w.groupChecker.PrepareGroupMaps(ctx, users)

	// Store original confidences for comparison
	originalConfidences := make(map[int64]float64)

	for _, user := range users {
		if existingReason, exists := user.Reasons[reasonType]; exists {
			originalConfidences[user.ID] = existingReason.Confidence
		}
	}

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
			SkipReasonGeneration:      true,
		})
	case enum.UserReasonTypeGroup:
		// Process through group checker
		w.groupChecker.ProcessUsers(ctx, &checker.GroupCheckerParams{
			Users:                    users,
			ReasonsMap:               reasonsMap,
			ConfirmedFriendsMap:      confirmedFriendsMap,
			FlaggedFriendsMap:        flaggedFriendsMap,
			ConfirmedGroupsMap:       confirmedGroupsMap,
			FlaggedGroupsMap:         flaggedGroupsMap,
			MixedGroupsMap:           mixedGroupsMap,
			InappropriateGroupsFlags: nil,
			SkipReasonGeneration:     true,
		})
	case enum.UserReasonTypeCondo:
		// Process through condo checker
		if err := w.condoChecker.ProcessUsers(ctx, &checker.CondoCheckerParams{
			Users:              users,
			ReasonsMap:         reasonsMap,
			ConfirmedGroupsMap: confirmedGroupsMap,
		}); err != nil {
			return 0, fmt.Errorf("failed to process condo checker: %w", err)
		}
	default:
		return 0, fmt.Errorf("%w: %s", ErrUnsupportedReason, reasonType.String())
	}

	// Check for improved confidences and update users accordingly
	updatedUsers := make(map[int64]*types.ReviewUser)

	for _, user := range users {
		if newReasons, ok := reasonsMap[user.ID]; ok {
			if newReason, hasNewReason := newReasons[reasonType]; hasNewReason {
				originalConfidence := originalConfidences[user.ID]
				newConfidence := newReason.Confidence

				// Only update if new confidence is higher
				if newConfidence > originalConfidence {
					user.Reasons = newReasons
					user.Confidence = utils.CalculateConfidence(newReasons)
					updatedUsers[user.ID] = user

					w.logger.Debug("Updated user confidence",
						zap.Int64("userID", user.ID),
						zap.String("reasonType", reasonType.String()),
						zap.Float64("oldConfidence", originalConfidence),
						zap.Float64("newConfidence", newConfidence),
						zap.Float64("overallConfidence", user.Confidence))
				}
			}
		}
	}

	// Save updated users
	if len(updatedUsers) > 0 {
		if err := w.db.Service().User().SaveUsers(ctx, updatedUsers); err != nil {
			return 0, fmt.Errorf("failed to save updated users: %w", err)
		}

		return len(updatedUsers), nil
	}

	return 0, nil
}

// getUsersWithRelationships gets users with their relationships for a batch.
func (w *Worker) getUsersWithRelationships(ctx context.Context, batch []*types.ReviewUser) ([]*types.ReviewUser, error) {
	if len(batch) == 0 {
		return batch, nil
	}

	// Get user IDs for the batch
	userIDs := make([]int64, len(batch))
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
