package core

import (
	"context"
	"fmt"
	"time"

	"github.com/robalyx/rotector/internal/database"
	"github.com/robalyx/rotector/internal/progress"
	"github.com/robalyx/rotector/pkg/utils"
	"go.uber.org/zap"
)

// ThresholdChecker handles common threshold checking logic for workers.
type ThresholdChecker struct {
	db         database.Client
	threshold  int
	bar        *progress.Bar
	reporter   *StatusReporter
	logger     *zap.Logger
	workerName string
}

// NewThresholdChecker creates a new threshold checker.
func NewThresholdChecker(
	db database.Client,
	threshold int,
	bar *progress.Bar,
	reporter *StatusReporter,
	logger *zap.Logger,
	workerName string,
) *ThresholdChecker {
	return &ThresholdChecker{
		db:         db,
		threshold:  threshold,
		bar:        bar,
		reporter:   reporter,
		logger:     logger,
		workerName: workerName,
	}
}

// CheckThreshold checks if the flagged users count exceeds the threshold.
// Returns true if threshold is exceeded and worker should pause, false if worker should continue.
func (tc *ThresholdChecker) CheckThreshold(ctx context.Context) (bool, error) {
	// Check flagged users count
	flaggedCount, err := tc.db.Model().User().GetFlaggedUsersCount(ctx)
	if err != nil {
		tc.logger.Error("Error getting flagged users count", zap.Error(err))
		tc.reporter.SetHealthy(false)

		return false, err
	}

	// If above threshold, pause processing
	if flaggedCount >= tc.threshold {
		tc.bar.SetStepMessage(fmt.Sprintf(
			"Paused - %d flagged users exceeds threshold of %d",
			flaggedCount, tc.threshold,
		), 0)
		tc.reporter.UpdateStatus(fmt.Sprintf(
			"Paused - %d flagged users exceeds threshold",
			flaggedCount,
		), 0)
		tc.logger.Info("Pausing worker - flagged users threshold exceeded",
			zap.Int("flaggedCount", flaggedCount),
			zap.Int("threshold", tc.threshold))

		if !utils.ThresholdSleep(ctx, 5*time.Minute, tc.logger, tc.workerName) {
			return false, ctx.Err()
		}

		return true, nil
	}

	return false, nil
}
