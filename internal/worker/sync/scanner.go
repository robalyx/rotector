package sync

import (
	"context"
	"errors"
	"strings"
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
			temporaryErrors := 0

			var lastTemporaryError error

			for accountIndex, scanner := range scanners {
				if utils.ContextGuardWithLog(ctx, w.logger, "Context cancelled during account scan, stopping mutual scanner") {
					return
				}

				scanCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
				scannedUsername, connections, err := scanner.PerformFullScan(scanCtx, userID, false)

				cancel()

				if err != nil {
					switch {
					case errors.Is(err, discord.ErrUserNotVisible):
						visibilityErrors++
					case isTemporaryError(err):
						temporaryErrors++
						lastTemporaryError = err
					default:
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

			// Circuit breaker backoff delay
			if temporaryErrors > 0 && temporaryErrors == len(scanners) {
				w.logger.Info("All scanners have temporary errors, waiting before next attempt",
					zap.Uint64("userID", userID),
					zap.Error(lastTemporaryError),
					zap.Duration("delay", 30*time.Second))

				if utils.ContextSleep(ctx, 30*time.Second) == utils.SleepCancelled {
					w.logger.Info("Context cancelled during temporary error wait, stopping mutual scanner")
					return
				}
			}

			// Add verification connections
			if successfulScans > 0 {
				verificationConns := w.verificationManager.FetchAllVerificationProfiles(ctx, userID)
				allConnections = append(allConnections, verificationConns...)

				if len(allConnections) > 0 {
					go func(userID uint64, connections []*types.DiscordRobloxConnection) {
						if err := w.scannerPool.ProcessConnections(ctx, userID, connections); err != nil {
							w.logger.Error("Failed to process connections",
								zap.Error(err),
								zap.Uint64("userID", userID))
						}
					}(userID, allConnections)
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
			case temporaryErrors == len(scanners):
				// All scanners have temporary errors (circuit breaker, rate limits)
				// Already logged and handled with backoff, will retry next cycle
			default:
				w.logger.Error("All accounts failed to scan user, will retry next cycle",
					zap.Uint64("userID", userID),
					zap.Int("total_accounts", len(scanners)))
			}

			// Add delay between user scans
			if utils.ContextSleep(ctx, 1*time.Second) == utils.SleepCancelled {
				w.logger.Info("Context cancelled during user delay, stopping mutual scanner")
				return
			}
		}

		if utils.ContextSleep(ctx, 5*time.Second) == utils.SleepCancelled {
			w.logger.Info("Context cancelled during batch wait, stopping mutual scanner")
			return
		}
	}
}

// isTemporaryError determines if an error is temporary (rate limits, circuit breaker, etc.)
func isTemporaryError(err error) bool {
	if err == nil {
		return false
	}

	errStr := err.Error()

	// Circuit breaker errors
	if errors.Is(err, discord.ErrCircuitBreakerOpen) {
		return true
	}

	// Rate limit errors
	if strings.Contains(errStr, "rate limit exceeds") ||
		strings.Contains(errStr, "rate: rate limit") ||
		strings.Contains(errStr, "is blocked acquire options") {
		return true
	}

	return false
}
