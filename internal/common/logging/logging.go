package logging

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// SetupLogging initializes the logging system.
func SetupLogging(logDir string, level string, maxLogsToKeep int) (*zap.Logger, *zap.Logger, error) {
	// Create logs directory if it doesn't exist
	err := os.MkdirAll(logDir, os.ModePerm)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create logs directory: %w", err)
	}

	// Rotate log sessions
	err = rotateLogSessions(logDir, maxLogsToKeep)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to rotate log sessions: %w", err)
	}

	// Create a new session directory
	sessionDir := filepath.Join(logDir, time.Now().Format("2006-01-02_15-04-05"))
	err = os.MkdirAll(sessionDir, os.ModePerm)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create session directory: %w", err)
	}

	// Initialize main logger
	mainLogger, err := initLogger(filepath.Join(sessionDir, "main.log"), level)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to initialize main logger: %w", err)
	}

	// Initialize database logger
	dbLogger, err := initLogger(filepath.Join(sessionDir, "database.log"), level)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to initialize database logger: %w", err)
	}

	return mainLogger, dbLogger, nil
}

// initLogger creates a new logger instance.
func initLogger(logPath string, level string) (*zap.Logger, error) {
	// Parse the log level
	zapLevel, err := zapcore.ParseLevel(level)
	if err != nil {
		return nil, fmt.Errorf("invalid log level: %w", err)
	}

	// Create a new logger with the development config
	config := zap.NewDevelopmentConfig()
	config.OutputPaths = []string{logPath}
	config.Level = zap.NewAtomicLevelAt(zapLevel)

	// Build the logger
	logger, err := config.Build()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize logger: %w", err)
	}

	return logger, nil
}

// GetWorkerLogger creates a logger for a specific worker.
func GetWorkerLogger(name string, logDir string, level string) *zap.Logger {
	// Parse the log level
	zapLevel, err := zapcore.ParseLevel(level)
	if err != nil {
		return zap.NewNop()
	}

	// Get the latest session directory
	sessionDir := getLatestSessionDir(logDir)

	// Create a new logger with the development config
	config := zap.NewDevelopmentConfig()
	config.OutputPaths = []string{
		filepath.Join(sessionDir, name+".log"),
	}
	config.Level = zap.NewAtomicLevelAt(zapLevel)

	// Build the logger
	logger, err := config.Build()
	if err != nil {
		return zap.NewNop()
	}

	return logger
}

// rotateLogSessions keeps only the most recent log sessions.
func rotateLogSessions(logDir string, maxLogsToKeep int) error {
	// Get all the sessions in the log directory
	sessions, err := filepath.Glob(filepath.Join(logDir, "*"))
	if err != nil {
		return err
	}

	// If we have less than the max logs to keep, we don't need to rotate
	if len(sessions) <= maxLogsToKeep {
		return nil
	}

	// Sort sessions by modification time (oldest first)
	sort.Slice(sessions, func(i, j int) bool {
		iInfo, _ := os.Stat(sessions[i])
		jInfo, _ := os.Stat(sessions[j])
		return iInfo.ModTime().Before(jInfo.ModTime())
	})

	// Remove oldest sessions
	for i := range len(sessions) - maxLogsToKeep {
		err := os.RemoveAll(sessions[i])
		if err != nil {
			return err
		}
	}

	return nil
}

// getLatestSessionDir returns the path to the most recent session directory.
func getLatestSessionDir(logDir string) string {
	// Get all the sessions in the log directory
	sessions, err := filepath.Glob(filepath.Join(logDir, "*"))
	if err != nil || len(sessions) == 0 {
		return logDir // Fallback to main log directory if we can't find sessions
	}

	// Sort sessions by modification time (newest first)
	sort.Slice(sessions, func(i, j int) bool {
		iInfo, _ := os.Stat(sessions[i])
		jInfo, _ := os.Stat(sessions[j])
		return iInfo.ModTime().After(jInfo.ModTime())
	})

	return sessions[0]
}
