package stats

import (
	"context"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/redis/rueidis"
	"github.com/robalyx/rotector/internal/ai"
	"github.com/robalyx/rotector/internal/database"
	"github.com/robalyx/rotector/internal/database/types"
	"github.com/robalyx/rotector/internal/redis"
	"github.com/robalyx/rotector/internal/setup"
	"github.com/robalyx/rotector/internal/tui/components"
	"github.com/robalyx/rotector/internal/worker/core"
	"github.com/robalyx/rotector/pkg/utils"
	"go.uber.org/zap"
)

// Redis key for cached chart images.
const (
	UserStatsChartKey  = "stats:chart:users"
	GroupStatsChartKey = "stats:chart:groups"
)

// Worker handles hourly statistics snapshots.
type Worker struct {
	db          database.Client
	bar         *components.ProgressBar
	reporter    *core.StatusReporter
	analyzer    *ai.StatsAnalyzer
	redisClient rueidis.Client
	logger      *zap.Logger
}

// New creates a new stats worker.
func New(app *setup.App, bar *components.ProgressBar, logger *zap.Logger) *Worker {
	// Get Redis client for stats
	statsClient, err := app.RedisManager.GetClient(redis.StatsDBIndex)
	if err != nil {
		logger.Fatal("Failed to get Redis client for stats", zap.Error(err))
	}

	return &Worker{
		db:          app.DB,
		bar:         bar,
		reporter:    core.NewStatusReporter(app.StatusClient, "stats", logger),
		analyzer:    ai.NewStatsAnalyzer(app, logger),
		redisClient: statsClient,
		logger:      logger.Named("stats_worker"),
	}
}

// Start begins the statistics worker's main loop.
func (w *Worker) Start(ctx context.Context) {
	w.logger.Info("Statistics Worker started", zap.String("workerID", w.reporter.GetWorkerID()))

	w.reporter.Start(ctx)
	defer w.reporter.Stop()

	w.bar.SetTotal(100)

	for {
		// Check if context was cancelled
		if utils.ContextGuardWithLog(ctx, w.logger, "Context cancelled, stopping stats worker") {
			w.bar.SetStepMessage("Shutting down", 100)
			w.reporter.UpdateStatus("Shutting down", 100)

			return
		}

		w.bar.Reset()
		w.reporter.SetHealthy(true)

		currentHour := time.Now().UTC().Truncate(time.Hour)

		// Step 1: Check if stats exist for current hour (0%)
		w.bar.SetStepMessage("Checking current hour stats", 0)
		w.reporter.UpdateStatus("Checking current hour stats", 0)

		exists, err := w.db.Model().Stats().HasStatsForHour(ctx, currentHour)
		if err != nil {
			w.logger.Error("Failed to check current hour stats", zap.Error(err))
			w.reporter.SetHealthy(false)

			if !utils.ErrorSleep(ctx, 30*time.Second, w.logger, "stats worker") {
				return
			}

			continue
		}

		if !exists {
			// Step 2: Save current stats (30%)
			w.bar.SetStepMessage("Saving statistics", 30)
			w.reporter.UpdateStatus("Saving statistics", 30)

			if err := w.db.Service().Stats().SaveHourlyStats(ctx); err != nil {
				w.logger.Error("Failed to save hourly stats", zap.Error(err))
				w.reporter.SetHealthy(false)

				if !utils.ErrorSleep(ctx, 30*time.Second, w.logger, "stats worker") {
					return
				}

				continue
			}
		}

		// Get hourly stats
		hourlyStats, err := w.db.Model().Stats().GetHourlyStats(ctx)
		if err != nil {
			w.logger.Error("Failed to get hourly stats", zap.Error(err))
			w.reporter.SetHealthy(false)

			if !utils.ErrorSleep(ctx, 30*time.Second, w.logger, "stats worker") {
				return
			}

			continue
		}

		// Step 3: Generate and cache charts (40%)
		w.bar.SetStepMessage("Generating charts", 40)
		w.reporter.UpdateStatus("Generating charts", 40)

		if err := w.generateAndCacheCharts(ctx, hourlyStats); err != nil {
			w.logger.Error("Failed to generate and cache charts", zap.Error(err))
			w.reporter.SetHealthy(false)

			if !utils.ErrorSleep(ctx, 30*time.Second, w.logger, "stats worker") {
				return
			}

			continue
		}

		// Step 4: Update welcome message (60%)
		w.bar.SetStepMessage("Updating welcome message", 60)
		w.reporter.UpdateStatus("Updating welcome message", 60)

		if err := w.updateWelcomeMessage(ctx, hourlyStats); err != nil {
			w.logger.Error("Failed to update welcome message", zap.Error(err))
			w.reporter.SetHealthy(false)

			if !utils.ErrorSleep(ctx, 30*time.Second, w.logger, "stats worker") {
				return
			}

			continue
		}

		// Step 5: Clean up old stats (80%)
		w.bar.SetStepMessage("Cleaning up old stats", 80)
		w.reporter.UpdateStatus("Cleaning up old stats", 80)

		cutoffDate := time.Now().UTC().AddDate(0, 0, -30) // 30 days ago
		if err := w.db.Model().Stats().PurgeOldStats(ctx, cutoffDate); err != nil {
			w.logger.Error("Failed to purge old stats", zap.Error(err))
			w.reporter.SetHealthy(false)

			if !utils.ErrorSleep(ctx, 30*time.Second, w.logger, "stats worker") {
				return
			}

			continue
		}

		// Step 6: Completed (100%)
		w.bar.SetStepMessage("Waiting for next hour", 100)
		w.reporter.UpdateStatus("Waiting for next hour", 100)

		nextHour := currentHour.Add(time.Hour)

		// Wait for next hour
		if utils.ContextSleepUntilWithLog(
			ctx, nextHour, w.logger, "Context cancelled during wait for next hour, stopping stats worker",
		) == utils.SleepCancelled {
			return
		}

		w.logger.Info("Hourly statistics processing completed")
	}
}

