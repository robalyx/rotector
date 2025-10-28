package commands

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/urfave/cli/v3"
	"go.uber.org/zap"
)

var (
	ErrUserIDRequiredForDeletion  = errors.New("USER_ID argument required in single mode")
	ErrGroupIDRequiredForDeletion = errors.New("GROUP_ID argument required in single mode")
	ErrInvalidUserIDForDeletion   = errors.New("invalid user ID: must be a number")
	ErrInvalidGroupIDForDeletion  = errors.New("invalid group ID: must be a number")
	ErrUserIDInMultiModeDeletion  = errors.New("do not provide USER_ID argument in multi mode")
	ErrGroupIDInMultiModeDeletion = errors.New("do not provide GROUP_ID argument in multi mode")
)

// DeletionCommands returns all deletion-related commands.
func DeletionCommands(deps *CLIDependencies) []*cli.Command {
	return []*cli.Command{
		{
			Name:      "delete-users",
			Usage:     "Delete user(s) and all associated data from the database",
			ArgsUsage: "[USER_ID]",
			Description: `Delete specific user(s) from the system:
  - USER_ID: Single Roblox user ID (required in single mode)
  - --multi: Enable multi-user mode with text editor
  - --batch-size: Number of users to process in each batch (default: 100)

Single user mode:
  db delete-users 12345

Multi-user mode (opens text editor):
  db delete-users --multi

WARNING: This permanently deletes users and all their associated data!`,
			Flags: []cli.Flag{
				&cli.BoolFlag{
					Name:    "multi",
					Usage:   "Enable multi-user mode with text editor",
					Aliases: []string{"m"},
				},
				&cli.IntFlag{
					Name:    "batch-size",
					Usage:   "Number of users to process in each batch",
					Value:   100,
					Aliases: []string{"b"},
				},
			},
			Action: handleDeleteUsers(deps),
		},
		{
			Name:      "delete-groups",
			Usage:     "Delete group(s) and all associated data from the database",
			ArgsUsage: "[GROUP_ID]",
			Description: `Delete specific group(s) from the system:
  - GROUP_ID: Single Roblox group ID (required in single mode)
  - --multi: Enable multi-group mode with text editor
  - --batch-size: Number of groups to process in each batch (default: 100)

Single group mode:
  db delete-groups 12345

Multi-group mode (opens text editor):
  db delete-groups --multi

WARNING: This permanently deletes groups and all their associated data!`,
			Flags: []cli.Flag{
				&cli.BoolFlag{
					Name:    "multi",
					Usage:   "Enable multi-group mode with text editor",
					Aliases: []string{"m"},
				},
				&cli.IntFlag{
					Name:    "batch-size",
					Usage:   "Number of groups to process in each batch",
					Value:   100,
					Aliases: []string{"b"},
				},
			},
			Action: handleDeleteGroups(deps),
		},
	}
}

