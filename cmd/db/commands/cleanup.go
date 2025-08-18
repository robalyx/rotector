package commands

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/robalyx/rotector/internal/bot/utils"
	"github.com/robalyx/rotector/internal/database/types/enum"
	"github.com/urfave/cli/v3"
	"go.uber.org/zap"
)

// CleanupCommands returns all cleanup-related commands.
func CleanupCommands(deps *CLIDependencies) []*cli.Command {
	return []*cli.Command{
		{
			Name:      "clear-reason",
			Usage:     "Clear flagged users with only a specific reason type and/or confidence threshold",
			ArgsUsage: "REASON",
			Description: `Clear flagged users based on specified criteria:
  - REASON: The reason type to filter by (required)
  - --confidence: Optional confidence threshold (users with confidence below this value will be deleted)
  - --batch-size: Number of users to process in each batch

Examples:
  db clear-reason OUTFIT                    # Clear users with only OUTFIT reason
  db clear-reason OUTFIT --confidence 0.85  # Clear users with only OUTFIT reason and confidence < 0.85
  db clear-reason OUTFIT -c 0.85 -b 1000   # Same as above with batch size of 1000`,
			Flags: []cli.Flag{
				&cli.IntFlag{
					Name:    "batch-size",
					Usage:   "Number of users to process in each batch",
					Value:   5000,
					Aliases: []string{"b"},
				},
				&cli.Float64Flag{
					Name:    "confidence",
					Usage:   "Delete users with confidence below this threshold",
					Value:   1.0,
					Aliases: []string{"c"},
				},
			},
			Action: handleClearReason(deps),
		},
		{
			Name:      "delete-after-time",
			Usage:     "Delete flagged users that have been updated after a specific time",
			ArgsUsage: "TIME",
			Description: `Delete flagged users that have been updated after the specified time.
			
TIME can be in various formats:
  - "2006-01-02" (date only, assumes 00:00:00 UTC)
  - "2006-01-02 15:04:05" (datetime, assumes UTC)
  - "2006-01-02 15:04:05 UTC" (datetime with UTC timezone)
  - "2006-01-02 15:04:05 America/New_York" (datetime with timezone)
  - "2006-01-02T15:04:05Z" (RFC3339 format)
  - "2006-01-02T15:04:05-07:00" (RFC3339 with timezone offset)

Examples:
  db delete-after-time "2024-01-01"
  db delete-after-time "2024-01-01 12:00:00"
  db delete-after-time "2024-01-01T12:00:00Z"
  db delete-after-time "2024-01-01T12:00:00+08:00"
  db delete-after-time "2024-01-01" --reason OUTFIT        # Only users with OUTFIT reason
  db delete-after-time "2024-01-01" -r FRIEND              # Only users with FRIEND reason
  db delete-after-time "2024-01-01" -r OUTFIT,FRIEND       # Only users with both OUTFIT and FRIEND reasons
  db delete-after-time "2024-01-01" -r PROFILE,GROUP,CHAT  # Only users with exactly these three reasons

Note: When using timezone names with spaces (like "Asia/Singapore"), you may need 
to escape quotes depending on your shell:
  just run-db delete-after-time '"2024-01-01 12:00:00 Asia/Singapore"'
  
For reliable cross-platform usage, prefer RFC3339 format with timezone offsets.`,
			Flags: []cli.Flag{
				&cli.IntFlag{
					Name:    "batch-size",
					Usage:   "Number of users to process in each batch",
					Value:   5000,
					Aliases: []string{"b"},
				},
				&cli.StringFlag{
					Name:    "reason",
					Usage:   "Filter by users with only these specific reason types",
					Aliases: []string{"r"},
				},
			},
			Action: handleDeleteAfterTime(deps),
		},
	}
}

