package sync

import (
	"context"
	"errors"
	"time"

	"github.com/diamondburned/arikawa/v3/gateway"
	"github.com/diamondburned/arikawa/v3/state"
	"github.com/diamondburned/ningen/v3"
	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/redis/rueidis"
	"github.com/robalyx/rotector/internal/common/client/ai"
	"github.com/robalyx/rotector/internal/common/client/fetcher"
	"github.com/robalyx/rotector/internal/common/discord"
	"github.com/robalyx/rotector/internal/common/progress"
	"github.com/robalyx/rotector/internal/common/setup"
	"github.com/robalyx/rotector/internal/common/setup/config"
	"github.com/robalyx/rotector/internal/common/storage/database"
	"github.com/robalyx/rotector/internal/common/storage/redis"
	"github.com/robalyx/rotector/internal/worker/core"
	"github.com/robalyx/rotector/internal/worker/sync/events"
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
	db               database.Client
	roAPI            *api.API
	state            *ningen.State
	bar              *progress.Bar
	reporter         *core.StatusReporter
	logger           *zap.Logger
	config           *config.Config
	messageAnalyzer  *ai.MessageAnalyzer
	eventHandler     *events.Handler
	ratelimit        rueidis.Client
	thumbnailFetcher *fetcher.ThumbnailFetcher
	scanner          *discord.Scanner
}

// New creates a new sync worker.
func New(app *setup.App, bar *progress.Bar, logger *zap.Logger) *Worker {
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
	reporter := core.NewStatusReporter(app.StatusClient, "sync", logger)

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
		db:               app.DB,
		roAPI:            app.RoAPI,
		state:            n,
		bar:              bar,
		reporter:         reporter,
		logger:           logger.Named("sync_worker"),
		config:           app.Config,
		messageAnalyzer:  messageAnalyzer,
		eventHandler:     eventHandler,
		ratelimit:        ratelimit,
		thumbnailFetcher: fetcher.NewThumbnailFetcher(app.RoAPI, logger),
		scanner:          discord.NewScanner(app.DB, ratelimit, n.Session, logger),
	}
}

// Start begins the sync worker's main loop.
func (w *Worker) Start() {
	w.logger.Info("Sync Worker started", zap.String("workerID", w.reporter.GetWorkerID()))
	w.reporter.Start()
	defer w.reporter.Stop()

	// Open Discord gateway connection
	if err := w.state.Open(context.Background()); err != nil {
		w.logger.Fatal("Failed to open gateway", zap.Error(err))
	}
	defer w.state.Close()

	// Start game scanner in a separate goroutine
	go w.runGameScanner()

	// Start full user scanner in a separate goroutine
	go w.runMutualScanner()

	for {
		w.bar.Reset()
		w.reporter.SetHealthy(true)

		// Run sync cycle
		if err := w.syncCycle(); err != nil {
			w.logger.Error("Failed to sync servers", zap.Error(err))
			w.reporter.SetHealthy(false)
			time.Sleep(1 * time.Minute)
			continue
		}

		// Short pause between cycles
		w.bar.SetStepMessage("Waiting for next cycle", 100)
		w.reporter.UpdateStatus("Waiting for next cycle", 100)
		time.Sleep(15 * time.Minute)
	}
}