// handleDeleteUsers handles the 'delete-users' command.
func handleDeleteUsers(deps *CLIDependencies) cli.ActionFunc {
	return func(ctx context.Context, c *cli.Command) error {
		multiMode := c.Bool("multi")
		batchSize := max(c.Int("batch-size"), 10)

		// Get user IDs based on mode
		var userIDs []int64

		if multiMode {
			// Multi-user mode with text editor
			if c.Args().Len() > 0 {
				return ErrUserIDInMultiModeDeletion
			}

			var err error

			userIDs, err = getUserIDsFromEditorForDeletion(ctx)
			if err != nil {
				return err
			}

			if len(userIDs) == 0 {
				fmt.Println("No valid user IDs found.")
				return nil
			}

			fmt.Printf("Found %d user IDs to delete.\n\n", len(userIDs))
		} else {
			// Single user mode
			if c.Args().Len() != 1 {
				return ErrUserIDRequiredForDeletion
			}

			userIDStr := c.Args().First()

			userID, err := strconv.ParseInt(userIDStr, 10, 64)
			if err != nil {
				return fmt.Errorf("%w: %q", ErrInvalidUserIDForDeletion, userIDStr)
			}

			userIDs = []int64{userID}
		}

		deps.Logger.Info("Starting user deletion process",
			zap.Int("userCount", len(userIDs)),
			zap.Int("batchSize", batchSize))

		// Ask for confirmation
		log.Printf("WARNING: This will permanently delete %d user(s) and all associated data!", len(userIDs))
		log.Printf("Are you sure you want to continue? (y/N)")

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

		for i := 0; i < len(userIDs); i += batchSize {
			end := min(i+batchSize, len(userIDs))
			batchIDs := userIDs[i:end]
			batchCount := len(batchIDs)

			deps.Logger.Info("Processing deletion batch",
				zap.Int("batch", i/batchSize+1),
				zap.Int("size", batchCount),
				zap.Int("processed", totalProcessed),
				zap.Int("remaining", len(userIDs)-totalProcessed))

			// Delete from PostgreSQL database
			deleted, err := deps.DB.Service().User().DeleteUsers(ctx, batchIDs)
			if err != nil {
				return fmt.Errorf("failed to delete users from database in batch %d: %w", i/batchSize+1, err)
			}

			totalDeleted += deleted
			totalProcessed += batchCount

			// Remove from Cloudflare D1 in batch
			if err := deps.CFClient.UserFlags.RemoveBatch(ctx, batchIDs); err != nil {
				deps.Logger.Warn("Failed to remove users from Cloudflare D1",
					zap.Error(err),
					zap.Int64s("batchIDs", batchIDs))
			}

			deps.Logger.Info("Batch processed successfully",
				zap.Int("batch", i/batchSize+1),
				zap.Int("processed", batchCount),
				zap.Int64("deleted_rows", deleted))

			log.Printf("Batch %d/%d: Deleted %d users",
				i/batchSize+1, (len(userIDs)+batchSize-1)/batchSize, batchCount)

			// Add a small delay between batches to reduce system load
			if end < len(userIDs) {
				time.Sleep(100 * time.Millisecond)
			}
		}

		deps.Logger.Info("Successfully deleted all users",
			zap.Int("total_users", len(userIDs)),
			zap.Int64("total_deleted_rows", totalDeleted))

		log.Printf("\n=== Deletion Complete ===")
		log.Printf("Deleted %d users", len(userIDs))
		log.Printf("Total database rows affected: %d", totalDeleted)

		return nil
	}
}

