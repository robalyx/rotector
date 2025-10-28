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

	"github.com/jaxron/roapi.go/pkg/api/resources/groups"
	"github.com/robalyx/rotector/internal/database/types/enum"
	"github.com/urfave/cli/v3"
	"go.uber.org/zap"
)

var (
	ErrGroupIDRequired    = errors.New("GROUP_ID argument required in single mode")
	ErrInvalidGroupID     = errors.New("invalid group ID: must be a number")
	ErrGroupIDInMultiMode = errors.New("do not provide GROUP_ID argument in multi mode")
)

// GroupCleanupCommands returns all group cleanup-related commands.
func GroupCleanupCommands(deps *CLIDependencies) []*cli.Command {
	return []*cli.Command{
		{
			Name:      "clean-group-members",
			Usage:     "Remove inappropriate members from group(s)",
			ArgsUsage: "[GROUP_ID]",
			Description: `Clean members from specific group(s):
  - GROUP_ID: Single Roblox group ID (required in single mode)
  - --multi: Enable multi-group mode with text editor
  - --status: Filter by status - 'flagged', 'confirmed', or 'both' (default: 'both')
  - --batch-size: Number of users to process in each batch (default: 100)

Single group mode:
  db clean-group-members 12345                           # Clean both flagged and confirmed members
  db clean-group-members 12345 --status flagged         # Clean only flagged members
  db clean-group-members 12345 --status confirmed       # Clean only confirmed members

Multi-group mode (opens text editor):
  db clean-group-members --multi                         # Clean members from multiple groups
  db clean-group-members --multi --status flagged       # Clean only flagged members from multiple groups`,
			Flags: []cli.Flag{
				&cli.BoolFlag{
					Name:    "multi",
					Usage:   "Enable multi-group mode with text editor",
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
			Action: handleCleanGroupMembers(deps),
		},
	}
}

// handleCleanGroupMembers handles the 'clean-group-members' command.
func handleCleanGroupMembers(deps *CLIDependencies) cli.ActionFunc {
	return func(ctx context.Context, c *cli.Command) error {
		multiMode := c.Bool("multi")
		statusFilter := c.String("status")
		batchSize := max(c.Int("batch-size"), 10)

		// Parse status filter
		statuses, err := parseStatusFilter(statusFilter)
		if err != nil {
			return err
		}

		// Get group IDs based on mode
		var groupIDs []int64

		if multiMode {
			// Multi-group mode with text editor
			if c.Args().Len() > 0 {
				return ErrGroupIDInMultiMode
			}

			groupIDs, err = getGroupIDsFromEditor(ctx)
			if err != nil {
				return err
			}

			if len(groupIDs) == 0 {
				fmt.Println("No valid group IDs found.")
				return nil
			}

			fmt.Printf("Found %d group IDs to process.\n\n", len(groupIDs))
		} else {
			// Single group mode
			if c.Args().Len() != 1 {
				return ErrGroupIDRequired
			}

			groupIDStr := c.Args().First()

			groupID, err := strconv.ParseInt(groupIDStr, 10, 64)
			if err != nil {
				return fmt.Errorf("%w: %q", ErrInvalidGroupID, groupIDStr)
			}

			groupIDs = []int64{groupID}
		}

		deps.Logger.Info("Starting group member cleanup process",
			zap.Int("groupCount", len(groupIDs)),
			zap.Int("batchSize", batchSize))

		// Process each group
		var totalInappropriateMembers int

		for i, groupID := range groupIDs {
			if len(groupIDs) > 1 {
				fmt.Printf("\n[%d/%d] Processing group %d...\n", i+1, len(groupIDs), groupID)
			}

			inappropriateCount, err := processGroupMembers(ctx, deps, groupID, statuses, batchSize)
			if err != nil {
				deps.Logger.Error("Failed to process group",
					zap.Int64("groupID", groupID),
					zap.Error(err))

				log.Printf("Error processing group %d: %v", groupID, err)

				continue
			}

			totalInappropriateMembers += inappropriateCount
		}

		// Summary
		if len(groupIDs) > 1 {
			fmt.Printf("\n=== Summary ===\n")
			fmt.Printf("Processed %d groups\n", len(groupIDs))
			fmt.Printf("Total inappropriate members removed: %d\n", totalInappropriateMembers)
		}

		deps.Logger.Info("Group cleanup completed",
			zap.Int("totalGroups", len(groupIDs)),
			zap.Int("totalInappropriateMembers", totalInappropriateMembers))

		return nil
	}
}

// processGroupMembers processes a single group's members for cleanup.
func processGroupMembers(
	ctx context.Context, deps *CLIDependencies,
	groupID int64, statuses []enum.UserType, batchSize int,
) (int, error) {
	// Fetch all group members
	deps.Logger.Info("Fetching all group members", zap.Int64("groupID", groupID))

	memberIDs, err := fetchGroupMembers(ctx, deps, groupID)
	if err != nil {
		return 0, fmt.Errorf("failed to fetch group members: %w", err)
	}

	if len(memberIDs) == 0 {
		deps.Logger.Info("No members found for group", zap.Int64("groupID", groupID))
		log.Printf("Group %d has no members.", groupID)

		return 0, nil
	}

	deps.Logger.Info("Fetched member IDs",
		zap.Int64("groupID", groupID),
		zap.Int("memberCount", len(memberIDs)))

	// Find which members match the status filter
	inappropriateMembers, err := getUsersByStatuses(ctx, deps.DB, memberIDs, statuses)
	if err != nil {
		return 0, fmt.Errorf("failed to check member status: %w", err)
	}

	if len(inappropriateMembers) == 0 {
		deps.Logger.Info("No inappropriate members found in group",
			zap.Int64("groupID", groupID),
			zap.Int("totalMembers", len(memberIDs)))

		log.Printf("Group %d: No inappropriate members found (out of %d total members).",
			groupID, len(memberIDs))

		return 0, nil
	}

	// Extract inappropriate member IDs
	inappropriateMemberIDs := make([]int64, len(inappropriateMembers))
	for i, user := range inappropriateMembers {
		inappropriateMemberIDs[i] = user.ID
	}

	// Log findings
	deps.Logger.Info("Found inappropriate members to delete",
		zap.Int64("groupID", groupID),
		zap.Int("totalMembers", len(memberIDs)),
		zap.Int("inappropriateMembers", len(inappropriateMembers)))

	log.Printf("Group %d: Found %d inappropriate members (out of %d total members)",
		groupID, len(inappropriateMembers), len(memberIDs))

	// Process deletions in batches
	totalDeleted, err := processDeletions(ctx, deps, inappropriateMemberIDs, batchSize)
	if err != nil {
		return 0, fmt.Errorf("failed to process deletions: %w", err)
	}

	deps.Logger.Info("Successfully deleted inappropriate members",
		zap.Int64("groupID", groupID),
		zap.Int("inappropriate_members", len(inappropriateMembers)),
		zap.Int64("deleted_rows", totalDeleted))

	log.Printf("Group %d: Successfully deleted %d inappropriate members",
		groupID, len(inappropriateMembers))

	return len(inappropriateMembers), nil
}

// getGroupIDsFromEditor opens a text editor for the user to input multiple group IDs.
func getGroupIDsFromEditor(ctx context.Context) ([]int64, error) {
	instructions := `# Rotector Group Member Cleanup
#
# Instructions:
# - Add one Roblox group ID per line
# - Lines starting with # are comments and will be ignored
# - Empty lines will be ignored
#
# Example:
# 1234567890
# 9876543210
#
# Add group IDs below:

`

	// Create temporary file with instructions
	tempFile, err := createTempFile("rotector_group_cleanup", instructions)
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

// processDeletions handles batch deletion of inappropriate users.
func processDeletions(
	ctx context.Context, deps *CLIDependencies,
	inappropriateUserIDs []int64, batchSize int,
) (int64, error) {
	var (
		totalDeleted   int64
		totalProcessed int
	)

	for i := 0; i < len(inappropriateUserIDs); i += batchSize {
		end := min(i+batchSize, len(inappropriateUserIDs))
		batchIDs := inappropriateUserIDs[i:end]
		batchCount := len(batchIDs)

		deps.Logger.Info("Processing deletion batch",
			zap.Int("batch", i/batchSize+1),
			zap.Int("size", batchCount),
			zap.Int("processed", totalProcessed),
			zap.Int("remaining", len(inappropriateUserIDs)-totalProcessed))

		// Delete from PostgreSQL database
		deleted, err := deps.DB.Service().User().DeleteUsers(ctx, batchIDs)
		if err != nil {
			return 0, fmt.Errorf("failed to delete users from database in batch %d: %w", i/batchSize+1, err)
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

		// Add a small delay between batches to reduce system load
		if end < len(inappropriateUserIDs) {
			time.Sleep(100 * time.Millisecond)
		}
	}

	return totalDeleted, nil
}

// fetchGroupMembers fetches all member IDs from a group using pagination.
func fetchGroupMembers(ctx context.Context, deps *CLIDependencies, groupID int64) ([]int64, error) {
	var allMemberIDs []int64

	cursor := ""

	for {
		// Build request with pagination
		builder := groups.NewGroupUsersBuilder(groupID).WithLimit(100)
		if cursor != "" {
			builder = builder.WithCursor(cursor)
		}

		// Fetch group members
		groupUsers, err := deps.RoAPI.Groups().GetGroupUsers(ctx, builder.Build())
		if err != nil {
			return nil, fmt.Errorf("failed to fetch group users: %w", err)
		}

		// Extract user IDs from the response
		for _, member := range groupUsers.Data {
			allMemberIDs = append(allMemberIDs, member.User.UserID)
		}

		// Check if there are more pages
		if groupUsers.NextPageCursor == nil || *groupUsers.NextPageCursor == "" {
			break
		}

		cursor = *groupUsers.NextPageCursor
	}

	return allMemberIDs, nil
}
