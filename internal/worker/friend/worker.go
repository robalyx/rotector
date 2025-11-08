package friend

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/robalyx/rotector/internal/cloudflare"
	"github.com/robalyx/rotector/internal/database"
	"github.com/robalyx/rotector/internal/database/types"
	"github.com/robalyx/rotector/internal/database/types/enum"
	"github.com/robalyx/rotector/internal/roblox/checker"
	"github.com/robalyx/rotector/internal/roblox/fetcher"
	"github.com/robalyx/rotector/internal/setup"
	"github.com/robalyx/rotector/internal/tui/components"
	"github.com/robalyx/rotector/internal/worker/core"
	"github.com/robalyx/rotector/pkg/utils"
	"go.uber.org/zap"
)

const (
	// MinFriendPercentage is the minimum percentage of friends that must be in the system for flagged users.
	MinFriendPercentage = 30.0
	// MinFriendsInSystem is the minimum number of friends that must be in the system for flagged users.
	MinFriendsInSystem = 10
	// DatabaseQueryBatchSize is the batch size for database queries to avoid overwhelming the database.
	DatabaseQueryBatchSize = 1000
)

// NetworkStats represents statistics about a user's network for filtering.
type NetworkStats struct {
	TotalFriends        int  // Total number of friends
	FlaggedFriends      int  // Number of flagged or confirmed friends
	ConfirmedFriends    int  // Number of confirmed friends only
	HasConfirmedOrMixed bool // Whether user has any confirmed or mixed groups
}

// Worker processes user friend networks by checking each friend's
// status and analyzing their profiles for inappropriate content.
type Worker struct {
	db               database.Client
	roAPI            *api.API
	cfClient         *cloudflare.Client
	bar              *components.ProgressBar
	userFetcher      *fetcher.UserFetcher
	userChecker      *checker.UserChecker
	friendFetcher    *fetcher.FriendFetcher
	reporter         *core.StatusReporter
	thresholdChecker *core.ThresholdChecker
	pendingFriends   []*types.ReviewUser
	logger           *zap.Logger
	batchSize        int
}

// New creates a new friend worker.
func New(app *setup.App, bar *components.ProgressBar, logger *zap.Logger, instanceID string) *Worker {
	userFetcher := fetcher.NewUserFetcher(app, logger)
	userChecker := checker.NewUserChecker(app, userFetcher, logger)
	friendFetcher := fetcher.NewFriendFetcher(app.DB, app.RoAPI, logger)
	reporter := core.NewStatusReporter(app.StatusClient, "friend", instanceID, logger)
	thresholdChecker := core.NewThresholdChecker(
		app.DB,
		app.Config.Worker.ThresholdLimits.FlaggedUsers,
		bar,
		reporter,
		logger.Named("friend_worker"),
		"friend worker",
	)

	return &Worker{
		db:               app.DB,
		roAPI:            app.RoAPI,
		cfClient:         app.CFClient,
		bar:              bar,
		userFetcher:      userFetcher,
		userChecker:      userChecker,
		friendFetcher:    friendFetcher,
		reporter:         reporter,
		thresholdChecker: thresholdChecker,
		pendingFriends:   make([]*types.ReviewUser, 0),
		logger:           logger.Named("friend_worker"),
		batchSize:        app.Config.Worker.BatchSizes.FriendUsers,
	}
}

