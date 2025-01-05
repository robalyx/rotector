package logger

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/rotector/rotector/internal/common/setup/config"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Manager handles the creation and management of log files and directories.
// It maintains both timestamped session logs and a "latest" symlink for easy access.
type Manager struct {
	currentSessionDir string // Path to the current session's log directory
	logDir            string // Base directory for all logs
	level             string // Logging level (debug, info, warn, error)
	maxLogsToKeep     int    // Maximum number of log sessions to retain
	maxLogLines       int    // Maximum number of lines to keep in each log file
}

// NewManager creates a new Manager instance.
func NewManager(logDir string, cfg *config.Debug) *Manager {
	return &Manager{
		logDir:        logDir,
		level:         cfg.LogLevel,
		maxLogsToKeep: cfg.MaxLogsToKeep,
		maxLogLines:   cfg.MaxLogLines,
	}
}

// GetLoggers initializes the main and database loggers.
// Returns separate loggers for main application and database logging.
// Both loggers write to their respective files in both the session and latest directories.
func (lm *Manager) GetLoggers() (*zap.Logger, *zap.Logger, error) {
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
func (lm *Manager) GetWorkerLogger(name string) *zap.Logger {
	zapLevel, err := zapcore.ParseLevel(lm.level)
	if err != nil {
		return zap.NewNop()
	}

	sessionDir := lm.getOrCreateSessionDir()
	latestDir := filepath.Join(lm.logDir, "latest")

	// Configure the logger
	config := zap.NewDevelopmentConfig()
	config.OutputPaths = []string{
		filepath.Join(sessionDir, name+".log"),
		filepath.Join(latestDir, name+".log"),
	}
	config.Level = zap.NewAtomicLevelAt(zapLevel)

	// Create the logger
	logger, err := config.Build()
	if err != nil {
		return zap.NewNop()
	}

	// Add Sentry core if Sentry client is initialized
	if sentry.CurrentHub().Client() != nil {
		sentryLevel := zap.LevelEnablerFunc(func(lvl zapcore.Level) bool {
			return lvl >= zapcore.ErrorLevel
		})
		logger = logger.WithOptions(zap.WrapCore(func(core zapcore.Core) zapcore.Core {
			return zapcore.NewTee(core, NewSentryCore(sentryLevel))
		}))
	}

	return logger
}

// setupLogDirectories creates and manages the log directory structure.
// It ensures the base directory exists, rotates old logs, and sets up the latest directory.
func (lm *Manager) setupLogDirectories() error {
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
	// Ignore errors when removing old latest directory
	// since it may be written to by another process
	_ = os.RemoveAll(latestDir)

	if err := os.MkdirAll(latestDir, os.ModePerm); err != nil {
		return fmt.Errorf("failed to create latest directory: %w", err)
	}

	return nil
}

// getOrCreateSessionDir returns the current session directory or creates a new one.
// Falls back to base log directory if creation fails.
func (lm *Manager) getOrCreateSessionDir() string {
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
func (lm *Manager) initLogger(logPaths []string) (*zap.Logger, error) {
	zapLevel, err := zapcore.ParseLevel(lm.level)
	if err != nil {
		return nil, fmt.Errorf("invalid log level: %w", err)
	}

	// Create custom writer for each output path
	cores := make([]zapcore.Core, 0, len(logPaths))
	encoderConfig := zap.NewDevelopmentEncoderConfig()
	encoderConfig.EncodeCaller = zapcore.ShortCallerEncoder

	for _, path := range logPaths {
		file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return nil, fmt.Errorf("cannot open log file %s: %w", path, err)
		}

		// Create log rotator
		logRotator := NewLogRotator(file, lm.maxLogLines, path)

		// Create custom core with our writer
		core := zapcore.NewCore(
			zapcore.NewConsoleEncoder(encoderConfig),
			zapcore.AddSync(logRotator),
			zapLevel,
		)
		cores = append(cores, core)
	}

	// Add Sentry core if Sentry client is initialized
	if sentry.CurrentHub().Client() != nil {
		sentryLevel := zap.LevelEnablerFunc(func(lvl zapcore.Level) bool {
			return lvl >= zapcore.ErrorLevel
		})
		cores = append(cores, NewSentryCore(sentryLevel))
	}

	// Create logger with all cores and development options
	return zap.New(
		zapcore.NewTee(cores...),
		zap.AddCaller(),
		zap.AddStacktrace(zapcore.ErrorLevel),
		zap.Development(),
	), nil
}

// rotateLogSessions maintains the log directory by removing old sessions.
// Keeps only the most recent sessions based on maxLogsToKeep.
func (lm *Manager) rotateLogSessions() error {
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
