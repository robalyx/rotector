package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	stdSync "sync"
	"syscall"
	"time"

	"github.com/robalyx/rotector/internal/setup"
	"github.com/robalyx/rotector/internal/setup/telemetry"
	"github.com/robalyx/rotector/internal/tui"
	"github.com/robalyx/rotector/internal/tui/components"
	"github.com/robalyx/rotector/internal/worker/friend"
	"github.com/robalyx/rotector/internal/worker/group"
	"github.com/robalyx/rotector/internal/worker/maintenance"
	"github.com/robalyx/rotector/internal/worker/queue"
	"github.com/robalyx/rotector/internal/worker/reason"
	"github.com/robalyx/rotector/internal/worker/stats"
	"github.com/robalyx/rotector/internal/worker/sync"
	"github.com/robalyx/rotector/internal/worker/upload"
	"github.com/robalyx/rotector/pkg/utils"
	"github.com/urfave/cli/v3"
	"go.uber.org/zap"
)

const (
	// WorkerLogDir specifies where worker log files are stored.
	WorkerLogDir = "logs/worker_logs"

	FriendWorker      = "friend"
	GroupWorker       = "group"
	MaintenanceWorker = "maintenance"
	StatsWorker       = "stats"
	QueueWorker       = "queue"
	SyncWorker        = "sync"
	ReasonWorker      = "reason"
	UploadWorker      = "upload"
	WarWorker         = "war"
)

func main() {
	if err := run(); err != nil {
		log.Printf("Error: %v", err)
		os.Exit(1)
	}
}

func run() error {
	app := &cli.Command{
		Name:  "worker",
		Usage: "Start the rotector worker",
		Flags: []cli.Flag{
			&cli.IntFlag{
				Name:    "workers",
				Aliases: []string{"w"},
				Value:   1,
				Usage:   "Number of workers to start",
			},
		},
		Commands: []*cli.Command{
			{
				Name:  FriendWorker,
				Usage: "Start friend network workers",
				Action: func(ctx context.Context, c *cli.Command) error {
					runWorkers(ctx, FriendWorker, c.Int("workers"))
					return nil
				},
			},
			{
				Name:  GroupWorker,
				Usage: "Start group member workers",
				Action: func(ctx context.Context, c *cli.Command) error {
					runWorkers(ctx, GroupWorker, c.Int("workers"))
					return nil
				},
			},
			{
				Name:  MaintenanceWorker,
				Usage: "Start maintenance workers",
				Action: func(ctx context.Context, c *cli.Command) error {
					runWorkers(ctx, MaintenanceWorker, c.Int("workers"))
					return nil
				},
			},
			{
				Name:  StatsWorker,
				Usage: "Start statistics worker",
				Action: func(ctx context.Context, c *cli.Command) error {
					runWorkers(ctx, StatsWorker, c.Int("workers"))
					return nil
				},
			},
			{
				Name:  QueueWorker,
				Usage: "Start queue process worker",
				Action: func(ctx context.Context, c *cli.Command) error {
					runWorkers(ctx, QueueWorker, c.Int("workers"))
					return nil
				},
			},
			{
				Name:  SyncWorker,
				Usage: "Start sync worker",
				Action: func(ctx context.Context, c *cli.Command) error {
					runWorkers(ctx, SyncWorker, c.Int("workers"))
					return nil
				},
			},
			{
				Name:  ReasonWorker,
				Usage: "Start reason update worker",
				Action: func(ctx context.Context, _ *cli.Command) error {
					runWorkers(ctx, ReasonWorker, 1)
					return nil
				},
			},
			{
				Name:  UploadWorker,
				Usage: "Start upload processing worker",
				Action: func(ctx context.Context, c *cli.Command) error {
					runWorkers(ctx, UploadWorker, c.Int("workers"))
					return nil
				},
			},
		},
	}

	return app.Run(context.Background(), os.Args)
}