// Start begins the friend worker's main loop.
func (w *Worker) Start(ctx context.Context) {
	w.logger.Info("Friend Worker started", zap.String("workerID", w.reporter.GetWorkerID()))

	w.reporter.Start(ctx)
	defer w.reporter.Stop()

	w.bar.SetTotal(100)

	for {
		// Check if context was cancelled
		if utils.ContextGuardWithLog(ctx, w.logger, "Context cancelled, stopping friend worker") {
			w.bar.SetStepMessage("Shutting down", 100)
			w.reporter.UpdateStatus("Shutting down", 100)

			return
		}

		w.bar.Reset()
		w.reporter.SetHealthy(true)

		// Check flagged users threshold
		thresholdExceeded, err := w.thresholdChecker.CheckThreshold(ctx)
		if err != nil {
			if !utils.ErrorSleep(ctx, 5*time.Minute, w.logger, "friend worker") {
				return
			}

			continue
		}

		if thresholdExceeded {
			continue
		}

		// Step 1: Process friends batch (40%)
		w.bar.SetStepMessage("Processing friends batch", 40)
		w.reporter.UpdateStatus("Processing friends batch", 40)

		userInfos, err := w.processFriendsBatch(ctx)
		if err != nil {
			w.reporter.SetHealthy(false)

			if !utils.ErrorSleep(ctx, 5*time.Minute, w.logger, "friend worker") {
				return
			}

			continue
		}

		// Step 2: Process users (60%)
		w.bar.SetStepMessage("Processing users", 60)
		w.reporter.UpdateStatus("Processing users", 60)
		w.userChecker.ProcessUsers(ctx, &checker.UserCheckerParams{
			Users:                     userInfos[:w.batchSize],
			InappropriateOutfitFlags:  nil,
			InappropriateProfileFlags: nil,
			InappropriateFriendsFlags: nil,
			InappropriateGroupsFlags:  nil,
			FromQueueWorker:           false,
		})

		// Mark processed users in cache to prevent reprocessing
		userCreationDates := make(map[int64]time.Time)
		for _, user := range userInfos[:w.batchSize] {
			userCreationDates[user.ID] = user.CreatedAt
		}

		if err := w.db.Service().Cache().MarkUsersProcessed(ctx, userCreationDates); err != nil {
			w.logger.Error("Failed to mark users as processed in cache", zap.Error(err))
		}

		// Step 3: Processing completed (80%)
		w.bar.SetStepMessage("Processing completed", 80)
		w.reporter.UpdateStatus("Processing completed", 80)

		// Step 4: Prepare for next batch
		w.pendingFriends = userInfos[w.batchSize:]

		// Step 5: Completed (100%)
		w.bar.SetStepMessage("Completed", 100)
		w.reporter.UpdateStatus("Completed", 100)

		// Short pause before next iteration
		if !utils.IntervalSleep(ctx, 1*time.Second, w.logger, "friend worker") {
			return
		}
	}
}

