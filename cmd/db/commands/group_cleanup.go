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
	"time"

	"github.com/jaxron/roapi.go/pkg/api/resources/groups"
	apiTypes "github.com/jaxron/roapi.go/pkg/api/types"
	"github.com/robalyx/rotector/internal/database"
	dbTypes "github.com/robalyx/rotector/internal/database/types"
	"github.com/robalyx/rotector/internal/database/types/enum"
	"github.com/robalyx/rotector/internal/setup"
	"github.com/robalyx/rotector/internal/setup/telemetry"
	"github.com/urfave/cli/v3"
	"go.uber.org/zap"
)

var (
	ErrGroupIDRequired = errors.New("GROUP_ID argument required")
	ErrInvalidGroupID  = errors.New("invalid group ID: must be a number")
	ErrInvalidInput    = errors.New("invalid input: must be a number")
	ErrInvalidChoice   = errors.New("invalid choice")
)

// GroupCleanupCommands returns all group cleanup-related commands.
func GroupCleanupCommands(deps *CLIDependencies) []*cli.Command {
	return []*cli.Command{
		{
			Name:      "clean-group-members",
			Usage:     "Remove inappropriate members from a group by role",
			ArgsUsage: "GROUP_ID",
			Description: `Clean group members for a specific group by role:
  - GROUP_ID: The Roblox group ID to process (required)
  - --batch-size: Number of users to process in each batch (default: 100)

The command will:
1. Display all group roles with member counts
2. Let you select a specific role or all members (0)
3. Remove flagged and confirmed users from the selection

Examples:
  db clean-group-members 12345                    # Interactive role selection for group 12345
  db clean-group-members 12345 --batch-size 50   # Clean with smaller batch size`,
			Flags: []cli.Flag{
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
		if c.Args().Len() != 1 {
			return ErrGroupIDRequired
		}

		// Parse group ID
		groupIDStr := c.Args().First()

		groupID, err := strconv.ParseInt(groupIDStr, 10, 64)
		if err != nil {
			return fmt.Errorf("%w: %q", ErrInvalidGroupID, groupIDStr)
		}

		// Get batch size from flag
		batchSize := max(c.Int("batch-size"), 10)

		deps.Logger.Info("Starting group member cleanup process",
			zap.Int64("groupID", groupID),
			zap.Int("batchSize", batchSize))

		// Initialize application
		app, err := setup.InitializeApp(ctx, telemetry.ServiceExport, "logs/cli", "db", "group-cleanup")
		if err != nil {
			return fmt.Errorf("failed to initialize app: %w", err)
		}

		// Fetch and display group roles
		deps.Logger.Info("Fetching group roles", zap.Int64("groupID", groupID))

		roles, err := fetchGroupRoles(ctx, app, groupID)
		if err != nil {
			return fmt.Errorf("failed to fetch group roles for group %d: %w", groupID, err)
		}

		if len(roles) == 0 {
			deps.Logger.Info("No roles found for group", zap.Int64("groupID", groupID))
			return nil
		}

		// Display roles and get user selection
		selectedRoleID, roleName, err := selectRole(roles)
		if err != nil {
			return fmt.Errorf("role selection failed: %w", err)
		}

		// Fetch members based on selection
		var memberIDs []int64

		if selectedRoleID == 0 {
			deps.Logger.Info("Fetching all group members", zap.Int64("groupID", groupID))

			memberIDs, err = fetchGroupMembers(ctx, app, groupID)
			if err != nil {
				return fmt.Errorf("failed to fetch group members: %w", err)
			}
		} else {
			deps.Logger.Info("Fetching role members",
				zap.Int64("groupID", groupID),
				zap.Int64("roleID", selectedRoleID),
				zap.String("roleName", roleName))

			memberIDs, err = fetchRoleMembers(ctx, app, groupID, selectedRoleID)
			if err != nil {
				return fmt.Errorf("failed to fetch role members: %w", err)
			}
		}

		if len(memberIDs) == 0 {
			log.Printf("No members found for selected role: %s", roleName)
			return nil
		}

		deps.Logger.Info("Fetched member IDs",
			zap.Int64("groupID", groupID),
			zap.String("selectedRole", roleName),
			zap.Int("memberCount", len(memberIDs)))

		// Find which members are flagged or confirmed in our system
		inappropriateMembers, err := getUsersByMultipleStatuses(ctx, app.DB, memberIDs,
			[]enum.UserType{enum.UserTypeFlagged, enum.UserTypeConfirmed})
		if err != nil {
			return fmt.Errorf("failed to check member status: %w", err)
		}

		if len(inappropriateMembers) == 0 {
			log.Printf("No inappropriate members found in role: %s (%d total members checked)",
				roleName, len(memberIDs))

			return nil
		}

		// Extract inappropriate member IDs
		inappropriateMemberIDs := make([]int64, len(inappropriateMembers))
		for i, user := range inappropriateMembers {
			inappropriateMemberIDs[i] = user.ID
		}

		// Ask for confirmation
		deps.Logger.Info("Found inappropriate members to delete",
			zap.Int64("groupID", groupID),
			zap.String("selectedRole", roleName),
			zap.Int("totalMembers", len(memberIDs)),
			zap.Int("inappropriateMembers", len(inappropriateMembers)))

		log.Printf("Found %d inappropriate members in role '%s' (out of %d total members)",
			len(inappropriateMembers), roleName, len(memberIDs))
		log.Printf("Are you sure you want to delete these %d inappropriate members in batches of %d? (y/N)",
			len(inappropriateMembers), batchSize)

		// Get user confirmation
		reader := bufio.NewReader(os.Stdin)

		response, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("failed to read input: %w", err)
		}

		response = strings.TrimSpace(response)
		if response != "y" && response != "Y" {
			deps.Logger.Info("Operation cancelled")
			return nil
		}

		// Process deletions in batches
		totalDeleted, err := processDeletions(ctx, app, deps, inappropriateMemberIDs, batchSize)
		if err != nil {
			return fmt.Errorf("failed to process deletions: %w", err)
		}

		deps.Logger.Info("Successfully deleted all inappropriate members",
			zap.Int64("groupID", groupID),
			zap.String("selectedRole", roleName),
			zap.Int("total_inappropriate_members", len(inappropriateMembers)),
			zap.Int64("total_deleted_rows", totalDeleted))

		log.Printf("Cleanup completed! Deleted %d inappropriate members from role '%s'",
			len(inappropriateMembers), roleName)

		return nil
	}
}

// fetchGroupRoles fetches all roles in a group.
func fetchGroupRoles(ctx context.Context, app *setup.App, groupID int64) ([]apiTypes.GroupRole, error) {
	rolesResponse, err := app.RoAPI.Groups().GetGroupRoles(ctx, groupID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch group roles: %w", err)
	}

	return rolesResponse.Roles, nil
}

// selectRole displays roles and prompts user to select one.
func selectRole(roles []apiTypes.GroupRole) (int64, string, error) {
	fmt.Println("\n=== Group Roles ===")
	fmt.Printf("0. All group members\n")

	for i, role := range roles {
		fmt.Printf("%d. %s (Rank: %d, Members: %d)\n",
			i+1, role.Name, role.Rank, role.MemberCount)
	}

	fmt.Print("\nEnter the number of the role you want to clean (0 for all members): ")

	reader := bufio.NewReader(os.Stdin)

	input, err := reader.ReadString('\n')
	if err != nil {
		return 0, "", fmt.Errorf("failed to read input: %w", err)
	}

	choice, err := strconv.Atoi(strings.TrimSpace(input))
	if err != nil {
		return 0, "", ErrInvalidInput
	}

	if choice == 0 {
		return 0, "All group members", nil
	}

	if choice < 1 || choice > len(roles) {
		return 0, "", fmt.Errorf("%w: must be between 0 and %d", ErrInvalidChoice, len(roles))
	}

	selectedRole := roles[choice-1]

	return selectedRole.ID, selectedRole.Name, nil
}

// processDeletions handles batch deletion of inappropriate users.
func processDeletions(
	ctx context.Context, app *setup.App, deps *CLIDependencies,
	inappropriateMemberIDs []int64, batchSize int,
) (int64, error) {
	var (
		totalDeleted   int64
		totalProcessed int
	)

	for i := 0; i < len(inappropriateMemberIDs); i += batchSize {
		end := min(i+batchSize, len(inappropriateMemberIDs))
		batchIDs := inappropriateMemberIDs[i:end]
		batchCount := len(batchIDs)

		deps.Logger.Info("Processing deletion batch",
			zap.Int("batch", i/batchSize+1),
			zap.Int("size", batchCount),
			zap.Int("processed", totalProcessed),
			zap.Int("remaining", len(inappropriateMemberIDs)-totalProcessed))

		// Delete from PostgreSQL database
		deleted, err := app.DB.Service().User().DeleteUsers(ctx, batchIDs)
		if err != nil {
			return 0, fmt.Errorf("failed to delete users from database in batch %d: %w", i/batchSize+1, err)
		}

		totalDeleted += deleted
		totalProcessed += batchCount

		// Remove from Cloudflare D1 in batch
		if err := app.CFClient.UserFlags.RemoveBatch(ctx, batchIDs); err != nil {
			app.Logger.Warn("Failed to remove users from Cloudflare D1",
				zap.Error(err),
				zap.Int64s("batchIDs", batchIDs))
		}

		deps.Logger.Info("Batch processed successfully",
			zap.Int("batch", i/batchSize+1),
			zap.Int("processed", batchCount),
			zap.Int64("deleted_rows", deleted))

		// Add a small delay between batches to reduce system load
		if end < len(inappropriateMemberIDs) {
			time.Sleep(100 * time.Millisecond)
		}
	}

	return totalDeleted, nil
}

// fetchRoleMembers fetches all member IDs from a specific role using pagination.
func fetchRoleMembers(ctx context.Context, app *setup.App, groupID, roleID int64) ([]int64, error) {
	var allMemberIDs []int64

	cursor := ""

	for {
		// Build request with pagination
		builder := groups.NewRoleUsersBuilder(groupID, roleID).WithLimit(100)
		if cursor != "" {
			builder = builder.WithCursor(cursor)
		}

		// Fetch role members
		roleUsers, err := app.RoAPI.Groups().GetRoleUsers(ctx, builder.Build())
		if err != nil {
			return nil, fmt.Errorf("failed to fetch role users: %w", err)
		}

		// Extract user IDs from the response
		for _, member := range roleUsers.Data {
			allMemberIDs = append(allMemberIDs, member.UserID)
		}

		// Check if there are more pages
		if roleUsers.NextPageCursor == nil || *roleUsers.NextPageCursor == "" {
			break
		}

		cursor = *roleUsers.NextPageCursor
	}

	return allMemberIDs, nil
}

// fetchGroupMembers fetches all member IDs from a group using pagination.
func fetchGroupMembers(ctx context.Context, app *setup.App, groupID int64) ([]int64, error) {
	var allMemberIDs []int64

	cursor := ""

	for {
		// Build request with pagination
		builder := groups.NewGroupUsersBuilder(groupID).WithLimit(100)
		if cursor != "" {
			builder = builder.WithCursor(cursor)
		}

		// Fetch group members
		groupUsers, err := app.RoAPI.Groups().GetGroupUsers(ctx, builder.Build())
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

// getUsersByMultipleStatuses gets users by their IDs filtered by multiple statuses.
func getUsersByMultipleStatuses(
	ctx context.Context, db database.Client, userIDs []int64, statuses []enum.UserType,
) ([]*dbTypes.User, error) {
	if len(userIDs) == 0 {
		return []*dbTypes.User{}, nil
	}

	// Get users by IDs with basic fields
	userMap, err := db.Model().User().GetUsersByIDs(ctx, userIDs, dbTypes.UserFieldBasic)
	if err != nil {
		return nil, fmt.Errorf("failed to get users by IDs: %w", err)
	}

	// Create status map for efficient lookup
	statusSet := make(map[enum.UserType]bool)
	for _, status := range statuses {
		statusSet[status] = true
	}

	// Filter by status and convert to simple User slice
	var result []*dbTypes.User

	for _, reviewUser := range userMap {
		if reviewUser.User != nil && statusSet[reviewUser.Status] {
			result = append(result, reviewUser.User)
		}
	}

	return result, nil
}