// handleClearReason handles the 'clear-reason' command.
func handleClearReason(deps *CLIDependencies) cli.ActionFunc {
	return func(ctx context.Context, c *cli.Command) error {
		if c.Args().Len() != 1 {
			return ErrReasonRequired
		}

		// Get batch size and confidence threshold from flags
		batchSize := max(c.Int("batch-size"), 5000)
		confidenceThreshold := c.Float64("confidence")

		// Get reason type from argument
		reasonStr := strings.ToUpper(c.Args().First())

		reasonType, err := enum.UserReasonTypeString(reasonStr)
		if err != nil {
			return fmt.Errorf("invalid reason type %q: %w", reasonStr, err)
		}

		// Get users with only this reason and below confidence threshold
		users, err := deps.DB.Model().User().GetFlaggedUsersWithOnlyReason(ctx, reasonType, confidenceThreshold)
		if err != nil {
			return fmt.Errorf("failed to get users: %w", err)
		}

		if len(users) == 0 {
			deps.Logger.Info("No users found matching the criteria",
				zap.String("reason", reasonType.String()),
				zap.Float64("confidenceThreshold", confidenceThreshold))

			return nil
		}

		// Ask for confirmation
		deps.Logger.Info("Found users to clear",
			zap.Int("count", len(users)),
			zap.String("reason", reasonType.String()),
			zap.Float64("confidenceThreshold", confidenceThreshold))

		log.Printf("Are you sure you want to delete these %d users (with confidence < %.2f) in batches of %d? (y/N)",
			len(users), confidenceThreshold, batchSize)

		var response string

		_, _ = fmt.Scanln(&response)

		if response != "y" && response != "Y" {
			deps.Logger.Info("Operation cancelled")
			return nil
		}

		// Create user ID slices
		userIDs := make([]int64, len(users))
		for i, user := range users {
			userIDs[i] = user.ID
		}

		// Process in batches
		var (
			totalAffected  int64
			totalProcessed int
		)

		for i := 0; i < len(userIDs); i += batchSize {
			end := min(i+batchSize, len(userIDs))

			batchIDs := userIDs[i:end]
			batchCount := len(batchIDs)

			deps.Logger.Info("Processing batch",
				zap.Int("batch", i/batchSize+1),
				zap.Int("size", batchCount),
				zap.Int("processed", totalProcessed),
				zap.Int("remaining", len(userIDs)-totalProcessed))

			affected, err := deps.DB.Service().User().DeleteUsers(ctx, batchIDs)
			if err != nil {
				return fmt.Errorf("failed to delete users in batch %d: %w", i/batchSize+1, err)
			}

			totalAffected += affected
			totalProcessed += batchCount

			deps.Logger.Info("Batch processed successfully",
				zap.Int("batch", i/batchSize+1),
				zap.Int("processed", batchCount),
				zap.Int64("affected_rows", affected))

			// Add a small delay between batches to reduce database load
			if end < len(userIDs) {
				time.Sleep(100 * time.Millisecond)
			}
		}

		deps.Logger.Info("Successfully cleared all users",
			zap.Int("total_count", len(users)),
			zap.Int64("total_affected_rows", totalAffected))

		return nil
	}
}

