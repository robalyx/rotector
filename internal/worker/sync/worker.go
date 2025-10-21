package sync

import (
	"context"
	"errors"
	"math/rand"
	"time"

	"github.com/diamondburned/arikawa/v3/gateway"
	"github.com/diamondburned/arikawa/v3/state"
	"github.com/diamondburned/ningen/v3"
	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/redis/rueidis"
	"github.com/robalyx/rotector/internal/ai"
	"github.com/robalyx/rotector/internal/database"
	"github.com/robalyx/rotector/internal/discord"
	"github.com/robalyx/rotector/internal/redis"
	"github.com/robalyx/rotector/internal/setup"
	"github.com/robalyx/rotector/internal/setup/config"
	"github.com/robalyx/rotector/internal/tui/components"
	"github.com/robalyx/rotector/internal/worker/core"
	"github.com/robalyx/rotector/internal/worker/sync/events"
	"github.com/robalyx/rotector/pkg/utils"
	"go.uber.org/zap"
)

var (
	ErrTimeout              = errors.New("timed out waiting for member chunks")
	ErrNoTextChannel        = errors.New("no text channel found in guild")
	ErrAllChannelsAttempted = errors.New("all available channels have been attempted")
	ErrListNotFoundRetry    = errors.New("member list not found after multiple attempts")
)

// Worker handles syncing Discord server members.
type Worker struct {
	db                 database.Client
	roAPI              *api.API
	state              *ningen.State
	bar                *components.ProgressBar
	reporter           *core.StatusReporter
	logger             *zap.Logger
	config             *config.Config
	messageAnalyzer    *ai.MessageAnalyzer
	eventHandler       *events.Handler
	ratelimit          rueidis.Client
	scanner            *discord.Scanner
	discordRateLimiter *requestRateLimiter
	rng                *rand.Rand
}

// New creates a new sync worker.
func New(app *setup.App, bar *components.ProgressBar, logger *zap.Logger, instanceID string) *Worker {
	// Create Discord state with sync token and required intents
	s := state.NewWithIntents(app.Config.Common.Discord.SyncToken,
		gateway.IntentGuilds|gateway.IntentGuildMembers|
			gateway.IntentGuildMessages|gateway.IntentMessageContent)

	// Disguise user agent
	s.UserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:136.0) Gecko/20100101 Firefox/136.0"

	// Create ningen state from discord state
	n := ningen.FromState(s)
	n.MemberState.OnError = func(err error) {
		logger.Warn("Member state error", zap.Error(err))
	}

	// Create status reporter
	reporter := core.NewStatusReporter(app.StatusClient, "sync", instanceID, logger)

	// Create message analyzer
	messageAnalyzer := ai.NewMessageAnalyzer(app, logger)

	// Create event handler
	eventHandler := events.New(app, n, messageAnalyzer, logger)
	eventHandler.Setup()

	// Create rate limit client
	ratelimit, err := app.RedisManager.GetClient(redis.RatelimitDBIndex)
	if err != nil {
		logger.Fatal("Failed to get Redis client for proxy rotation", zap.Error(err))
	}

	return &Worker{
		db:                 app.DB,
		roAPI:              app.RoAPI,
		state:              n,
		bar:                bar,
		reporter:           reporter,
		logger:             logger.Named("sync_worker"),
		config:             app.Config,
		messageAnalyzer:    messageAnalyzer,
		eventHandler:       eventHandler,
		ratelimit:          ratelimit,
		scanner:            discord.NewScanner(app.DB, app.CFClient, ratelimit, n.Session, messageAnalyzer, logger),
		discordRateLimiter: newRequestRateLimiter(1*time.Second, 200*time.Millisecond),
		rng:                rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// Start begins the sync worker's main loop.
func (w *Worker) Start(ctx context.Context) {
	w.logger.Info("Sync Worker started", zap.String("workerID", w.reporter.GetWorkerID()))

	w.reporter.Start(ctx)
	defer w.reporter.Stop()

	// Open Discord gateway connection
	if err := w.state.Open(ctx); err != nil {
		w.logger.Fatal("Failed to open gateway", zap.Error(err))
	}
	defer w.state.Close()

	// Start full user scanner in a separate goroutine
	go w.runMutualScanner(ctx)

	for {
		// Check if context was cancelled
		if utils.ContextGuardWithLog(ctx, w.logger, "Context cancelled, stopping sync worker") {
			w.bar.SetStepMessage("Shutting down", 100)
			w.reporter.UpdateStatus("Shutting down", 100)

			return
		}

		w.bar.Reset()
		w.reporter.SetHealthy(true)

		// Run sync cycle
		if err := w.syncCycle(ctx); err != nil {
			w.logger.Error("Failed to sync servers", zap.Error(err))
			w.reporter.SetHealthy(false)

			if !utils.ErrorSleep(ctx, 1*time.Minute, w.logger, "sync worker") {
				return
			}

			continue
		}

		// Short pause between cycles
		w.bar.SetStepMessage("Waiting for next cycle", 100)
		w.reporter.UpdateStatus("Waiting for next cycle", 100)

		if !utils.IntervalSleep(ctx, 15*time.Minute, w.logger, "sync worker") {
			return
		}
	}
}
