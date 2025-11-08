package sync

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/robalyx/rotector/internal/database/models"
	"github.com/robalyx/rotector/internal/database/types"
	"github.com/robalyx/rotector/internal/discord"
	"github.com/robalyx/rotector/pkg/utils"
	"go.uber.org/zap"
)

// runMutualScanner continuously runs full scans for users.
func (w *Worker) runMutualScanner(ctx context.Context) {
	pairCount := w.verificationManager.GetPairCount()
	if pairCount == 0 {
		w.logger.Error("No verification token pairs available, cannot start mutual scanner")
		return
	}

	w.logger.Info("Starting mutual scanner with worker pool",
		zap.Int("worker_count", pairCount))

	// Create worker pool with one worker per token pair
	var wg sync.WaitGroup
	for workerID := range pairCount {
		wg.Add(1)

		go func(pairIndex int) {
			defer wg.Done()

			w.scannerWorker(ctx, pairIndex)
		}(workerID)
	}

	wg.Wait()
	w.logger.Info("All scanner workers stopped")
}

// scannerWorker continuously fetches and processes users using its assigned token pair.
func (w *Worker) scannerWorker(ctx context.Context, pairIndex int) {
	before := time.Now().Add(-12 * time.Hour)

	for {
		if utils.ContextGuardWithLog(ctx, w.logger, "Context cancelled, stopping scanner worker") {
			return
		}

		// Fetch one user to scan
		userID, err := w.db.Model().Sync().GetUserForFullScan(ctx, before)
		if err != nil {
			// Check if no users need scanning
			if errors.Is(err, models.ErrNoUsersNeedScanning) {
				if utils.ContextSleep(ctx, 5*time.Second) == utils.SleepCancelled {
					return
				}

				continue
			}

			w.logger.Error("Failed to get user for full scan",
				zap.Int("pair_index", pairIndex),
				zap.Error(err))

			if utils.ContextSleep(ctx, 5*time.Second) == utils.SleepCancelled {
				return
			}

			continue
		}

		// Process this single user with assigned token pair
		w.processSingleUser(ctx, *userID, pairIndex)

		// Delay between processing users
		if utils.ContextSleep(ctx, 1*time.Second) == utils.SleepCancelled {
			return
		}
	}
}

// processSingleUser processes a single user with the specified token pair.
func (w *Worker) processSingleUser(ctx context.Context, userID uint64, pairIndex int) {
	// Get all scanners to check in all accounts
	scanners := w.scannerPool.GetAll()
	if len(scanners) == 0 {
		w.logger.Error("No scanners available")
		return
	}

	// Run all scanners
	var (
		allConnections     []*types.DiscordRobloxConnection
		username           string
		successfulScans    int
		visibilityErrors   int
		temporaryErrors    int
		lastTemporaryError error
		mu                 sync.Mutex
	)

	var wg sync.WaitGroup
	for accountIndex, scanner := range scanners {
		wg.Add(1)

		go func(index int, sc *discord.Scanner) {
			defer wg.Done()

			if utils.ContextGuardWithLog(ctx, w.logger, "Context cancelled during account scan") {
				return
			}

			scanCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()

			scannedUsername, connections, err := sc.PerformFullScan(scanCtx, userID, false)

			mu.Lock()
			defer mu.Unlock()

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
						zap.Int("account_index", index))
				}
			} else {
				successfulScans++

				if username == "" {
					username = scannedUsername
				}

				allConnections = append(allConnections, connections...)
			}
		}(accountIndex, scanner)
	}

	// Wait for all scanners to complete
	wg.Wait()

	// Circuit breaker backoff delay
	if temporaryErrors > 0 && temporaryErrors == len(scanners) {
		w.logger.Info("All scanners have temporary errors, waiting before next attempt",
			zap.Uint64("userID", userID),
			zap.Error(lastTemporaryError),
			zap.Duration("delay", 30*time.Second))

		if utils.ContextSleep(ctx, 30*time.Second) == utils.SleepCancelled {
			return
		}
	}

	// Add verification connections
	if successfulScans > 0 {
		verificationConns := w.verificationManager.FetchVerificationProfilesWithPair(ctx, userID, pairIndex)
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
				zap.Int("pair_index", pairIndex),
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
		// Already logged and handled with backoff and will retry next cycle
	default:
		w.logger.Error("All accounts failed to scan user, will retry next cycle",
			zap.Uint64("userID", userID),
			zap.Int("total_accounts", len(scanners)))
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
