package main

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/rotector/rotector/internal/common/logging"
	"github.com/rotector/rotector/internal/common/setup"
	"github.com/rotector/rotector/internal/worker/user"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

const (
	WorkerLogDir = "logs/worker_logs"

	UserWorker           = "user"
	UserWorkerTypeGroup  = "group"
	UserWorkerTypeFriend = "friend"
	GroupWorker          = "group"
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
		Long:  `This command starts the rotector worker, which can be either a group worker or a user worker.`,
	}
	rootCmd.PersistentFlags().IntP("workers", "w", 1, "Number of workers to start")
	rootCmd.AddCommand(newUserCmd())
	rootCmd.AddCommand(newGroupCmd())

	return rootCmd
}

// newUserCmd creates a new user worker command.
func newUserCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   UserWorker,
		Short: "Start user workers",
		Long:  `Start user workers, which can be either friend or group workers.`,
	}

	cmd.AddCommand(
		&cobra.Command{
			Use:   UserWorkerTypeFriend,
			Short: "Start friend workers",
			Run: func(cmd *cobra.Command, _ []string) {
				count, _ := cmd.Flags().GetInt("workers")
				runWorkers(UserWorker, UserWorkerTypeFriend, count)
			},
		},
		&cobra.Command{
			Use:   UserWorkerTypeGroup,
			Short: "Start group workers",
			Run: func(cmd *cobra.Command, _ []string) {
				count, _ := cmd.Flags().GetInt("workers")
				runWorkers(UserWorker, UserWorkerTypeGroup, count)
			},
		},
	)

	return cmd
}

// newGroupCmd creates a new group worker command.
func newGroupCmd() *cobra.Command {
	return &cobra.Command{
		Use:   GroupWorker,
		Short: "Start group workers",
		Run: func(_ *cobra.Command, _ []string) {
			log.Println("Group worker functionality not implemented yet.")
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

			var w interface{ Start() }
			switch {
			case workerType == UserWorker && subType == UserWorkerTypeGroup:
				w = user.NewGroupWorker(setup.DB, setup.OpenAIClient, setup.RoAPI, workerLogger)
			case workerType == UserWorker && subType == UserWorkerTypeFriend:
				w = user.NewFriendWorker(setup.DB, setup.OpenAIClient, setup.RoAPI, workerLogger)
			default:
				log.Fatalf("Invalid worker type: %s %s", workerType, subType)
			}

			runWorker(w, workerLogger)
		}(i)
	}

	log.Printf("Started %d %s %s workers", count, workerType, subType)

	// Wait for all workers to finish
	wg.Wait()

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