// runWorkers starts multiple instances of a worker type.
func runWorkers(ctx context.Context, workerType string, count int) {
	// Create context that can be cancelled on signals
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Set up signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		cancel()
	}()

	// Initialize shared app for TUI and common resources
	app, err := setup.InitializeApp(ctx, telemetry.ServiceWorker, WorkerLogDir, workerType, "")
	if err != nil {
		log.Printf("Failed to initialize application: %v", err)
		return
	}
	defer app.Cleanup(ctx)

	// Initialize TUI manager
	sessionLogDir := app.LogManager.GetCurrentSessionDir()

	tuiManager := tui.NewManager(ctx, sessionLogDir, app.Logger)
	if err := tuiManager.Start(); err != nil {
		log.Printf("Failed to start TUI: %v", err)
		return
	}
	defer tuiManager.Stop()

	// Initialize progress bars for workers
	bars := make([]*components.ProgressBar, count)
	for i := range count {
		workerName := fmt.Sprintf("%s Worker %d", workerType, i)
		loggerName := fmt.Sprintf("%s_worker_%d", workerType, i)
		logPath := filepath.Join(sessionLogDir, loggerName+".log")
		bars[i] = tuiManager.AddWorker(i, workerType, workerName, logPath)
	}

	// Get startup delay from config
	startupDelay := app.Config.Worker.StartupDelay
	if startupDelay <= 0 {
		startupDelay = 2000 // Default to 2000ms if not configured
	}

	// Start workers
	var wg stdSync.WaitGroup
	for workerID := range count {
		wg.Go(func() {
			// Add staggered startup delay
			delay := time.Duration(workerID) * time.Duration(startupDelay) * time.Millisecond
			if utils.ContextSleep(ctx, delay) == utils.SleepCancelled {
				return
			}

			// Create worker-specific logger using the shared app
			workerLogger := app.LogManager.GetWorkerLogger(
				fmt.Sprintf("%s_worker_%d", workerType, workerID),
			)

			// Generate unique instance ID for this worker
			instanceID := fmt.Sprintf("%s_%d", app.LogManager.GetInstanceID(), workerID)

			// Get progress bar for this worker
			bar := bars[workerID]

			var w interface{ Start(context.Context) }

			switch workerType {
			case FriendWorker:
				w = friend.New(app, bar, workerLogger, instanceID)
			case GroupWorker:
				w = group.New(app, bar, workerLogger, instanceID)
			case MaintenanceWorker:
				w = maintenance.New(app, bar, workerLogger, instanceID)
			case StatsWorker:
				w = stats.New(app, bar, workerLogger, instanceID)
			case QueueWorker:
				w = queue.New(app, bar, workerLogger, instanceID)
			case SyncWorker:
				w = sync.New(app, bar, workerLogger, instanceID)
			case ReasonWorker:
				w = reason.New(app, bar, workerLogger, instanceID)
			case UploadWorker:
				w = upload.New(app, bar, workerLogger, instanceID)
			default:
				log.Fatalf("Invalid worker type: %s", workerType)
			}

			runWorker(ctx, w, workerLogger)
		})
	}

	log.Printf("Started %d %s workers", count, workerType)
	wg.Wait()
}

// runWorker runs a single worker in a loop with error recovery.
func runWorker(ctx context.Context, w interface{ Start(context.Context) }, logger *zap.Logger) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			func() {
				defer func() {
					if r := recover(); r != nil {
						logger.Error("Worker execution failed",
							zap.String("worker_type", fmt.Sprintf("%T", w)),
							zap.Any("panic", r),
						)
						logger.Info("Restarting worker in 5 seconds...")

						// Respect context cancellation during sleep
						if utils.ContextSleep(ctx, 5*time.Second) == utils.SleepCancelled {
							return
						}
					}
				}()

				logger.Info("Starting worker")
				w.Start(ctx)
			}()

			// Check if context was cancelled
			if ctx.Err() != nil {
				return
			}

			logger.Warn("Worker stopped unexpectedly",
				zap.String("worker_type", fmt.Sprintf("%T", w)),
			)

			// Respect context cancellation during sleep
			if utils.ContextSleep(ctx, 5*time.Second) == utils.SleepCancelled {
				return
			}
		}
	}
}
