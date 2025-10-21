package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/robalyx/rotector/internal/cloudflare/manager"
	"github.com/robalyx/rotector/internal/database/types"
	"github.com/robalyx/rotector/internal/setup"
	"github.com/robalyx/rotector/internal/setup/telemetry"
	"github.com/robalyx/rotector/pkg/utils"
	"github.com/urfave/cli/v3"
	"go.uber.org/zap"
)

const (
	// QueueLogDir specifies where queue log files are stored.
	QueueLogDir = "logs/queue_logs"
)

var (
	// ErrEmptyLine indicates an empty line was provided.
	ErrEmptyLine = errors.New("empty line")
	// ErrInvalidUserID indicates an invalid user ID was provided.
	ErrInvalidUserID = errors.New("invalid user ID")
	// ErrNoSuitableEditor indicates no suitable editor was found on the system.
	ErrNoSuitableEditor = errors.New("no suitable editor found")
	// ErrUnsupportedOS indicates the operating system is not supported.
	ErrUnsupportedOS = errors.New("unsupported operating system")
)

func main() {
	if err := run(); err != nil {
		log.Printf("Error: %v", err)
		os.Exit(1)
	}
}

func run() error {
	app := &cli.Command{
		Name:  "queue",
		Usage: "Queue Roblox users for processing",
		Action: func(ctx context.Context, _ *cli.Command) error {
			// Initialize application with required dependencies
			app, err := setup.InitializeApp(ctx, telemetry.ServiceQueue, QueueLogDir)
			if err != nil {
				return fmt.Errorf("failed to initialize application: %w", err)
			}
			defer app.Cleanup(ctx)

			// Create temporary file with instructions
			tempFile, err := createTempFileWithInstructions()
			if err != nil {
				return fmt.Errorf("failed to create temporary file: %w", err)
			}
			defer os.Remove(tempFile) // Clean up when done

			fmt.Printf("Created temporary file: %s\n", tempFile)
			fmt.Println("Please add Roblox user IDs to the file (one per line).")
			fmt.Println("User IDs can be numeric IDs or profile URLs.")
			fmt.Println("Press Enter when you're done editing the file...")

			// Try to open the file in the default editor
			if err := openFileInEditor(ctx, tempFile); err != nil {
				fmt.Printf("Warning: Could not open file in editor: %v\n", err)
				fmt.Println("Please manually edit the file and press Enter when done.")
			}

			// Wait for user input
			_, _ = fmt.Scanln()

			// Read and process the file
			userIDs, err := readUserIDsFromFile(tempFile)
			if err != nil {
				return fmt.Errorf("failed to read user IDs: %w", err)
			}

			if len(userIDs) == 0 {
				fmt.Println("No valid user IDs found in the file.")
				return nil
			}

			fmt.Printf("Found %d user IDs to queue.\n", len(userIDs))

			// Queue users in batches
			totalQueued, totalFailed, err := queueUsers(ctx, app, userIDs)
			if err != nil {
				return fmt.Errorf("failed to queue users: %w", err)
			}

			fmt.Printf("Successfully queued %d users.\n", totalQueued)

			if totalFailed > 0 {
				fmt.Printf("Failed to queue %d users (see logs for details).\n", totalFailed)
			}

			return nil
		},
	}

	return app.Run(context.Background(), os.Args)
}

// createTempFileWithInstructions creates a temporary file with instructions for the user.
func createTempFileWithInstructions() (string, error) {
	tempDir := os.TempDir()
	tempFile := filepath.Join(tempDir, fmt.Sprintf("rotector_queue_%d.txt", time.Now().Unix()))

	instructions := `# Rotector User Queue File
# 
# Instructions:
# - Add one Roblox user ID per line
# - You can use numeric IDs (e.g., 1234567890) or profile URLs (e.g., https://www.roblox.com/users/1234567890/profile)
# - Lines starting with # are comments and will be ignored
# - Empty lines will be ignored
#
# Example:
# 1234567890
# https://www.roblox.com/users/9876543210/profile
# 5555555555
#
# Add your user IDs below this line:

`

	if err := os.WriteFile(tempFile, []byte(instructions), 0o600); err != nil {
		return "", err
	}

	return tempFile, nil
}

// openFileInEditor tries to open the file in the system's default text editor.
func openFileInEditor(ctx context.Context, filename string) error {
	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "windows":
		cmd = exec.CommandContext(ctx, "notepad", filename)
	case "darwin":
		cmd = exec.CommandContext(ctx, "open", "-t", filename)
	case "linux":
		// Try common editors
		editors := []string{"nano", "vim", "vi"}
		for _, editor := range editors {
			if _, err := exec.LookPath(editor); err == nil {
				cmd = exec.CommandContext(ctx, editor, filename)
				break
			}
		}

		if cmd == nil {
			return ErrNoSuitableEditor
		}
	default:
		return fmt.Errorf("%w: %s", ErrUnsupportedOS, runtime.GOOS)
	}

	return cmd.Start()
}