// handleDeleteAfterTime handles the 'delete-after-time' command.
func handleDeleteAfterTime(deps *CLIDependencies) cli.ActionFunc {
	return func(ctx context.Context, c *cli.Command) error {
		if c.Args().Len() != 1 {
			return ErrTimeRequired
		}

		// Get batch size from flag
		batchSize := max(c.Int("batch-size"), 1000)

		// Parse the time string with timezone support
		timeStr := c.Args().First()

		cutoffTime, err := utils.ParseTimeWithTimezone(timeStr)
		if err != nil {
			return fmt.Errorf("failed to parse time %q: %w", timeStr, err)
		}

		deps.Logger.Info("Parsed cutoff time",
			zap.String("input", timeStr),
			zap.Time("cutoffTime", cutoffTime),
			zap.String("timezone", cutoffTime.Location().String()))

		// Check if reason filtering is requested
		reasonStr := c.String("reason")

		var reasonTypes []enum.UserReasonType

		if reasonStr != "" {
			// Parse comma-separated reason types
			reasonStrs := strings.Split(reasonStr, ",")
			reasonTypes = make([]enum.UserReasonType, len(reasonStrs))

			for i, rs := range reasonStrs {
				rs = strings.TrimSpace(strings.ToUpper(rs))

				rt, err := enum.UserReasonTypeString(rs)
				if err != nil {
					return fmt.Errorf("invalid reason type %q: %w", rs, err)
				}

				reasonTypes[i] = rt
			}

			reasonTypeStrs := make([]string, len(reasonTypes))
			for i, rt := range reasonTypes {
				reasonTypeStrs[i] = rt.String()
			}

			deps.Logger.Info("Filtering by reason types",
				zap.Strings("reasons", reasonTypeStrs))
		}

		// Get users updated after the cutoff time
		users, err := deps.DB.Model().User().GetUsersUpdatedAfter(ctx, cutoffTime, reasonTypes)
		if err != nil {
			return fmt.Errorf("failed to get users: %w", err)
		}

		if len(users) == 0 {
			deps.Logger.Info("No flagged users found updated after the specified time",
				zap.Time("cutoffTime", cutoffTime))

			return nil
		}

		// Ask for confirmation
		if reasonStr != "" {
			reasonTypeStrs := make([]string, len(reasonTypes))
			for i, rt := range reasonTypes {
				reasonTypeStrs[i] = rt.String()
			}

			reasonsDisplay := strings.Join(reasonTypeStrs, ", ")

			deps.Logger.Info("Found flagged users with only the specified reasons to delete",
				zap.Int("count", len(users)),
				zap.Time("cutoffTime", cutoffTime),
				zap.String("timezone", cutoffTime.Location().String()),
				zap.Strings("reasons", reasonTypeStrs))

			log.Printf("Are you sure you want to delete these %d flagged users "+
				"(with only these %d reasons: %s) updated after %s in batches of %d? (y/N)",
				len(users), len(reasonTypes), reasonsDisplay, cutoffTime.Format("2006-01-02 15:04:05 MST"), batchSize)
		} else {
			deps.Logger.Info("Found flagged users to delete",
				zap.Int("count", len(users)),
				zap.Time("cutoffTime", cutoffTime),
				zap.String("timezone", cutoffTime.Location().String()))

			log.Printf("Are you sure you want to delete these %d flagged users updated after %s in batches of %d? (y/N)",
				len(users), cutoffTime.Format("2006-01-02 15:04:05 MST"), batchSize)
		}

		var response string

		_, _ = fmt.Scanln(&response)

		if response != "y" && response != "Y" {
			deps.Logger.Info("Operation cancelled")
			return nil
		}

		// Create user ID slices
		userIDs := make([]int64, len(users))
		for i, user := range users {
			userIDs[i] = user.ID
		}

		// Process in batches
		var (
			totalAffected  int64
			totalProcessed int
		)

		for i := 0; i < len(userIDs); i += batchSize {
			end := min(i+batchSize, len(userIDs))

			batchIDs := userIDs[i:end]
			batchCount := len(batchIDs)

			deps.Logger.Info("Processing batch",
				zap.Int("batch", i/batchSize+1),
				zap.Int("size", batchCount),
				zap.Int("processed", totalProcessed),
				zap.Int("remaining", len(userIDs)-totalProcessed))

			affected, err := deps.DB.Service().User().DeleteUsers(ctx, batchIDs)
			if err != nil {
				return fmt.Errorf("failed to delete users in batch %d: %w", i/batchSize+1, err)
			}

			totalAffected += affected
			totalProcessed += batchCount

			deps.Logger.Info("Batch processed successfully",
				zap.Int("batch", i/batchSize+1),
				zap.Int("processed", batchCount),
				zap.Int64("affected_rows", affected))

			// Add a small delay between batches to reduce database load
			if end < len(userIDs) {
				time.Sleep(100 * time.Millisecond)
			}
		}

		deps.Logger.Info("Successfully deleted all flagged users",
			zap.Int("total_count", len(users)),
			zap.Int64("total_affected_rows", totalAffected),
			zap.Time("cutoffTime", cutoffTime))

		return nil
	}
}
