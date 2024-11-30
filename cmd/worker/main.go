package main

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/rotector/rotector/internal/common/progress"
	"github.com/rotector/rotector/internal/common/setup"
	"github.com/rotector/rotector/internal/worker/ai"
	"github.com/rotector/rotector/internal/worker/purge"
	"github.com/rotector/rotector/internal/worker/queue"
	"github.com/rotector/rotector/internal/worker/stats"
	"github.com/spf13/cobra"
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
	if err := newRootCmd().Execute(); err != nil {
		log.Fatalf("Failed to execute root command: %v", err)
	}
}

// newRootCmd creates the root command with subcommands for each worker type.
// The workers flag controls how many instances of each worker to start.
func newRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "worker",
		Short: "Start the rotector worker",
		Long:  `This command starts the rotector worker, which can be either a group worker, user worker, stats worker, or tracking worker.`,
	}
	rootCmd.PersistentFlags().IntP("workers", "w", 1, "Number of workers to start")
	rootCmd.AddCommand(newAIWorkerCmd())
	rootCmd.AddCommand(newPurgeWorkerCmd())
	rootCmd.AddCommand(newStatsWorkerCmd())
	rootCmd.AddCommand(newQueueWorkerCmd())

	return rootCmd
}

// newAIWorkerCmd creates subcommands for AI-based content analysis:
// - friend: analyzes user friend networks
// - member: analyzes group members.
func newAIWorkerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   AIWorker,
		Short: "Start AI workers",
		Long:  `Start AI workers, which can be friend or group workers.`,
	}

	cmd.AddCommand(
		&cobra.Command{
			Use:   AIWorkerTypeFriend,
			Short: "Start user friend workers",
			Run: func(cmd *cobra.Command, _ []string) {
				count, _ := cmd.Flags().GetInt("workers")
				runWorkers(AIWorker, AIWorkerTypeFriend, count)
			},
		},
		&cobra.Command{
			Use:   AIWorkerTypeMember,
			Short: "Start group member workers",
			Run: func(cmd *cobra.Command, _ []string) {
				count, _ := cmd.Flags().GetInt("workers")
				runWorkers(AIWorker, AIWorkerTypeMember, count)
			},
		},
	)

	return cmd
}

// newPurgeWorkerCmd creates subcommands for data cleanup:
// - banned: removes banned users
// - cleared: removes old cleared users
// - tracking: removes stale tracking data.
func newPurgeWorkerCmd() *cobra.Command {
	return &cobra.Command{
		Use:   PurgeWorker,
		Short: "Start purge workers",
		Run: func(_ *cobra.Command, _ []string) {
			runWorkers(PurgeWorker, "", 1)
		},
	}
}

// newStatsWorkerCmd creates a command for statistics management.
// Only one stats worker is needed to handle all statistics operations.
func newStatsWorkerCmd() *cobra.Command {
	return &cobra.Command{
		Use:   StatsWorker,
		Short: "Start statistics worker",
		Run: func(_ *cobra.Command, _ []string) {
			runWorkers(StatsWorker, "", 1)
		},
	}
}

// newQueueWorkerCmd creates a command for queue processing.
// Multiple workers can process the queue concurrently.
func newQueueWorkerCmd() *cobra.Command {
	return &cobra.Command{
		Use:   QueueWorker,
		Short: "Start queue process worker",
		Run: func(cmd *cobra.Command, _ []string) {
			count, _ := cmd.Flags().GetInt("workers")
			runWorkers(QueueWorker, "", count)
		},
	}
}

// runWorkers starts multiple instances of a worker type:
// 1. Initializes the application and creates progress bars
// 2. Starts the renderer to show progress
// 3. Launches workers in goroutines
// 4. Waits for all workers to finish.
func runWorkers(workerType, subType string, count int) {
	app, err := setup.InitializeApp(WorkerLogDir)
	if err != nil {
		log.Fatalf("Failed to initialize application: %v", err)
	}
	defer app.CleanupApp()

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
		go func(workerID int) {
			defer wg.Done()

			workerLogger := app.LogManager.GetWorkerLogger(
				fmt.Sprintf("%s_%s_worker_%d", workerType, subType, workerID),
			)

			// Get progress bar for this worker
			bar := bars[workerID]

			var w interface{ Start() }
			switch {
			case workerType == AIWorker && subType == AIWorkerTypeMember:
				w = ai.NewGroupWorker(app.DB, app.OpenAIClient, app.RoAPI, app.StatusClient, bar, &app.Config.Worker, workerLogger)
			case workerType == AIWorker && subType == AIWorkerTypeFriend:
				w = ai.NewFriendWorker(app.DB, app.OpenAIClient, app.RoAPI, app.StatusClient, bar, &app.Config.Worker, workerLogger)
			case workerType == PurgeWorker:
				w = purge.New(app.DB, app.RoAPI, app.StatusClient, bar, &app.Config.Worker, workerLogger)
			case workerType == StatsWorker:
				w = stats.New(app.DB, app.OpenAIClient, app.StatusClient, bar, workerLogger)
			case workerType == QueueWorker:
				w = queue.New(app.DB, app.OpenAIClient, app.RoAPI, app.Queue, app.StatusClient, bar, workerLogger)
			default:
				log.Fatalf("Invalid worker type: %s %s", workerType, subType)
			}

			runWorker(w, workerLogger)
		}(i)
	}

	log.Printf("Started %d %s %s workers", count, workerType, subType)
	wg.Wait()
	renderer.Stop()
	log.Println("All workers have finished. Exiting.")
}

// runWorker runs a single worker in a loop with error recovery:
// 1. Catches panics to prevent worker crashes
// 2. Restarts the worker after a delay if it stops
// 3. Logs any errors that occur.
func runWorker(w interface{ Start() }, logger *zap.Logger) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("Worker panicked", zap.Any("panic", r))
		}
	}()

	for {
		logger.Info("Starting worker")
		w.Start()
		logger.Error("Worker stopped unexpectedly. Restarting in 5 seconds...")
		time.Sleep(5 * time.Second)
	}
}