// handleDeleteGroups handles the 'delete-groups' command.
func handleDeleteGroups(deps *CLIDependencies) cli.ActionFunc {
	return func(ctx context.Context, c *cli.Command) error {
		multiMode := c.Bool("multi")
		batchSize := max(c.Int("batch-size"), 10)

		// Get group IDs based on mode
		var groupIDs []int64

		if multiMode {
			// Multi-group mode with text editor
			if c.Args().Len() > 0 {
				return ErrGroupIDInMultiModeDeletion
			}

			var err error

			groupIDs, err = getGroupIDsFromEditorForDeletion(ctx)
			if err != nil {
				return err
			}

			if len(groupIDs) == 0 {
				fmt.Println("No valid group IDs found.")
				return nil
			}

			fmt.Printf("Found %d group IDs to delete.\n\n", len(groupIDs))
		} else {
			// Single group mode
			if c.Args().Len() != 1 {
				return ErrGroupIDRequiredForDeletion
			}

			groupIDStr := c.Args().First()

			groupID, err := strconv.ParseInt(groupIDStr, 10, 64)
			if err != nil {
				return fmt.Errorf("%w: %q", ErrInvalidGroupIDForDeletion, groupIDStr)
			}

			groupIDs = []int64{groupID}
		}

		deps.Logger.Info("Starting group deletion process",
			zap.Int("groupCount", len(groupIDs)),
			zap.Int("batchSize", batchSize))

		// Ask for confirmation
		log.Printf("WARNING: This will permanently delete %d group(s) and all associated data!", len(groupIDs))
		log.Printf("Are you sure you want to continue? (y/N)")

		var response string

		_, _ = fmt.Scanln(&response)

		if response != "y" && response != "Y" {
			deps.Logger.Info("Operation cancelled")
			return nil
		}

		// Process deletions in batches
		var (
			totalProcessed int
			totalDeleted   int
		)

		for i := 0; i < len(groupIDs); i += batchSize {
			end := min(i+batchSize, len(groupIDs))
			batchIDs := groupIDs[i:end]
			batchCount := len(batchIDs)

			deps.Logger.Info("Processing deletion batch",
				zap.Int("batch", i/batchSize+1),
				zap.Int("size", batchCount),
				zap.Int("processed", totalProcessed),
				zap.Int("remaining", len(groupIDs)-totalProcessed))

			// Delete groups from the system
			deleted, err := deps.DB.Service().Group().DeleteGroups(ctx, batchIDs)
			if err != nil {
				deps.Logger.Error("Failed to delete batch",
					zap.Int("batchSize", len(batchIDs)),
					zap.Error(err))

				log.Printf("Error deleting batch of %d groups: %v", len(batchIDs), err)
			} else {
				totalDeleted += int(deleted)

				// Remove from Cloudflare D1
				for _, groupID := range batchIDs {
					if err := deps.CFClient.GroupFlags.Remove(ctx, groupID); err != nil {
						deps.Logger.Warn("Failed to remove group from Cloudflare D1",
							zap.Error(err),
							zap.Int64("groupID", groupID))
					}
				}
			}

			totalProcessed += batchCount

			deps.Logger.Info("Batch processed successfully",
				zap.Int("batch", i/batchSize+1),
				zap.Int("processed", batchCount))

			log.Printf("Batch %d/%d: Processed %d groups",
				i/batchSize+1, (len(groupIDs)+batchSize-1)/batchSize, batchCount)

			// Add a small delay between batches to reduce system load
			if end < len(groupIDs) {
				time.Sleep(100 * time.Millisecond)
			}
		}

		deps.Logger.Info("Successfully deleted groups",
			zap.Int("total_groups_requested", len(groupIDs)),
			zap.Int("total_groups_deleted", totalDeleted))

		log.Printf("\n=== Deletion Complete ===")
		log.Printf("Deleted %d groups", totalDeleted)

		return nil
	}
}

// getUserIDsFromEditorForDeletion opens a text editor for the user to input multiple user IDs for deletion.
func getUserIDsFromEditorForDeletion(ctx context.Context) ([]int64, error) {
	instructions := `# Rotector User Deletion
#
# Instructions:
# - Add one Roblox user ID per line
# - Lines starting with # are comments and will be ignored
# - Empty lines will be ignored
#
# WARNING: This will permanently delete these users and all associated data!
#
# Example:
# 1234567890
# 9876543210
#
# Add user IDs to delete below:

`

	// Create temporary file with instructions
	tempFile, err := createTempFile("rotector_user_deletion", instructions)
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

// getGroupIDsFromEditorForDeletion opens a text editor for the user to input multiple group IDs for deletion.
func getGroupIDsFromEditorForDeletion(ctx context.Context) ([]int64, error) {
	instructions := `# Rotector Group Deletion
#
# Instructions:
# - Add one Roblox group ID per line
# - Lines starting with # are comments and will be ignored
# - Empty lines will be ignored
#
# WARNING: This will permanently delete these groups and all associated data!
#
# Example:
# 1234567890
# 9876543210
#
# Add group IDs to delete below:

`

	// Create temporary file with instructions
	tempFile, err := createTempFile("rotector_group_deletion", instructions)
	if err != nil {
		return nil, fmt.Errorf("failed to create temporary file: %w", err)
	}
	defer os.Remove(tempFile)

	fmt.Printf("Created temporary file: %s\n", tempFile)
	fmt.Printf("Please add group IDs to the file (one per line).\n")
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
	groupIDs, err := readIDsFromFile(tempFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read group IDs: %w", err)
	}

	return groupIDs, nil
}
