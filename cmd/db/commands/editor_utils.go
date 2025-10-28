package commands

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/robalyx/rotector/internal/database"
	"github.com/robalyx/rotector/internal/database/types"
	"github.com/robalyx/rotector/internal/database/types/enum"
)

var (
	ErrEmptyLine        = errors.New("empty line")
	ErrInvalidID        = errors.New("invalid ID")
	ErrNoSuitableEditor = errors.New("no suitable editor found")
	ErrUnsupportedOS    = errors.New("unsupported operating system")
	ErrInvalidStatus    = errors.New("invalid status: must be 'flagged', 'confirmed', or 'both'")
)

// createTempFile creates a temporary file with instructions for the user.
func createTempFile(prefix, instructions string) (string, error) {
	tempDir := os.TempDir()
	tempFile := filepath.Join(tempDir, fmt.Sprintf("%s_%d.txt", prefix, time.Now().Unix()))

	if err := os.WriteFile(tempFile, []byte(instructions), 0o600); err != nil {
		return "", err
	}

	return tempFile, nil
}

// openFileInEditor opens the file in the system's default text editor.
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

// readIDsFromFile reads and parses int64 IDs from the file.
func readIDsFromFile(filename string) ([]int64, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var (
		ids           []int64
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

		id, err := processIDInput(line)
		if err != nil {
			invalidInputs = append(invalidInputs, fmt.Sprintf("line %d: %s", lineNum, line))
			continue
		}

		ids = append(ids, id)
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

	return ids, nil
}

// processIDInput processes a single line of input and returns the parsed ID if valid.
func processIDInput(line string) (int64, error) {
	line = strings.TrimSpace(line)
	if line == "" {
		return 0, ErrEmptyLine
	}

	// Parse ID
	id, err := strconv.ParseInt(line, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%w: %s", ErrInvalidID, line)
	}

	if id <= 0 {
		return 0, fmt.Errorf("%w: %s", ErrInvalidID, line)
	}

	return id, nil
}

// parseStatusFilter parses the status filter string into UserType enums.
func parseStatusFilter(status string) ([]enum.UserType, error) {
	switch strings.ToLower(status) {
	case "flagged":
		return []enum.UserType{enum.UserTypeFlagged}, nil
	case "confirmed":
		return []enum.UserType{enum.UserTypeConfirmed}, nil
	case "both":
		return []enum.UserType{enum.UserTypeFlagged, enum.UserTypeConfirmed}, nil
	default:
		return nil, fmt.Errorf("%w: %q", ErrInvalidStatus, status)
	}
}

// getUsersByStatuses gets users by their IDs filtered by multiple statuses.
func getUsersByStatuses(
	ctx context.Context, db database.Client, userIDs []int64, statuses []enum.UserType,
) ([]*types.User, error) {
	if len(userIDs) == 0 {
		return []*types.User{}, nil
	}

	// Get users by IDs with basic fields
	userMap, err := db.Model().User().GetUsersByIDs(ctx, userIDs, types.UserFieldBasic)
	if err != nil {
		return nil, fmt.Errorf("failed to get users by IDs: %w", err)
	}

	// Create status map for efficient lookup
	statusSet := make(map[enum.UserType]bool)
	for _, status := range statuses {
		statusSet[status] = true
	}

	// Filter by status and convert to simple User slice
	var result []*types.User

	for _, reviewUser := range userMap {
		if reviewUser.User != nil && statusSet[reviewUser.Status] {
			result = append(result, reviewUser.User)
		}
	}

	return result, nil
}
