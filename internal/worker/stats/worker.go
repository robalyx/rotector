package stats

import (
	"context"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/redis/rueidis"
	"github.com/robalyx/rotector/internal/common/client/ai"
	"github.com/robalyx/rotector/internal/common/progress"
	"github.com/robalyx/rotector/internal/common/setup"
	"github.com/robalyx/rotector/internal/common/storage/database"
	"github.com/robalyx/rotector/internal/common/storage/database/types"
	"github.com/robalyx/rotector/internal/common/storage/redis"
	"github.com/robalyx/rotector/internal/worker/core"
	"go.uber.org/zap"
)

// Redis key for cached chart images.
const (
	UserStatsChartKey  = "stats:chart:users"
	GroupStatsChartKey = "stats:chart:groups"
)

// Worker handles hourly statistics snapshots.
type Worker struct {
	db          *database.Client
	bar         *progress.Bar
	reporter    *core.StatusReporter
	analyzer    *ai.StatsAnalyzer
	redisClient rueidis.Client
	logger      *zap.Logger
}

// New creates a new stats worker.
func New(app *setup.App, bar *progress.Bar, logger *zap.Logger) *Worker {
	// Get Redis client for stats
	statsClient, err := app.RedisManager.GetClient(redis.StatsDBIndex)
	if err != nil {
		logger.Fatal("Failed to get Redis client for stats", zap.Error(err))
	}

	return &Worker{
		db:          app.DB,
		bar:         bar,
		reporter:    core.NewStatusReporter(app.StatusClient, "stats", "", logger),
		analyzer:    ai.NewStatsAnalyzer(app, logger),
		redisClient: statsClient,
		logger:      logger,
	}
}

// Start begins the statistics worker's main loop.
func (w *Worker) Start() { //nolint:funlen
	w.logger.Info("Statistics Worker started", zap.String("workerID", w.reporter.GetWorkerID()))
	w.reporter.Start()
	defer w.reporter.Stop()

	w.bar.SetTotal(100)

	for {
		w.bar.Reset()
		w.reporter.SetHealthy(true)

		ctx := context.Background()
		currentHour := time.Now().UTC().Truncate(time.Hour)

		// Step 1: Check if stats exist for current hour (0%)
		w.bar.SetStepMessage("Checking current hour stats", 0)
		w.reporter.UpdateStatus("Checking current hour stats", 0)

		exists, err := w.db.Stats().HasStatsForHour(ctx, currentHour)
		if err != nil {
			w.logger.Error("Failed to check current hour stats", zap.Error(err))
			w.reporter.SetHealthy(false)
			continue
		}

		if !exists {
			// Step 2: Get current stats (20%)
			w.bar.SetStepMessage("Collecting statistics", 20)
			w.reporter.UpdateStatus("Collecting statistics", 20)
			stats, err := w.db.Stats().GetCurrentStats(ctx)
			if err != nil {
				w.logger.Error("Failed to get current stats", zap.Error(err))
				w.reporter.SetHealthy(false)
				continue
			}

			// Step 3: Save current stats (40%)
			w.bar.SetStepMessage("Saving statistics", 40)
			w.reporter.UpdateStatus("Saving statistics", 40)
			if err := w.db.Stats().SaveHourlyStats(ctx, stats); err != nil {
				w.logger.Error("Failed to save hourly stats", zap.Error(err))
				w.reporter.SetHealthy(false)
				continue
			}
		}

		// Get hourly stats
		hourlyStats, err := w.db.Stats().GetHourlyStats(ctx)
		if err != nil {
			w.logger.Error("Failed to get hourly stats", zap.Error(err))
			w.reporter.SetHealthy(false)
			continue
		}

		// Step 4: Generate and cache charts (50%)
		w.bar.SetStepMessage("Generating charts", 50)
		w.reporter.UpdateStatus("Generating charts", 50)
		if err := w.generateAndCacheCharts(ctx, hourlyStats); err != nil {
			w.logger.Error("Failed to generate and cache charts", zap.Error(err))
			w.reporter.SetHealthy(false)
			continue
		}

		// Step 5: Update welcome message (60%)
		w.bar.SetStepMessage("Updating welcome message", 60)
		w.reporter.UpdateStatus("Updating welcome message", 60)
		if err := w.updateWelcomeMessage(ctx, hourlyStats); err != nil {
			w.logger.Error("Failed to update welcome message", zap.Error(err))
			w.reporter.SetHealthy(false)
			continue
		}

		// Step 6: Clean up old stats (80%)
		w.bar.SetStepMessage("Cleaning up old stats", 80)
		w.reporter.UpdateStatus("Cleaning up old stats", 80)
		cutoffDate := time.Now().UTC().AddDate(0, 0, -30) // 30 days ago
		if err := w.db.Stats().PurgeOldStats(ctx, cutoffDate); err != nil {
			w.logger.Error("Failed to purge old stats", zap.Error(err))
			w.reporter.SetHealthy(false)
			continue
		}

		// Step 7: Completed (100%)
		w.bar.SetStepMessage("Waiting for next hour", 100)
		w.reporter.UpdateStatus("Waiting for next hour", 100)
		nextHour := currentHour.Add(time.Hour)
		time.Sleep(time.Until(nextHour))

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
	botSettings, err := w.db.Settings().GetBotSettings(ctx)
	if err != nil {
		return fmt.Errorf("failed to get bot settings: %w", err)
	}
	botSettings.WelcomeMessage = message

	if err := w.db.Settings().SaveBotSettings(ctx, botSettings); err != nil {
		return fmt.Errorf("failed to save welcome message: %w", err)
	}

	w.logger.Info("Updated welcome message", zap.String("message", message))
	return nil
}
