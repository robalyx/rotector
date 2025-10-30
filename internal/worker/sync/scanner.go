package sync

import (
	"context"
	"errors"
	"time"

	"github.com/robalyx/rotector/internal/database/types"
	"github.com/robalyx/rotector/internal/discord"
	"github.com/robalyx/rotector/pkg/utils"
	"go.uber.org/zap"
)

// runMutualScanner continuously runs full scans for users.
func (w *Worker) runMutualScanner(ctx context.Context) {
	for {
		if utils.ContextGuardWithLog(ctx, w.logger, "Context cancelled, stopping mutual scanner") {
			return
		}

		before := time.Now().Add(-12 * time.Hour)

		userIDs, err := w.db.Model().Sync().GetUsersForFullScan(ctx, before, 100)
		if err != nil {
			w.logger.Error("Failed to get users for full scan", zap.Error(err))

			if utils.ContextSleep(ctx, 5*time.Second) == utils.SleepCancelled {
				w.logger.Info("Context cancelled during error wait, stopping mutual scanner")
				return
			}

			continue
		}

		for _, userID := range userIDs {
			if utils.ContextGuardWithLog(ctx, w.logger, "Context cancelled during user scan, stopping mutual scanner") {
				return
			}

			// Get all scanners to check in all accounts
			scanners := w.scannerPool.GetAll()
			if len(scanners) == 0 {
				w.logger.Error("No scanners available")
				break
			}

			// Track successful scans and collect connections from all scanners
			var (
				allConnections []*types.DiscordRobloxConnection
				username       string
			)

			successfulScans := 0
			visibilityErrors := 0

			for accountIndex, scanner := range scanners {
				if utils.ContextGuardWithLog(ctx, w.logger, "Context cancelled during account scan, stopping mutual scanner") {
					return
				}

				// Wait for rate limit using the specific account's rate limiter
				if accountIndex >= 0 && accountIndex < len(w.discordRateLimiters) {
					if err := w.discordRateLimiters[accountIndex].waitForNextSlot(ctx); err != nil {
						w.logger.Info("Context cancelled during rate limit wait, stopping mutual scanner",
							zap.Int("account_index", accountIndex))

						return
					}
				}

				// Perform scan on user
				scannedUsername, connections, err := scanner.PerformFullScan(ctx, userID, false)
				if err != nil {
					if errors.Is(err, discord.ErrUserNotVisible) {
						visibilityErrors++
					} else {
						w.logger.Error("Failed to perform full scan",
							zap.Error(err),
							zap.Uint64("userID", userID),
							zap.Int("account_index", accountIndex))
					}
				} else {
					successfulScans++

					if username == "" {
						username = scannedUsername
					}

					allConnections = append(allConnections, connections...)
				}
			}

			// Add verification connections once
			if successfulScans > 0 {
				verificationConns := w.verificationManager.FetchAllVerificationProfiles(ctx, userID)
				allConnections = append(allConnections, verificationConns...)

				// Process all connections once
				if len(allConnections) > 0 {
					if err := w.scannerPool.ProcessConnections(ctx, userID, allConnections); err != nil {
						w.logger.Error("Failed to process connections",
							zap.Error(err),
							zap.Uint64("userID", userID))
					}
				}
			}

			// Update scan timestamp
			switch {
			case successfulScans > 0:
				if err := w.db.Model().Sync().UpdateUserScanTimestamp(ctx, userID); err != nil {
					w.logger.Error("Failed to update user scan timestamp",
						zap.Error(err),
						zap.Uint64("userID", userID))
				} else {
					w.logger.Info("Completed full scan",
						zap.Uint64("userID", userID),
						zap.Int("successful_accounts", successfulScans),
						zap.Int("total_accounts", len(scanners)))
				}
			case visibilityErrors == len(scanners):
				if err := w.db.Model().Sync().UpdateUserScanTimestamp(ctx, userID); err != nil {
					w.logger.Error("Failed to update user scan timestamp",
						zap.Error(err),
						zap.Uint64("userID", userID))
				} else {
					w.logger.Info("User not visible to any scanner, skipping",
						zap.Uint64("userID", userID),
						zap.Int("total_accounts", len(scanners)))
				}
			default:
				w.logger.Error("All accounts failed to scan user, will retry next cycle",
					zap.Uint64("userID", userID),
					zap.Int("total_accounts", len(scanners)))
			}
		}

		if utils.ContextSleep(ctx, 5*time.Second) == utils.SleepCancelled {
			w.logger.Info("Context cancelled during batch wait, stopping mutual scanner")
			return
		}
	}
}