// generateAndCacheCharts generates statistics charts and caches them in Redis.
func (w *Worker) generateAndCacheCharts(ctx context.Context, hourlyStats []*types.HourlyStats) error {
	// Generate charts
	userStatsChart, groupStatsChart, err := NewChartBuilder(hourlyStats).Build()
	if err != nil {
		return fmt.Errorf("failed to build stats charts: %w", err)
	}

	// Cache user stats chart
	if err := w.redisClient.Do(ctx,
		w.redisClient.B().Set().
			Key(UserStatsChartKey).
			Value(base64.StdEncoding.EncodeToString(userStatsChart.Bytes())).
			Ex(time.Hour*2).
			Build(),
	).Error(); err != nil {
		return fmt.Errorf("failed to cache user stats chart: %w", err)
	}

	// Cache group stats chart
	if err := w.redisClient.Do(ctx,
		w.redisClient.B().Set().
			Key(GroupStatsChartKey).
			Value(base64.StdEncoding.EncodeToString(groupStatsChart.Bytes())).
			Ex(time.Hour*2).
			Build(),
	).Error(); err != nil {
		return fmt.Errorf("failed to cache group stats chart: %w", err)
	}

	return nil
}

// updateWelcomeMessage handles the generation and updating of the welcome message.
func (w *Worker) updateWelcomeMessage(ctx context.Context, hourlyStats []*types.HourlyStats) error {
	// Generate new welcome message
	message, err := w.analyzer.GenerateWelcomeMessage(ctx, hourlyStats)
	if err != nil {
		return fmt.Errorf("failed to generate welcome message: %w", err)
	}

	// Get and update bot settings
	botSettings, err := w.db.Model().Setting().GetBotSettings(ctx)
	if err != nil {
		return fmt.Errorf("failed to get bot settings: %w", err)
	}

	botSettings.WelcomeMessage = message

	if err := w.db.Model().Setting().SaveBotSettings(ctx, botSettings); err != nil {
		return fmt.Errorf("failed to save welcome message: %w", err)
	}

	w.logger.Info("Updated welcome message", zap.String("message", message))

	return nil
}
