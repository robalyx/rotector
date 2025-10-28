package commands

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/robalyx/rotector/internal/database/types/enum"
	"github.com/robalyx/rotector/internal/roblox/fetcher"
	"github.com/urfave/cli/v3"
	"go.uber.org/zap"
)

var (
	ErrUserIDRequired    = errors.New("USER_ID argument required in single mode")
	ErrInvalidUserID     = errors.New("invalid user ID: must be a number")
	ErrUserIDInMultiMode = errors.New("do not provide USER_ID argument in multi mode")
)

// FriendCleanupCommands returns all friend cleanup-related commands.
func FriendCleanupCommands(deps *CLIDependencies) []*cli.Command {
	return []*cli.Command{
		{
			Name:      "clean-friends",
			Usage:     "Remove inappropriate friends from user(s) friend list",
			ArgsUsage: "[USER_ID]",
			Description: `Clean friends for specific user(s):
  - USER_ID: Single Roblox user ID (required in single mode)
  - --multi: Enable multi-user mode with text editor
  - --status: Filter by status - 'flagged', 'confirmed', or 'both' (default: 'both')
  - --batch-size: Number of users to process in each batch (default: 100)

Single user mode:
  db clean-friends 12345                           # Clean both flagged and confirmed friends
  db clean-friends 12345 --status flagged         # Clean only flagged friends
  db clean-friends 12345 --status confirmed       # Clean only confirmed friends

Multi-user mode (opens text editor):
  db clean-friends --multi                         # Clean friends for multiple users
  db clean-friends --multi --status flagged       # Clean only flagged friends for multiple users`,
			Flags: []cli.Flag{
				&cli.BoolFlag{
					Name:    "multi",
					Usage:   "Enable multi-user mode with text editor",
					Aliases: []string{"m"},
				},
				&cli.StringFlag{
					Name:    "status",
					Usage:   "Filter by status: 'flagged', 'confirmed', or 'both'",
					Value:   "both",
					Aliases: []string{"s"},
				},
				&cli.IntFlag{
					Name:    "batch-size",
					Usage:   "Number of users to process in each batch",
					Value:   100,
					Aliases: []string{"b"},
				},
			},
			Action: handleCleanFriends(deps),
		},
	}
}

// handleCleanFriends handles the 'clean-friends' command.
func handleCleanFriends(deps *CLIDependencies) cli.ActionFunc {
	return func(ctx context.Context, c *cli.Command) error {
		multiMode := c.Bool("multi")
		statusFilter := c.String("status")
		batchSize := max(c.Int("batch-size"), 10)

		// Parse status filter
		statuses, err := parseStatusFilter(statusFilter)
		if err != nil {
			return err
		}

		// Get user IDs based on mode
		var userIDs []int64

		if multiMode {
			// Multi-user mode with text editor
			if c.Args().Len() > 0 {
				return ErrUserIDInMultiMode
			}

			userIDs, err = getUserIDsFromEditor(ctx, "friend cleanup")
			if err != nil {
				return err
			}

			if len(userIDs) == 0 {
				fmt.Println("No valid user IDs found.")
				return nil
			}

			fmt.Printf("Found %d user IDs to process.\n\n", len(userIDs))
		} else {
			// Single user mode
			if c.Args().Len() != 1 {
				return ErrUserIDRequired
			}

			userIDStr := c.Args().First()

			userID, err := strconv.ParseInt(userIDStr, 10, 64)
			if err != nil {
				return fmt.Errorf("%w: %q", ErrInvalidUserID, userIDStr)
			}

			userIDs = []int64{userID}
		}

		deps.Logger.Info("Starting friend cleanup process",
			zap.Int("userCount", len(userIDs)),
			zap.Int("batchSize", batchSize))

		// Process each user
		var totalInappropriateFriends int

		for i, userID := range userIDs {
			if len(userIDs) > 1 {
				fmt.Printf("\n[%d/%d] Processing user %d...\n", i+1, len(userIDs), userID)
			}

			inappropriateCount, err := processUserFriends(ctx, deps, userID, statuses, batchSize)
			if err != nil {
				deps.Logger.Error("Failed to process user",
					zap.Int64("userID", userID),
					zap.Error(err))

				log.Printf("Error processing user %d: %v", userID, err)

				continue
			}

			totalInappropriateFriends += inappropriateCount
		}

		// Summary
		if len(userIDs) > 1 {
			fmt.Printf("\n=== Summary ===\n")
			fmt.Printf("Processed %d users\n", len(userIDs))
			fmt.Printf("Total inappropriate friends removed: %d\n", totalInappropriateFriends)
		}

		deps.Logger.Info("Friend cleanup completed",
			zap.Int("totalUsers", len(userIDs)),
			zap.Int("totalInappropriateFriends", totalInappropriateFriends))

		return nil
	}
}