// processFriendsBatch builds a list of validated friends to check.
func (w *Worker) processFriendsBatch(ctx context.Context) ([]*types.ReviewUser, error) {
	validFriends := w.pendingFriends

	// Track processing metrics
	usersProcessed := 0
	usersSkipped := 0

	for len(validFriends) < w.batchSize {
		// Get the next confirmed user
		user, err := w.db.Model().User().GetUserToScan(ctx)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				w.logger.Warn("No more users to scan")
				break
			}

			w.logger.Error("Error getting user to scan", zap.Error(err))

			return validFriends, err
		}

		// Check if we should process this user's friends based on count changes
		userFriendIDs, err := w.friendFetcher.GetFriendIDs(ctx, user.ID)
		if err != nil {
			w.logger.Error("Error fetching friends", zap.Error(err), zap.Int64("userID", user.ID))
			continue
		}

		currentFriendCount := len(userFriendIDs)

		// Compare current friend count with cached value
		friendCountChanged, err := w.db.Model().Cache().HasFriendCountChanged(ctx, user.ID, currentFriendCount)
		if err != nil {
			w.logger.Warn("Error checking friend count cache, proceeding with processing",
				zap.Int64("userID", user.ID),
				zap.Error(err))
		} else if !friendCountChanged {
			usersSkipped++

			w.logger.Debug("User friend count unchanged, skipping",
				zap.Int64("userID", user.ID),
				zap.Int("friendCount", currentFriendCount))

			continue
		}

		// If the user has no friends, skip them
		if currentFriendCount == 0 {
			continue
		}

		// Cache the current friend count
		if err := w.db.Model().Cache().SetFriendCount(ctx, user.ID, currentFriendCount); err != nil {
			w.logger.Warn("Failed to cache friend count",
				zap.Int64("userID", user.ID),
				zap.Int("friendCount", currentFriendCount),
				zap.Error(err))
		}

		// Get all users that exist in our system
		existingUsers, err := w.db.Model().User().GetUsersByIDs(ctx, userFriendIDs, types.UserFieldID)
		if err != nil {
			w.logger.Error("Error checking existing users", zap.Error(err))
			continue
		}

		// For flagged users, check if they meet the friend criteria
		if user.Status == enum.UserTypeFlagged {
			existingCount := len(existingUsers)
			friendPercentage := (float64(existingCount) / float64(len(userFriendIDs))) * 100

			if existingCount < MinFriendsInSystem || friendPercentage < MinFriendPercentage {
				w.logger.Debug("Flagged user does not meet friend criteria",
					zap.Int64("userID", user.ID),
					zap.Int("totalFriends", len(userFriendIDs)),
					zap.Int("existingFriends", existingCount),
					zap.Float64("friendPercentage", friendPercentage))

				continue
			}

			w.logger.Info("Processing flagged user",
				zap.Int("userFriends", len(userFriendIDs)),
				zap.Int("existingFriends", existingCount),
				zap.Float64("friendPercentage", friendPercentage),
				zap.Int64("userID", user.ID))
		} else {
			w.logger.Info("Processing confirmed user",
				zap.Int("userFriends", len(userFriendIDs)),
				zap.Int("existingFriends", len(existingUsers)),
				zap.Int64("userID", user.ID))
		}

		usersProcessed++

		// Collect users that do not exist in our system
		var friendIDs []int64

		for _, userID := range userFriendIDs {
			if _, exists := existingUsers[userID]; !exists {
				friendIDs = append(friendIDs, userID)
			}
		}

		if len(friendIDs) > 0 {
			// Filter out users within their processing cooldown period
			unprocessedIDs, err := w.db.Service().Cache().FilterProcessedUsers(ctx, friendIDs)
			if err != nil {
				w.logger.Error("Error filtering processed users",
					zap.Error(err),
					zap.Int64("userID", user.ID))

				continue
			}

			// Add fetched users to the validation list
			if len(unprocessedIDs) > 0 {
				userInfos := w.userFetcher.FetchInfos(ctx, unprocessedIDs)

				if len(userInfos) > 0 {
					filteredUserInfos := w.filterUsersByNetwork(ctx, userInfos)
					validFriends = append(validFriends, filteredUserInfos...)

					w.logger.Debug("Added friends for processing",
						zap.Int64("userID", user.ID),
						zap.Int("totalFriends", len(userFriendIDs)),
						zap.Int("existingFriends", len(existingUsers)),
						zap.Int("newFriends", len(friendIDs)),
						zap.Int("unprocessedFriends", len(unprocessedIDs)),
						zap.Int("fetchedFriends", len(userInfos)),
						zap.Int("filteredFriends", len(filteredUserInfos)),
						zap.Int("totalValidFriends", len(validFriends)))
				}
			}
		}
	}

	// Log processing summary
	if usersProcessed > 0 || usersSkipped > 0 {
		totalUsers := usersProcessed + usersSkipped
		skipRate := float64(usersSkipped) / float64(totalUsers) * 100

		w.logger.Info("Friend count optimization summary",
			zap.Int("usersProcessed", usersProcessed),
			zap.Int("usersSkipped", usersSkipped),
			zap.Int("totalUsers", totalUsers),
			zap.Float64("skipRate", skipRate),
			zap.Int("validFriendsFound", len(validFriends)))
	}

	return validFriends, nil
}

// filterUsersByNetwork filters users based on their network characteristics.
func (w *Worker) filterUsersByNetwork(ctx context.Context, userInfos []*types.ReviewUser) []*types.ReviewUser {
	if len(userInfos) == 0 {
		return userInfos
	}

	// Collect all friend IDs and group IDs from all users
	allFriendIDs := make(map[int64]bool)
	allGroupIDs := make(map[int64]bool)

	for _, user := range userInfos {
		for _, friend := range user.Friends {
			allFriendIDs[friend.ID] = true
		}

		for _, group := range user.Groups {
			allGroupIDs[group.Group.ID] = true
		}
	}

	// Convert maps to slices
	friendIDsList := make([]int64, 0, len(allFriendIDs))
	for friendID := range allFriendIDs {
		friendIDsList = append(friendIDsList, friendID)
	}

	groupIDsList := make([]int64, 0, len(allGroupIDs))
	for groupID := range allGroupIDs {
		groupIDsList = append(groupIDsList, groupID)
	}

	// Query friend and group statuses
	friendStatuses, err := w.batchQueryFriendStatuses(ctx, friendIDsList)
	if err != nil {
		w.logger.Error("Failed to query friend statuses, processing all users", zap.Error(err))
		return userInfos
	}

	groupStatuses, err := w.batchQueryGroupStatuses(ctx, groupIDsList)
	if err != nil {
		w.logger.Error("Failed to query group statuses, processing all users", zap.Error(err))
		return userInfos
	}

	// Filter users based on criteria
	filtered := make([]*types.ReviewUser, 0, len(userInfos))
	filteredCount := 0

	for _, user := range userInfos {
		// Calculate network stats for this user
		stats := w.calculateUserNetworkStats(user, friendStatuses, groupStatuses)

		// Apply filter criteria
		if stats.FlaggedFriends >= 4 ||
			stats.ConfirmedFriends >= 2 ||
			(stats.TotalFriends > 0 && float64(stats.FlaggedFriends)/float64(stats.TotalFriends) >= 0.5) ||
			stats.HasConfirmedOrMixed {
			filtered = append(filtered, user)
		} else {
			filteredCount++
		}
	}

	// Log filtering metrics
	if len(userInfos) > 0 {
		filterRate := float64(filteredCount) / float64(len(userInfos)) * 100

		w.logger.Info("Network filtering summary",
			zap.Int("totalUsers", len(userInfos)),
			zap.Int("processedUsers", len(filtered)),
			zap.Int("filteredUsers", filteredCount),
			zap.Int("totalFriendIDs", len(friendIDsList)),
			zap.Int("totalGroupIDs", len(groupIDsList)),
			zap.Float64("filterRate", filterRate))
	}

	return filtered
}

