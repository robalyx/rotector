package telemetry

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/robalyx/rotector/internal/setup/config"
	"github.com/robalyx/rotector/internal/setup/telemetry/logger"
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
// Both loggers write to their respective files in the session directory.
func (lm *Manager) GetLoggers() (*zap.Logger, *zap.Logger, error) {
	if err := lm.setupLogDirectories(); err != nil {
		return nil, nil, err
	}

	// Initialize main application logger
	mainLogger, err := lm.initLogger([]string{
		filepath.Join(lm.currentSessionDir, "main.log"),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to initialize main logger: %w", err)
	}

	// Initialize database logger
	dbLogger, err := lm.initLogger([]string{
		filepath.Join(lm.currentSessionDir, "database.log"),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to initialize database logger: %w", err)
	}

	return mainLogger, dbLogger, nil
}

// GetWorkerLogger creates a logger for background workers.
// Each worker gets its own log file in the session directory.
// Returns a no-op logger if initialization fails.
func (lm *Manager) GetWorkerLogger(name string) *zap.Logger {
	zapLevel, err := zapcore.ParseLevel(lm.level)
	if err != nil {
		return zap.NewNop()
	}

	sessionDir := lm.getOrCreateSessionDir()

	// Configure the logger
	zapConfig := zap.NewDevelopmentConfig()
	zapConfig.OutputPaths = []string{
		filepath.Join(sessionDir, name+".log"),
	}
	zapConfig.Level = zap.NewAtomicLevelAt(zapLevel)

	// Create the logger
	zapLogger, err := zapConfig.Build()
	if err != nil {
		return zap.NewNop()
	}

	// Add Sentry core if Sentry client is initialized
	if sentry.CurrentHub().Client() != nil {
		sentryLevel := zap.LevelEnablerFunc(func(lvl zapcore.Level) bool {
			return lvl >= zapcore.ErrorLevel
		})
		zapLogger = zapLogger.WithOptions(zap.WrapCore(func(core zapcore.Core) zapcore.Core {
			return zapcore.NewTee(core, NewSentryCore(sentryLevel))
		}))
	}

	return zapLogger
}

// GetCurrentSessionDir returns the current session directory.
// This is useful for external components that need to access logs in the same session.
func (lm *Manager) GetCurrentSessionDir() string {
	return lm.getOrCreateSessionDir()
}

// GetImageLogger creates a logger specifically for handling image logging.
// It creates a dedicated image directory within the current session directory.
func (lm *Manager) GetImageLogger(name string) (*zap.Logger, string, error) {
	sessionDir := lm.getOrCreateSessionDir()
	imageDir := filepath.Join(sessionDir, "images", name)

	// Create image directory
	if err := os.MkdirAll(imageDir, os.ModePerm); err != nil {
		return nil, "", fmt.Errorf("failed to create image directory: %w", err)
	}

	// Create logger for image operations
	zapLevel, err := zapcore.ParseLevel(lm.level)
	if err != nil {
		return nil, "", err
	}

	zapConfig := zap.NewDevelopmentConfig()
	zapConfig.OutputPaths = []string{
		filepath.Join(imageDir, name+".log"),
	}
	zapConfig.Level = zap.NewAtomicLevelAt(zapLevel)

	zapLogger, err := zapConfig.Build()
	if err != nil {
		return nil, "", err
	}

	return zapLogger, imageDir, nil
}

// GetTextLogger creates a logger specifically for handling blocked text content.
// It creates a dedicated text directory within the current session directory.
func (lm *Manager) GetTextLogger(name string) (*zap.Logger, string, error) {
	sessionDir := lm.getOrCreateSessionDir()
	textDir := filepath.Join(sessionDir, "blocked_text", name)

	// Create text directory
	if err := os.MkdirAll(textDir, os.ModePerm); err != nil {
		return nil, "", fmt.Errorf("failed to create text directory: %w", err)
	}

	// Create logger for text operations
	zapLevel, err := zapcore.ParseLevel(lm.level)
	if err != nil {
		return nil, "", err
	}

	zapConfig := zap.NewDevelopmentConfig()
	zapConfig.OutputPaths = []string{
		filepath.Join(textDir, name+".log"),
	}
	zapConfig.Level = zap.NewAtomicLevelAt(zapLevel)

	zapLogger, err := zapConfig.Build()
	if err != nil {
		return nil, "", err
	}

	return zapLogger, textDir, nil
}

// setupLogDirectories creates and manages the log directory structure.
// It ensures the base directory exists, rotates old logs, and creates a new session directory.
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
		logRotator := logger.NewLogRotator(file, lm.maxLogLines, path)

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
