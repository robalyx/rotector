// Package main provides the Docker container entrypoint
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func main() {
	// Get environment variables with defaults
	runType := getEnvWithDefault("RUN_TYPE", "bot")
	workerType := getEnvWithDefault("WORKER_TYPE", "ai")
	workerSubtype := os.Getenv("WORKER_SUBTYPE")
	workersCount := getEnvWithDefault("WORKERS_COUNT", "1")

	// Execute the appropriate binary based on RUN_TYPE
	switch runType {
	case "bot":
		execBinary("/app/bin/bot")
	case "worker":
		args := []string{workerType, "--workers", workersCount}
		if workerSubtype != "" {
			args = []string{workerType, workerSubtype, "--workers", workersCount}
		}
		execBinary("/app/bin/worker", args...)
	default:
		fmt.Fprintf(os.Stderr, "Invalid RUN_TYPE. Must be either 'bot' or 'worker'\n")
		fmt.Fprintf(os.Stderr, "Usage: RUN_TYPE=worker WORKER_TYPE=<type> [WORKER_SUBTYPE=<subtype>] WORKERS_COUNT=<count>\n")
		os.Exit(1)
	}
}

// getEnvWithDefault returns the environment variable value or the default if not set.
func getEnvWithDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// execBinary executes the specified binary with given arguments.
func execBinary(path string, args ...string) {
	cmd := exec.Command(path, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to execute %s: %v\n", filepath.Base(path), err)
		os.Exit(1)
	}
}
