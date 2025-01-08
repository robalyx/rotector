package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/rotector/rotector/internal/common/progress"
	"github.com/rotector/rotector/internal/common/setup"
	"github.com/rotector/rotector/internal/worker/ai"
	"github.com/rotector/rotector/internal/worker/purge"
	"github.com/rotector/rotector/internal/worker/queue"
	"github.com/rotector/rotector/internal/worker/stats"
	"github.com/urfave/cli/v3"
	"go.uber.org/zap"
)

const (
	// WorkerLogDir specifies where worker log files are stored.
	WorkerLogDir = "logs/worker_logs"

	// AIWorker processes user content through AI analysis.
	AIWorker           = "ai"
	AIWorkerTypeFriend = "friend"
	AIWorkerTypeMember = "member"

	// PurgeWorker removes outdated or banned data from the system.
	PurgeWorker = "purge"

	// StatsWorker handles statistics aggregation and storage.
	StatsWorker = "stats"

	// QueueWorker manages the processing queue for user checks.
	QueueWorker = "queue"
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
				Name:  AIWorker,
				Usage: "Start AI workers",
				Commands: []*cli.Command{
					{
						Name:  AIWorkerTypeFriend,
						Usage: "Start user friend workers",
						Action: func(ctx context.Context, c *cli.Command) error {
							runWorkers(ctx, AIWorker, AIWorkerTypeFriend, c.Int("workers"))
							return nil
						},
					},
					{
						Name:  AIWorkerTypeMember,
						Usage: "Start group member workers",
						Action: func(ctx context.Context, c *cli.Command) error {
							runWorkers(ctx, AIWorker, AIWorkerTypeMember, c.Int("workers"))
							return nil
						},
					},
				},
			},
			{
				Name:  PurgeWorker,
				Usage: "Start purge workers",
				Action: func(ctx context.Context, c *cli.Command) error {
					runWorkers(ctx, PurgeWorker, "", c.Int("workers"))
					return nil
				},
			},
			{
				Name:  StatsWorker,
				Usage: "Start statistics worker",
				Action: func(ctx context.Context, c *cli.Command) error {
					runWorkers(ctx, StatsWorker, "", c.Int("workers"))
					return nil
				},
			},
			{
				Name:  QueueWorker,
				Usage: "Start queue process worker",
				Action: func(ctx context.Context, c *cli.Command) error {
					runWorkers(ctx, QueueWorker, "", c.Int("workers"))
					return nil
				},
			},
		},
	}

	return app.Run(context.Background(), os.Args)
}

// runWorkers starts multiple instances of a worker type.
func runWorkers(ctx context.Context, workerType, subType string, count int64) {
	app, err := setup.InitializeApp(ctx, WorkerLogDir)
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

	// Start workers
	var wg sync.WaitGroup
	for i := range count {
		wg.Add(1)
		go func(workerID int64) {
			defer wg.Done()

			workerLogger := app.LogManager.GetWorkerLogger(
				fmt.Sprintf("%s_%s_worker_%d", workerType, subType, workerID),
			)

			// Get progress bar for this worker
			bar := bars[workerID]

			var w interface{ Start() }
			switch {
			case workerType == AIWorker && subType == AIWorkerTypeMember:
				w = ai.NewGroupWorker(app, bar, workerLogger)
			case workerType == AIWorker && subType == AIWorkerTypeFriend:
				w = ai.NewFriendWorker(app, bar, workerLogger)
			case workerType == PurgeWorker:
				w = purge.New(app, bar, workerLogger)
			case workerType == StatsWorker:
				w = stats.New(app, bar, workerLogger)
			case workerType == QueueWorker:
				w = queue.New(app, bar, workerLogger)
			default:
				log.Fatalf("Invalid worker type: %s %s", workerType, subType)
			}

			runWorker(ctx, w, workerLogger)
		}(i)
	}

	log.Printf("Started %d %s %s workers", count, workerType, subType)
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