// batchQueryFriendStatuses queries friend statuses in batches.
func (w *Worker) batchQueryFriendStatuses(
	ctx context.Context, friendIDs []int64,
) (map[int64]enum.UserType, error) {
	friendStatuses := make(map[int64]enum.UserType)

	if len(friendIDs) == 0 {
		return friendStatuses, nil
	}

	// Process in chunks
	for i := 0; i < len(friendIDs); i += DatabaseQueryBatchSize {
		end := min(i+DatabaseQueryBatchSize, len(friendIDs))
		chunk := friendIDs[i:end]

		// Query this chunk
		friends, err := w.db.Model().User().GetUsersByIDs(
			ctx, chunk, types.UserFieldID|types.UserFieldStatus,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to get friend statuses for chunk %d-%d: %w", i, end, err)
		}

		// Merge into result map
		for id, friend := range friends {
			friendStatuses[id] = friend.Status
		}
	}

	return friendStatuses, nil
}

// batchQueryGroupStatuses queries group statuses in batches.
func (w *Worker) batchQueryGroupStatuses(
	ctx context.Context, groupIDs []int64,
) (map[int64]enum.GroupType, error) {
	groupStatuses := make(map[int64]enum.GroupType)

	if len(groupIDs) == 0 {
		return groupStatuses, nil
	}

	// Process in chunks
	for i := 0; i < len(groupIDs); i += DatabaseQueryBatchSize {
		end := min(i+DatabaseQueryBatchSize, len(groupIDs))
		chunk := groupIDs[i:end]

		// Query this chunk
		groups, err := w.db.Model().Group().GetGroupsByIDs(
			ctx, chunk, types.GroupFieldID|types.GroupFieldStatus,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to get group statuses for chunk %d-%d: %w", i, end, err)
		}

		// Merge into result map
		for id, group := range groups {
			groupStatuses[id] = group.Status
		}
	}

	return groupStatuses, nil
}

// calculateUserNetworkStats calculates network statistics for a user based on their friends and groups.
func (w *Worker) calculateUserNetworkStats(
	user *types.ReviewUser, friendStatuses map[int64]enum.UserType, groupStatuses map[int64]enum.GroupType,
) *types.NetworkStats {
	stats := &types.NetworkStats{
		TotalFriends: len(user.Friends),
	}

	// Count flagged/confirmed friends
	for _, friend := range user.Friends {
		if status, exists := friendStatuses[friend.ID]; exists {
			if status == enum.UserTypeFlagged || status == enum.UserTypeConfirmed {
				stats.FlaggedFriends++
			}

			if status == enum.UserTypeConfirmed {
				stats.ConfirmedFriends++
			}
		}
	}

	// Check for confirmed/mixed groups
	for _, group := range user.Groups {
		if status, exists := groupStatuses[group.Group.ID]; exists {
			if status == enum.GroupTypeConfirmed || status == enum.GroupTypeMixed {
				stats.HasConfirmedOrMixed = true
				break
			}
		}
	}

	return stats
}
