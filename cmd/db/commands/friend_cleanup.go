package commands

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/robalyx/rotector/internal/database"
	"github.com/robalyx/rotector/internal/database/types"
	"github.com/robalyx/rotector/internal/database/types/enum"
	"github.com/robalyx/rotector/internal/roblox/fetcher"
	"github.com/robalyx/rotector/internal/setup"
	"github.com/robalyx/rotector/internal/setup/telemetry"
	"github.com/urfave/cli/v3"
	"go.uber.org/zap"
)

var (
	ErrUserIDRequired = errors.New("USER_ID argument required")
	ErrInvalidUserID  = errors.New("invalid user ID: must be a number")
)

// FriendCleanupCommands returns all friend cleanup-related commands.
func FriendCleanupCommands(deps *CLIDependencies) []*cli.Command {
	return []*cli.Command{
		{
			Name:      "clean-flagged-friends",
			Usage:     "Remove flagged friends from a user's friend list",
			ArgsUsage: "USER_ID",
			Description: `Clean flagged friends for a specific user:
  - USER_ID: The Roblox user ID to fetch friends for (required)
  - --batch-size: Number of users to process in each batch (default: 100)

Examples:
  db clean-flagged-friends 12345                    # Clean flagged friends for user 12345
  db clean-flagged-friends 12345 --batch-size 50   # Clean with smaller batch size`,
			Flags: []cli.Flag{
				&cli.IntFlag{
					Name:    "batch-size",
					Usage:   "Number of users to process in each batch",
					Value:   100,
					Aliases: []string{"b"},
				},
			},
			Action: handleCleanFlaggedFriends(deps),
		},
	}
}

// handleCleanFlaggedFriends handles the 'clean-flagged-friends' command.
func handleCleanFlaggedFriends(deps *CLIDependencies) cli.ActionFunc {
	return func(ctx context.Context, c *cli.Command) error {
		if c.Args().Len() != 1 {
			return ErrUserIDRequired
		}

		// Parse user ID
		userIDStr := c.Args().First()

		userID, err := strconv.ParseInt(userIDStr, 10, 64)
		if err != nil {
			return fmt.Errorf("%w: %q", ErrInvalidUserID, userIDStr)
		}

		// Get batch size from flag
		batchSize := max(c.Int("batch-size"), 10)

		deps.Logger.Info("Starting friend cleanup process",
			zap.Int64("userID", userID),
			zap.Int("batchSize", batchSize))

		// Initialize application
		app, err := setup.InitializeApp(ctx, telemetry.ServiceExport, "logs/cli", "db", "friend-cleanup")
		if err != nil {
			return fmt.Errorf("failed to initialize app: %w", err)
		}

		// Create friend fetcher
		friendFetcher := fetcher.NewFriendFetcher(app, app.Logger)

		// Fetch friends for the user
		deps.Logger.Info("Fetching friends for user", zap.Int64("userID", userID))

		friendIDs, err := friendFetcher.GetFriendIDs(ctx, userID)
		if err != nil {
			return fmt.Errorf("failed to fetch friend IDs for user %d: %w", userID, err)
		}

		if len(friendIDs) == 0 {
			deps.Logger.Info("No friends found for user", zap.Int64("userID", userID))
			return nil
		}

		deps.Logger.Info("Fetched friend IDs",
			zap.Int64("userID", userID),
			zap.Int("friendCount", len(friendIDs)))

		// Find which friends are flagged in our system
		flaggedFriends, err := getUsersByStatus(ctx, app.DB, friendIDs, enum.UserTypeFlagged)
		if err != nil {
			return fmt.Errorf("failed to check friend status: %w", err)
		}

		if len(flaggedFriends) == 0 {
			deps.Logger.Info("No flagged friends found for user",
				zap.Int64("userID", userID),
				zap.Int("totalFriends", len(friendIDs)))

			return nil
		}

		// Extract flagged friend IDs
		flaggedFriendIDs := make([]int64, len(flaggedFriends))
		for i, user := range flaggedFriends {
			flaggedFriendIDs[i] = user.ID
		}

		// Ask for confirmation
		deps.Logger.Info("Found flagged friends to delete",
			zap.Int64("userID", userID),
			zap.Int("totalFriends", len(friendIDs)),
			zap.Int("flaggedFriends", len(flaggedFriends)))

		log.Printf("Are you sure you want to delete these %d flagged friends of user %d in batches of %d? (y/N)",
			len(flaggedFriends), userID, batchSize)

		var response string

		_, _ = fmt.Scanln(&response)

		if response != "y" && response != "Y" {
			deps.Logger.Info("Operation cancelled")
			return nil
		}

		// Process deletions in batches
		var (
			totalDeleted   int64
			totalProcessed int
		)

		for i := 0; i < len(flaggedFriendIDs); i += batchSize {
			end := min(i+batchSize, len(flaggedFriendIDs))
			batchIDs := flaggedFriendIDs[i:end]
			batchCount := len(batchIDs)

			deps.Logger.Info("Processing deletion batch",
				zap.Int("batch", i/batchSize+1),
				zap.Int("size", batchCount),
				zap.Int("processed", totalProcessed),
				zap.Int("remaining", len(flaggedFriendIDs)-totalProcessed))

			// Delete from PostgreSQL database
			deleted, err := app.DB.Service().User().DeleteUsers(ctx, batchIDs)
			if err != nil {
				return fmt.Errorf("failed to delete users from database in batch %d: %w", i/batchSize+1, err)
			}

			totalDeleted += deleted
			totalProcessed += batchCount

			// Remove from Cloudflare D1 in batch
			if err := app.D1Client.UserFlags.RemoveBatch(ctx, batchIDs); err != nil {
				app.Logger.Warn("Failed to remove users from Cloudflare D1",
					zap.Error(err),
					zap.Int64s("batchIDs", batchIDs))
			}

			deps.Logger.Info("Batch processed successfully",
				zap.Int("batch", i/batchSize+1),
				zap.Int("processed", batchCount),
				zap.Int64("deleted_rows", deleted))

			// Add a small delay between batches to reduce system load
			if end < len(flaggedFriendIDs) {
				time.Sleep(100 * time.Millisecond)
			}
		}

		deps.Logger.Info("Successfully deleted all flagged friends",
			zap.Int64("userID", userID),
			zap.Int("total_flagged_friends", len(flaggedFriends)),
			zap.Int64("total_deleted_rows", totalDeleted))

		return nil
	}
}

// getUsersByStatus gets users by their IDs filtered by status.
func getUsersByStatus(ctx context.Context, db database.Client, userIDs []int64, status enum.UserType) ([]*types.User, error) {
	if len(userIDs) == 0 {
		return []*types.User{}, nil
	}

	// Get users by IDs with basic fields
	userMap, err := db.Model().User().GetUsersByIDs(ctx, userIDs, types.UserFieldBasic)
	if err != nil {
		return nil, fmt.Errorf("failed to get users by IDs: %w", err)
	}

	// Filter by status and convert to simple User slice
	var result []*types.User
	for _, reviewUser := range userMap {
		if reviewUser.User != nil && reviewUser.Status == status {
			result = append(result, reviewUser.User)
		}
	}

	return result, nil
}
