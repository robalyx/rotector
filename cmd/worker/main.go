package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/robalyx/rotector/internal/common/progress"
	"github.com/robalyx/rotector/internal/common/setup"
	"github.com/robalyx/rotector/internal/worker/friend"
	"github.com/robalyx/rotector/internal/worker/group"
	"github.com/robalyx/rotector/internal/worker/maintenance"
	"github.com/robalyx/rotector/internal/worker/queue"
	"github.com/robalyx/rotector/internal/worker/stats"
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
		},
	}

	return app.Run(context.Background(), os.Args)
}

// runWorkers starts multiple instances of a worker type.
func runWorkers(ctx context.Context, workerType string, count int64) {
	app, err := setup.InitializeApp(ctx, setup.ServiceWorker, WorkerLogDir)
	if err != nil {
		log.Fatalf("Failed to initialize application: %v", err)
	}
	defer app.Cleanup(ctx)

	// Initialize progress bars
	bars := make([]*progress.Bar, count)
	for i := range count {
		bars[i] = progress.NewBar(100, 25, fmt.Sprintf("Worker %d", i))
	}

	// Create and start the renderer
	renderer := progress.NewRenderer(bars)
	go renderer.Render()

	// Get startup delay from config
	startupDelay := app.Config.Worker.StartupDelay
	if startupDelay <= 0 {
		startupDelay = 2000 // Default to 2000ms if not configured
	}

	// Start workers
	var wg sync.WaitGroup
	for i := range count {
		wg.Add(1)
		go func(workerID int64) {
			defer wg.Done()

			// Add staggered startup delay
			delay := time.Duration(workerID) * time.Duration(startupDelay) * time.Millisecond
			select {
			case <-time.After(delay):
				// Proceed after delay
			case <-ctx.Done():
				return // Exit if context cancelled during delay
			}

			workerLogger := app.LogManager.GetWorkerLogger(
				fmt.Sprintf("%s_worker_%d", workerType, workerID),
			)

			// Get progress bar for this worker
			bar := bars[workerID]

			var w interface{ Start() }
			switch workerType {
			case FriendWorker:
				w = friend.New(app, bar, workerLogger)
			case GroupWorker:
				w = group.New(app, bar, workerLogger)
			case MaintenanceWorker:
				w = maintenance.New(app, bar, workerLogger)
			case StatsWorker:
				w = stats.New(app, bar, workerLogger)
			case QueueWorker:
				w = queue.New(app, bar, workerLogger)
			default:
				log.Fatalf("Invalid worker type: %s", workerType)
			}

			runWorker(ctx, w, workerLogger)
		}(i)
	}

	log.Printf("Started %d %s workers", count, workerType)
	wg.Wait()
	renderer.Stop()
	log.Println("All workers have finished. Exiting.")
}

// runWorker runs a single worker in a loop with error recovery.
func runWorker(ctx context.Context, w interface{ Start() }, logger *zap.Logger) {
	for {
		select {
		case <-ctx.Done():
			logger.Info("Context cancelled, stopping worker")
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
						time.Sleep(5 * time.Second)
					}
				}()

				logger.Info("Starting worker")
				w.Start()
			}()

			logger.Warn("Worker stopped unexpectedly",
				zap.String("worker_type", fmt.Sprintf("%T", w)),
			)
			time.Sleep(5 * time.Second)
		}
	}
}