// processUserFriends processes a single user's friends for cleanup.
func processUserFriends(
	ctx context.Context, deps *CLIDependencies,
	userID int64, statuses []enum.UserType, batchSize int,
) (int, error) {
	// Create friend fetcher
	friendFetcher := fetcher.NewFriendFetcher(deps.DB, deps.RoAPI, deps.Logger)

	// Fetch friends for the user
	deps.Logger.Info("Fetching friends for user", zap.Int64("userID", userID))

	friendIDs, err := friendFetcher.GetFriendIDs(ctx, userID)
	if err != nil {
		return 0, fmt.Errorf("failed to fetch friend IDs: %w", err)
	}

	if len(friendIDs) == 0 {
		deps.Logger.Info("No friends found for user", zap.Int64("userID", userID))
		log.Printf("User %d has no friends.", userID)

		return 0, nil
	}

	deps.Logger.Info("Fetched friend IDs",
		zap.Int64("userID", userID),
		zap.Int("friendCount", len(friendIDs)))

	// Find which friends match the status filter
	inappropriateFriends, err := getUsersByStatuses(ctx, deps.DB, friendIDs, statuses)
	if err != nil {
		return 0, fmt.Errorf("failed to check friend status: %w", err)
	}

	if len(inappropriateFriends) == 0 {
		deps.Logger.Info("No inappropriate friends found for user",
			zap.Int64("userID", userID),
			zap.Int("totalFriends", len(friendIDs)))

		log.Printf("User %d: No inappropriate friends found (out of %d total friends).",
			userID, len(friendIDs))

		return 0, nil
	}

	// Extract inappropriate friend IDs
	inappropriateFriendIDs := make([]int64, len(inappropriateFriends))
	for i, user := range inappropriateFriends {
		inappropriateFriendIDs[i] = user.ID
	}

	// Log findings
	deps.Logger.Info("Found inappropriate friends to delete",
		zap.Int64("userID", userID),
		zap.Int("totalFriends", len(friendIDs)),
		zap.Int("inappropriateFriends", len(inappropriateFriends)))

	log.Printf("User %d: Found %d inappropriate friends (out of %d total friends)",
		userID, len(inappropriateFriends), len(friendIDs))

	// Process deletions in batches
	totalDeleted, err := processDeletions(ctx, deps, inappropriateFriendIDs, batchSize)
	if err != nil {
		return 0, fmt.Errorf("failed to process deletions: %w", err)
	}

	deps.Logger.Info("Successfully deleted inappropriate friends",
		zap.Int64("userID", userID),
		zap.Int("inappropriate_friends", len(inappropriateFriends)),
		zap.Int64("deleted_rows", totalDeleted))

	log.Printf("User %d: Successfully deleted %d inappropriate friends",
		userID, len(inappropriateFriends))

	return len(inappropriateFriends), nil
}

// getUserIDsFromEditor opens a text editor for the user to input multiple user IDs.
func getUserIDsFromEditor(ctx context.Context, purpose string) ([]int64, error) {
	instructions := `# Rotector Friend Cleanup
#
# Instructions:
# - Add one Roblox user ID per line
# - Lines starting with # are comments and will be ignored
# - Empty lines will be ignored
#
# Example:
# 1234567890
# 9876543210
#
# Add user IDs below (whose friends should be cleaned):

`

	// Create temporary file with instructions
	tempFile, err := createTempFile("rotector_"+strings.ReplaceAll(purpose, " ", "_"), instructions)
	if err != nil {
		return nil, fmt.Errorf("failed to create temporary file: %w", err)
	}
	defer os.Remove(tempFile)

	fmt.Printf("Created temporary file: %s\n", tempFile)
	fmt.Printf("Please add user IDs to the file (one per line).\n")
	fmt.Println("Press Enter when you're done editing the file...")

	// Try to open the file in the default editor
	if err := openFileInEditor(ctx, tempFile); err != nil {
		fmt.Printf("Warning: Could not open file in editor: %v\n", err)
		fmt.Println("Please manually edit the file and press Enter when done.")
	}

	// Wait for user input
	reader := bufio.NewReader(os.Stdin)

	_, _ = reader.ReadString('\n')

	// Read and process the file
	userIDs, err := readIDsFromFile(tempFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read user IDs: %w", err)
	}

	return userIDs, nil
}
