package setup

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/jaxron/axonet/pkg/client/logger"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Logger adapts zap.Logger to implement the axonet logger.Logger interface.
type Logger struct {
	zap *zap.Logger
}

// NewLogger creates a new Logger instance that wraps a zap.Logger.
// This adapter allows zap.Logger to be used with the axonet logging interface.
func NewLogger(zapLogger *zap.Logger) logger.Logger {
	return &Logger{zap: zapLogger}
}

func (l *Logger) Debug(msg string)                          { l.zap.Debug(msg) }
func (l *Logger) Info(msg string)                           { l.zap.Info(msg) }
func (l *Logger) Warn(msg string)                           { l.zap.Warn(msg) }
func (l *Logger) Error(msg string)                          { l.zap.Error(msg) }
func (l *Logger) Debugf(format string, args ...interface{}) { l.zap.Sugar().Debugf(format, args...) }
func (l *Logger) Infof(format string, args ...interface{})  { l.zap.Sugar().Infof(format, args...) }
func (l *Logger) Warnf(format string, args ...interface{})  { l.zap.Sugar().Warnf(format, args...) }
func (l *Logger) Errorf(format string, args ...interface{}) { l.zap.Sugar().Errorf(format, args...) }

// LogManager handles the creation and management of log files and directories.
// It maintains both timestamped session logs and a "latest" symlink for easy access.
type LogManager struct {
	currentSessionDir string // Path to the current session's log directory
	logDir            string // Base directory for all logs
	level             string // Logging level (debug, info, warn, error)
	maxLogsToKeep     int    // Maximum number of log sessions to retain
}

// NewLogManager creates a new LogManager instance.
func NewLogManager(logDir string, level string, maxLogsToKeep int) *LogManager {
	return &LogManager{
		logDir:        logDir,
		level:         level,
		maxLogsToKeep: maxLogsToKeep,
	}
}

// WithFields creates a new logger with additional context fields.
// It converts axonet fields to zap fields and creates a new logger instance.
func (l *Logger) WithFields(fields ...logger.Field) logger.Logger {
	zapFields := make([]zap.Field, len(fields))
	for i, f := range fields {
		zapFields[i] = zap.Any(f.Key, f.Value)
	}
	return &Logger{zap: l.zap.With(zapFields...)}
}

// GetLoggers initializes the main and database loggers.
// Returns separate loggers for main application and database logging.
// Both loggers write to their respective files in both the session and latest directories.
func (lm *LogManager) GetLoggers() (*zap.Logger, *zap.Logger, error) {
	if err := lm.setupLogDirectories(); err != nil {
		return nil, nil, err
	}

	// Initialize main application logger
	mainLogger, err := lm.initLogger([]string{
		filepath.Join(lm.currentSessionDir, "main.log"),
		filepath.Join(lm.logDir, "latest", "main.log"),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to initialize main logger: %w", err)
	}

	// Initialize database logger
	dbLogger, err := lm.initLogger([]string{
		filepath.Join(lm.currentSessionDir, "database.log"),
		filepath.Join(lm.logDir, "latest", "database.log"),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to initialize database logger: %w", err)
	}

	return mainLogger, dbLogger, nil
}

// GetWorkerLogger creates a logger for background workers.
// Each worker gets its own log file in both the session and latest directories.
// Returns a no-op logger if initialization fails.
func (lm *LogManager) GetWorkerLogger(name string) *zap.Logger {
	zapLevel, err := zapcore.ParseLevel(lm.level)
	if err != nil {
		return zap.NewNop() // Return no-op logger on invalid level
	}

	sessionDir := lm.getOrCreateSessionDir()
	latestDir := filepath.Join(lm.logDir, "latest")

	// Configure the logger with both session and latest outputs
	config := zap.NewDevelopmentConfig()
	config.OutputPaths = []string{
		filepath.Join(sessionDir, name+".log"),
		filepath.Join(latestDir, name+".log"),
	}
	config.Level = zap.NewAtomicLevelAt(zapLevel)

	logger, err := config.Build()
	if err != nil {
		return zap.NewNop() // Return no-op logger on build failure
	}

	return logger
}

// setupLogDirectories creates and manages the log directory structure.
// It ensures the base directory exists, rotates old logs, and sets up the latest directory.
func (lm *LogManager) setupLogDirectories() error {
	// Ensure base log directory exists
	if err := os.MkdirAll(lm.logDir, os.ModePerm); err != nil {
		return fmt.Errorf("failed to create logs directory: %w", err)
	}

	// Clean up old log sessions
	if err := lm.rotateLogSessions(); err != nil {
		return fmt.Errorf("failed to rotate log sessions: %w", err)
	}

	// Create new session directory with timestamp
	lm.currentSessionDir = filepath.Join(lm.logDir, time.Now().Format("2006-01-02_15-04-05"))
	if err := os.MkdirAll(lm.currentSessionDir, os.ModePerm); err != nil {
		return fmt.Errorf("failed to create session directory: %w", err)
	}

	// Setup latest directory (remove old and create new)
	latestDir := filepath.Join(lm.logDir, "latest")
	if err := os.RemoveAll(latestDir); err != nil {
		return fmt.Errorf("failed to remove old latest directory: %w", err)
	}
	if err := os.MkdirAll(latestDir, os.ModePerm); err != nil {
		return fmt.Errorf("failed to create latest directory: %w", err)
	}

	return nil
}

// getOrCreateSessionDir returns the current session directory or creates a new one.
// Falls back to base log directory if creation fails.
func (lm *LogManager) getOrCreateSessionDir() string {
	if lm.currentSessionDir != "" {
		return lm.currentSessionDir
	}

	// Create new session directory if none exists
	sessionDir := filepath.Join(lm.logDir, time.Now().Format("2006-01-02_15-04-05"))
	if err := os.MkdirAll(sessionDir, os.ModePerm); err != nil {
		return lm.logDir // Fallback to base log directory
	}
	return sessionDir
}

// initLogger creates a new zap logger instance with the specified paths and level.
func (lm *LogManager) initLogger(logPaths []string) (*zap.Logger, error) {
	zapLevel, err := zapcore.ParseLevel(lm.level)
	if err != nil {
		return nil, fmt.Errorf("invalid log level: %w", err)
	}

	config := zap.NewDevelopmentConfig()
	config.OutputPaths = logPaths                 // Configure output paths
	config.Level = zap.NewAtomicLevelAt(zapLevel) // Set log level

	return config.Build()
}

// rotateLogSessions maintains the log directory by removing old sessions.
// Keeps only the most recent sessions based on maxLogsToKeep.
func (lm *LogManager) rotateLogSessions() error {
	sessions, err := filepath.Glob(filepath.Join(lm.logDir, "*"))
	if err != nil {
		return err
	}

	if len(sessions) <= lm.maxLogsToKeep {
		return nil // No rotation needed
	}

	// Sort sessions by modification time (oldest first)
	sort.Slice(sessions, func(i, j int) bool {
		iInfo, _ := os.Stat(sessions[i])
		jInfo, _ := os.Stat(sessions[j])
		return iInfo.ModTime().Before(jInfo.ModTime())
	})

	// Remove oldest sessions to maintain maxLogsToKeep
	for i := range len(sessions) - lm.maxLogsToKeep {
		if err := os.RemoveAll(sessions[i]); err != nil {
			return err
		}
	}

	return nil
}
