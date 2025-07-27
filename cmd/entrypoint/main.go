// Package main provides the Docker container entrypoint
package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func main() {
	// Get environment variables with defaults
	runType := getEnvWithDefault("RUN_TYPE", "bot")
	workerType := getEnvWithDefault("WORKER_TYPE", "friend")
	workersCount := getEnvWithDefault("WORKERS_COUNT", "1")

	// Execute the appropriate binary based on RUN_TYPE
	switch runType {
	case "bot":
		execBinary("/app/bin/bot")
	case "worker":
		execBinary("/app/bin/worker", workerType, "--workers", workersCount)
	case "export":
		execBinary("/app/bin/export", os.Args[1:]...)
	case "db":
		execBinary("/app/bin/db", os.Args[1:]...)
	default:
		fmt.Fprintf(os.Stderr, "Invalid RUN_TYPE. Must be one of: 'bot', 'worker', 'export', 'db'\n")
		fmt.Fprintf(os.Stderr, "Usage:\n")
		fmt.Fprintf(os.Stderr, "  RUN_TYPE=worker WORKER_TYPE=<type> WORKERS_COUNT=<count>\n")
		fmt.Fprintf(os.Stderr, "  RUN_TYPE=<other_type>\n")
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
	cmd := exec.CommandContext(context.Background(), path, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to execute %s: %v\n", filepath.Base(path), err)
		os.Exit(1)
	}
}
