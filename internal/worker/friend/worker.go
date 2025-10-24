package friend

import (
	"context"
	"database/sql"
	"errors"
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
)

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
	friendFetcher := fetcher.NewFriendFetcher(app, logger)
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
			if !errors.Is(err, sql.ErrNoRows) {
				w.logger.Error("Error getting user to scan", zap.Error(err))
			} else {
				w.logger.Warn("No more users to scan", zap.Error(err))
			}

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
			// Fetch all friend user infos
			allUserInfos := w.userFetcher.FetchInfos(ctx, friendIDs)

			// Filter out users within their processing cooldown period
			unprocessedUserInfos, err := w.db.Service().Cache().FilterProcessedUsers(ctx, allUserInfos)
			if err != nil {
				w.logger.Error("Error filtering processed users",
					zap.Error(err),
					zap.Int64("userID", user.ID))

				continue
			}

			// Add unprocessed users to the validation list
			if len(unprocessedUserInfos) > 0 {
				validFriends = append(validFriends, unprocessedUserInfos...)

				w.logger.Debug("Added friends for processing",
					zap.Int64("userID", user.ID),
					zap.Int("totalFriends", len(userFriendIDs)),
					zap.Int("existingFriends", len(existingUsers)),
					zap.Int("fetchedFriends", len(friendIDs)),
					zap.Int("allUserInfos", len(allUserInfos)),
					zap.Int("unprocessedFriends", len(unprocessedUserInfos)),
					zap.Int("totalValidFriends", len(validFriends)))
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
