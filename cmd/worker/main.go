package main

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/rotector/rotector/internal/common/logging"
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
	WorkerLogDir = "logs/worker_logs"

	AIWorker           = "ai"
	AIWorkerTypeFriend = "friend"
	AIWorkerTypeMember = "member"

	PurgeWorker             = "purge"
	PurgeWorkerTypeBanned   = "banned"
	PurgeWorkerTypeCleared  = "cleared"
	PurgeWorkerTypeTracking = "tracking"

	StatsWorker           = "stats"
	StatsWorkerTypeUpload = "upload"

	QueueWorker            = "queue"
	QueueWorkerTypeProcess = "process"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		log.Fatalf("Failed to execute root command: %v", err)
	}
}

// newRootCmd creates a new root command.
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

// newAIWorkerCmd creates a new AI worker command.
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

// newPurgeWorkerCmd creates a new purge worker command.
func newPurgeWorkerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   PurgeWorker,
		Short: "Start purge workers",
		Long:  `Start purge workers, which can be banned, cleared, or tracking workers.`,
	}

	cmd.AddCommand(
		&cobra.Command{
			Use:   PurgeWorkerTypeBanned,
			Short: "Start banned user purge workers",
			Run: func(cmd *cobra.Command, _ []string) {
				count, _ := cmd.Flags().GetInt("workers")
				runWorkers(PurgeWorker, PurgeWorkerTypeBanned, count)
			},
		},
		&cobra.Command{
			Use:   PurgeWorkerTypeCleared,
			Short: "Start cleared user purge workers",
			Run: func(cmd *cobra.Command, _ []string) {
				count, _ := cmd.Flags().GetInt("workers")
				runWorkers(PurgeWorker, PurgeWorkerTypeCleared, count)
			},
		},
		&cobra.Command{
			Use:   PurgeWorkerTypeTracking,
			Short: "Start tracking purge workers",
			Run: func(cmd *cobra.Command, _ []string) {
				count, _ := cmd.Flags().GetInt("workers")
				runWorkers(PurgeWorker, PurgeWorkerTypeTracking, count)
			},
		},
	)

	return cmd
}

// newStatsWorkerCmd creates a new statistics worker command.
func newStatsWorkerCmd() *cobra.Command {
	return &cobra.Command{
		Use:   StatsWorker,
		Short: "Start statistics worker",
		Run: func(_ *cobra.Command, _ []string) {
			runWorkers(StatsWorker, StatsWorkerTypeUpload, 1)
		},
	}
}

// newQueueWorkerCmd creates a new queue worker command.
func newQueueWorkerCmd() *cobra.Command {
	return &cobra.Command{
		Use:   QueueWorker,
		Short: "Start queue process worker",
		Run: func(cmd *cobra.Command, _ []string) {
			count, _ := cmd.Flags().GetInt("workers")
			runWorkers(QueueWorker, QueueWorkerTypeProcess, count)
		},
	}
}

// runWorkers starts the specified number of workers of the given type.
func runWorkers(workerType, subType string, count int) {
	setup, err := setup.InitializeApp(WorkerLogDir)
	if err != nil {
		log.Fatalf("Failed to initialize application: %v", err)
	}
	defer setup.CleanupApp()

	var wg sync.WaitGroup
	logLevel := setup.Config.Logging.Level

	// Initialize progress bars
	bars := make([]*progress.Bar, count)
	for i := range count {
		bars[i] = progress.NewBar(100, 25, fmt.Sprintf("Worker %d", i))
	}

	// Create and start the renderer
	renderer := progress.NewRenderer(bars)
	go renderer.Render()

	// Start workers
	for i := range count {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			workerLogger := logging.GetWorkerLogger(
				fmt.Sprintf("%s_%s_worker_%d", workerType, subType, workerID),
				WorkerLogDir,
				logLevel,
			)

			// Get progress bar for this worker
			bar := bars[workerID]

			var w interface{ Start() }
			switch {
			case workerType == AIWorker && subType == AIWorkerTypeMember:
				w = ai.NewMemberWorker(setup.DB, setup.OpenAIClient, setup.RoAPI, bar, workerLogger)
			case workerType == AIWorker && subType == AIWorkerTypeFriend:
				w = ai.NewFriendWorker(setup.DB, setup.OpenAIClient, setup.RoAPI, bar, workerLogger)
			case workerType == PurgeWorker && subType == PurgeWorkerTypeBanned:
				w = purge.NewBannedWorker(setup.DB, setup.RoAPI, bar, workerLogger)
			case workerType == PurgeWorker && subType == PurgeWorkerTypeCleared:
				w = purge.NewClearedWorker(setup.DB, setup.RoAPI, bar, workerLogger)
			case workerType == PurgeWorker && subType == PurgeWorkerTypeTracking:
				w = purge.NewTrackingWorker(setup.DB, setup.RoAPI, bar, workerLogger)
			case workerType == StatsWorker:
				w = stats.NewStatisticsWorker(setup.DB, bar, workerLogger)
			case workerType == QueueWorker:
				w = queue.NewProcessWorker(setup.DB, setup.OpenAIClient, setup.RoAPI, setup.Queue, bar, workerLogger)
			default:
				log.Fatalf("Invalid worker type: %s %s", workerType, subType)
			}

			runWorker(w, workerLogger)
		}(i)
	}

	log.Printf("Started %d %s %s workers", count, workerType, subType)

	// Wait for all workers to finish
	wg.Wait()

	// Stop the renderer
	renderer.Stop()

	log.Println("All workers have finished. Exiting.")
}

// runWorker runs a worker in a loop, restarting it if it stops unexpectedly.
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