// readUserIDsFromFile reads and parses user IDs from the temporary file.
func readUserIDsFromFile(filename string) ([]int64, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var (
		userIDs       []int64
		invalidInputs []string
	)

	processedLines := make(map[string]struct{})

	scanner := bufio.NewScanner(file)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		// Skip comments and empty lines
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Skip duplicate lines
		if _, exists := processedLines[line]; exists {
			continue
		}

		processedLines[line] = struct{}{}

		userID, err := processUserIDInput(line)
		if err != nil {
			invalidInputs = append(invalidInputs, fmt.Sprintf("line %d: %s", lineNum, line))
			continue
		}

		userIDs = append(userIDs, userID)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	if len(invalidInputs) > 0 {
		fmt.Printf("Warning: Found %d invalid entries:\n", len(invalidInputs))

		for _, invalid := range invalidInputs {
			fmt.Printf("  - %s\n", invalid)
		}

		fmt.Println()
	}

	return userIDs, nil
}

// processUserIDInput processes a single line of input and returns the parsed user ID if valid.
func processUserIDInput(line string) (int64, error) {
	line = strings.TrimSpace(line)
	if line == "" {
		return 0, ErrEmptyLine
	}

	// Parse profile URL if provided
	userIDStr := line

	parsedURL, err := utils.ExtractUserIDFromURL(line)
	if err == nil {
		userIDStr = parsedURL
	}

	// Parse user ID
	userID, err := strconv.ParseInt(userIDStr, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%w: %s", ErrInvalidUserID, line)
	}

	if userID <= 0 {
		return 0, fmt.Errorf("%w: %s", ErrInvalidUserID, line)
	}

	return userID, nil
}

// queueUsers queues multiple users for processing, handling batching and error reporting.
func queueUsers(ctx context.Context, app *setup.App, userIDs []int64) (int, int, error) {
	var (
		totalQueued int
		totalFailed int
	)

	logger := app.Logger.Named("queue_cli")

	for i := 0; i < len(userIDs); i += manager.MaxQueueBatchSize {
		end := min(i+manager.MaxQueueBatchSize, len(userIDs))
		batch := userIDs[i:end]

		queuedCount, failedCount, err := queueUserBatch(ctx, app, batch, logger)
		if err != nil {
			return totalQueued, totalFailed, err
		}

		totalQueued += queuedCount
		totalFailed += failedCount

		fmt.Printf("Processed batch %d-%d: %d queued, %d failed\n", i+1, end, queuedCount, failedCount)
	}

	return totalQueued, totalFailed, nil
}

// queueUserBatch queues a batch of users and returns the results.
func queueUserBatch(ctx context.Context, app *setup.App, batch []int64, logger *zap.Logger) (int, int, error) {
	// Check which users already exist in database
	existingUsers, err := app.DB.Service().User().GetUsersByIDs(ctx, batch, types.UserFieldBasic)
	if err != nil {
		logger.Error("Failed to check existing users in database", zap.Error(err))
		return 0, len(batch), err
	}

	// Filter out users that already exist
	existingUserSet := make(map[int64]struct{})
	usersToQueue := make([]int64, 0, len(batch))

	for _, userID := range batch {
		if _, exists := existingUsers[userID]; exists {
			existingUserSet[userID] = struct{}{}
			logger.Debug("Skipping user - already exists in database", zap.Int64("userID", userID))

			continue
		}

		usersToQueue = append(usersToQueue, userID)
	}

	// Queue the remaining users in D1 if any
	queueErrors := make(map[int64]error)
	if len(usersToQueue) > 0 {
		var err error

		queueErrors, err = app.CFClient.Queue.AddUsers(ctx, usersToQueue)
		if err != nil {
			logger.Error("Failed to queue batch", zap.Error(err))
			return 0, len(batch), err
		}
	}

	// Process results and create activity logs
	var (
		queuedCount int
		failedCount int
	)

	for _, userID := range batch {
		// Skip if user exists in database
		if _, existsInDB := existingUserSet[userID]; existsInDB {
			failedCount++
			continue
		}

		// Check if user had D1 queue errors
		if queueErr, failed := queueErrors[userID]; failed {
			failedCount++

			if errors.Is(queueErr, manager.ErrUserRecentlyQueued) {
				logger.Warn("User recently queued", zap.Int64("userID", userID))
			} else {
				logger.Error("Failed to queue user", zap.Int64("userID", userID), zap.Error(queueErr))
			}

			continue
		}

		queuedCount++
	}

	return queuedCount, failedCount, nil
}
