package sync

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/diamondburned/arikawa/v3/gateway"
	"github.com/diamondburned/arikawa/v3/state"
	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/redis/rueidis"
	"github.com/robalyx/rotector/internal/ai"
	"github.com/robalyx/rotector/internal/database"
	"github.com/robalyx/rotector/internal/discord"
	"github.com/robalyx/rotector/internal/discord/memberstate"
	"github.com/robalyx/rotector/internal/discord/verification"
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
	ErrRateLimiterNotFound  = errors.New("rate limiter not found for account")
)

// Worker handles syncing Discord server members.
type Worker struct {
	db                  database.Client
	roAPI               *api.API
	states              []*state.State
	memberStates        []*memberstate.State
	scannerPool         *discord.ScannerPool
	verificationManager *verification.ServiceManager
	bar                 *components.ProgressBar
	reporter            *core.StatusReporter
	logger              *zap.Logger
	config              *config.Config
	messageAnalyzer     *ai.MessageAnalyzer
	eventHandlers       []*events.Handler
	ratelimit           rueidis.Client
	discordRateLimiters []*requestRateLimiter
	seenServers         map[uint64]int
	seenServersMutex    sync.RWMutex
	rng                 *rand.Rand
}

// New creates a new sync worker.
func New(app *setup.App, bar *components.ProgressBar, logger *zap.Logger, instanceID string) *Worker {
	syncLogger := logger.Named("sync_worker")

	// Validate sync tokens
	if len(app.Config.Common.Discord.SyncTokens) == 0 {
		logger.Fatal("No sync tokens configured")
	}

	// Create rate limit client
	ratelimit, err := app.RedisManager.GetClient(redis.RatelimitDBIndex)
	if err != nil {
		logger.Fatal("Failed to get Redis client for proxy rotation", zap.Error(err))
	}

	// Create verification service manager
	verificationManager, err := verification.NewServiceManager(app.Config.Common.Discord, logger)
	if err != nil {
		logger.Fatal("Failed to create verification services", zap.Error(err))
	}

	// Create message analyzer
	messageAnalyzer := ai.NewMessageAnalyzer(app, logger)

	// Create status reporter
	reporter := core.NewStatusReporter(app.StatusClient, "sync", instanceID, logger)

	// Initialize arrays for multi-account support
	states := make([]*state.State, 0, len(app.Config.Common.Discord.SyncTokens))
	memberStates := make([]*memberstate.State, 0, len(app.Config.Common.Discord.SyncTokens))
	eventHandlers := make([]*events.Handler, 0, len(app.Config.Common.Discord.SyncTokens))
	scanners := make([]*discord.Scanner, 0, len(app.Config.Common.Discord.SyncTokens))
	discordRateLimiters := make([]*requestRateLimiter, 0, len(app.Config.Common.Discord.SyncTokens))

	// Create necessary dependencies for each sync token
	for i, token := range app.Config.Common.Discord.SyncTokens {
		// Create Discord state with sync token and required intents
		s := state.NewWithIntents(token,
			gateway.IntentGuilds|gateway.IntentGuildMembers|
				gateway.IntentGuildMessages|gateway.IntentMessageContent)

		// Disguise user agent
		s.UserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:136.0) Gecko/20100101 Firefox/136.0"

		// Create member state with error handling
		ms := memberstate.NewState(s, s)
		ms.OnError = func(err error) {
			syncLogger.Warn("Member state error", zap.Int("account_index", i), zap.Error(err))
		}

		// Create event handler for this account
		eventHandler := events.New(app, s, ms, messageAnalyzer, logger)
		eventHandler.Setup()

		// Create scanner for this account
		scannerID := fmt.Sprintf("scanner_%d", i)
		scanner := discord.NewScanner(app.DB, app.CFClient, ratelimit, s.Session, messageAnalyzer, scannerID, logger)

		// Create rate limiter for this account
		rateLimiter := newRequestRateLimiter(1*time.Second, 200*time.Millisecond)

		states = append(states, s)
		memberStates = append(memberStates, ms)
		eventHandlers = append(eventHandlers, eventHandler)
		scanners = append(scanners, scanner)
		discordRateLimiters = append(discordRateLimiters, rateLimiter)

		syncLogger.Info("Initialized sync account", zap.Int("account_index", i))
	}

	return &Worker{
		db:                  app.DB,
		roAPI:               app.RoAPI,
		states:              states,
		memberStates:        memberStates,
		scannerPool:         discord.NewScannerPool(scanners, app.DB, syncLogger),
		verificationManager: verificationManager,
		bar:                 bar,
		reporter:            reporter,
		logger:              syncLogger,
		config:              app.Config,
		messageAnalyzer:     messageAnalyzer,
		eventHandlers:       eventHandlers,
		ratelimit:           ratelimit,
		discordRateLimiters: discordRateLimiters,
		seenServers:         make(map[uint64]int),
		rng:                 rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// Start begins the sync worker's main loop.
func (w *Worker) Start(ctx context.Context) {
	w.logger.Info("Sync Worker started",
		zap.String("workerID", w.reporter.GetWorkerID()),
		zap.Int("account_count", len(w.states)))

	w.reporter.Start(ctx)
	defer w.reporter.Stop()

	// Open all Discord gateway connections
	for i, s := range w.states {
		if err := s.Open(ctx); err != nil {
			w.logger.Fatal("Failed to open gateway",
				zap.Int("account_index", i),
				zap.Error(err))
		}

		w.logger.Info("Gateway opened", zap.Int("account_index", i))
	}

	// Close all gateway connections on shutdown
	defer func() {
		for i, s := range w.states {
			if err := s.Close(); err != nil {
				w.logger.Warn("Failed to close gateway",
					zap.Int("account_index", i),
					zap.Error(err))
			}
		}
	}()

	// Start verification services
	if err := w.verificationManager.Start(ctx); err != nil {
		w.logger.Fatal("Failed to start verification services", zap.Error(err))
	}

	// Close verification services on shutdown
	defer func() {
		if err := w.verificationManager.Close(); err != nil {
			w.logger.Warn("Failed to close verification services", zap.Error(err))
		}
	}()

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
		w.syncCycle(ctx)

		// Short pause between cycles
		w.bar.SetStepMessage("Waiting for next cycle", 100)
		w.reporter.UpdateStatus("Waiting for next cycle", 100)

		if !utils.IntervalSleep(ctx, 15*time.Minute, w.logger, "sync worker") {
			return
		}
	}
}
