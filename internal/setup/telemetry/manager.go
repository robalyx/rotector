package telemetry

import (
	"context"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/robalyx/rotector/internal/setup/config"
	"github.com/robalyx/rotector/internal/setup/telemetry/logger"
	"github.com/robalyx/rotector/internal/setup/telemetry/loki"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// ServiceType represents the type of service being initialized.
type ServiceType int

const (
	ServiceBot ServiceType = iota
	ServiceWorker
	ServiceExport
	ServiceQueue
)

// GetRequestTimeout returns the request timeout for the given service type.
func (s ServiceType) GetRequestTimeout(cfg *config.Config) time.Duration {
	var timeout int

	switch s {
	case ServiceWorker:
		timeout = cfg.Worker.RequestTimeout
	case ServiceBot:
		timeout = cfg.Bot.RequestTimeout
	case ServiceExport:
		timeout = 30000
	case ServiceQueue:
		timeout = 10000
	default:
		timeout = 5000
	}

	return time.Duration(timeout) * time.Millisecond
}

// Manager handles the creation and management of log files and directories.
// It maintains both timestamped session logs and a "latest" symlink for easy access.
type Manager struct {
	lokiPusher        *loki.Pusher // Loki pusher for cloud logging
	instanceID        string       // Unique identifier for this program instance
	componentName     string       // Component identifier for this instance
	currentSessionDir string       // Path to the current session's log directory
	logDir            string       // Base directory for all logs
	level             string       // Logging level (debug, info, warn, error)
	maxLogsToKeep     int          // Maximum number of log sessions to retain
	maxLogLines       int          // Maximum number of lines to keep in each log file
}

// NewManager creates a new Manager instance.
func NewManager(
	ctx context.Context, serviceType ServiceType, logDir string,
	debugCfg *config.Debug, lokiCfg *config.Loki, workerType string, workerID string,
) *Manager {
	instanceID := uuid.New().String()

	// Determine component name based on service type
	var componentName string

	switch serviceType {
	case ServiceBot:
		componentName = "bot"
	case ServiceWorker:
		if workerType != "" {
			if workerID != "" {
				componentName = fmt.Sprintf("%s_worker_%s", workerType, workerID)
			} else {
				componentName = workerType + "_worker"
			}
		} else {
			componentName = "worker"
		}
	case ServiceExport:
		componentName = "export"
	case ServiceQueue:
		componentName = "queue"
	default:
		componentName = "unknown"
	}

	manager := &Manager{
		instanceID:    instanceID,
		componentName: componentName,
		logDir:        logDir,
		level:         debugCfg.LogLevel,
		maxLogsToKeep: debugCfg.MaxLogsToKeep,
		maxLogLines:   debugCfg.MaxLogLines,
	}

	// Initialize Loki pusher if enabled
	if lokiCfg.Enabled && lokiCfg.URL != "" {
		// Build complete label set
		baseLabels := make(map[string]string)
		maps.Copy(baseLabels, lokiCfg.Labels)

		baseLabels["component"] = componentName
		baseLabels["instance_id"] = instanceID

		// Initialize Loki pusher
		lokiConfigWithLabels := *lokiCfg
		lokiConfigWithLabels.Labels = baseLabels
		manager.lokiPusher = loki.NewPusher(ctx, lokiConfigWithLabels)
	}

	return manager
}

// Stop gracefully shuts down the telemetry manager.
// This should be called on application shutdown to ensure logs are flushed.
func (lm *Manager) Stop() {
	if lm.lokiPusher != nil {
		lm.lokiPusher.Stop()
	}
}

// GetLoggers initializes the main and database loggers.
// Returns separate loggers for main application and database logging.
func (lm *Manager) GetLoggers() (*zap.Logger, *zap.Logger, error) {
	if err := lm.setupLogDirectories(); err != nil {
		return nil, nil, err
	}

	// Initialize main application logger
	warnLevel := zapcore.WarnLevel

	mainLogger, err := lm.initLogger([]string{
		filepath.Join(lm.currentSessionDir, "main.log"),
	}, &warnLevel)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to initialize main logger: %w", err)
	}

	// Initialize database logger
	warnLevel = zapcore.WarnLevel

	dbLogger, err := lm.initLogger([]string{
		filepath.Join(lm.currentSessionDir, "database.log"),
	}, &warnLevel)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to initialize database logger: %w", err)
	}

	return mainLogger, dbLogger, nil
}

// GetWorkerLogger creates a logger for background workers.
// Each worker gets its own log file in the session directory.
func (lm *Manager) GetWorkerLogger(name string) *zap.Logger {
	sessionDir := lm.getOrCreateSessionDir()

	logger, err := lm.initLogger([]string{
		filepath.Join(sessionDir, name+".log"),
	}, nil)
	if err != nil {
		return zap.NewNop()
	}

	return logger
}

// GetCurrentSessionDir returns the current session directory.
// This is useful for external components that need to access logs in the same session.
func (lm *Manager) GetCurrentSessionDir() string {
	return lm.getOrCreateSessionDir()
}

// GetInstanceID returns the unique instance identifier for this program run.
// This ID is used for both logging and worker status correlation.
func (lm *Manager) GetInstanceID() string {
	return lm.instanceID
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

	logger, err := lm.initLogger([]string{
		filepath.Join(imageDir, name+".log"),
	}, nil)
	if err != nil {
		return nil, "", err
	}

	return logger, imageDir, nil
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

	logger, err := lm.initLogger([]string{
		filepath.Join(textDir, name+".log"),
	}, nil)
	if err != nil {
		return nil, "", err
	}

	return logger, textDir, nil
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

// initLogger creates a new zap logger with file output and optional Loki integration.
func (lm *Manager) initLogger(logPaths []string, lokiMinLevel *zapcore.Level) (*zap.Logger, error) {
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

	// Add Loki core if Loki pusher is available
	if lm.lokiPusher != nil {
		var lokiLevel zapcore.LevelEnabler
		if lokiMinLevel != nil {
			// Use custom level for Loki
			lokiLevel = zap.LevelEnablerFunc(func(lvl zapcore.Level) bool {
				return lvl >= *lokiMinLevel
			})
		} else {
			// Use configured level for Loki
			lokiLevel = zap.LevelEnablerFunc(func(lvl zapcore.Level) bool {
				return lvl >= zapLevel
			})
		}

		cores = append(cores, loki.NewCore(lokiLevel, lm.lokiPusher))
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
	toDelete := len(sessions) - lm.maxLogsToKeep
	for i := range toDelete {
		if err := os.RemoveAll(sessions[i]); err != nil {
			return err
		}
	}

	return nil
}
