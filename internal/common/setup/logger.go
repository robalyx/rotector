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

// Logger adapts zap.Logger to implement the axonet logger.Logger interface,
// allowing it to be used with the axonet HTTP client.
type Logger struct {
	zap *zap.Logger
}

// NewLogger wraps a zap.Logger instance to provide the axonet logging interface.
// All log methods are delegated directly to the underlying zap logger.
func NewLogger(zapLogger *zap.Logger) logger.Logger {
	return &Logger{
		zap: zapLogger,
	}
}

func (l *Logger) Debug(msg string)                          { l.zap.Debug(msg) }
func (l *Logger) Info(msg string)                           { l.zap.Info(msg) }
func (l *Logger) Warn(msg string)                           { l.zap.Warn(msg) }
func (l *Logger) Error(msg string)                          { l.zap.Error(msg) }
func (l *Logger) Debugf(format string, args ...interface{}) { l.zap.Sugar().Debugf(format, args...) }
func (l *Logger) Infof(format string, args ...interface{})  { l.zap.Sugar().Infof(format, args...) }
func (l *Logger) Warnf(format string, args ...interface{})  { l.zap.Sugar().Warnf(format, args...) }
func (l *Logger) Errorf(format string, args ...interface{}) { l.zap.Sugar().Errorf(format, args...) }

// WithFields creates a new logger with additional context fields by wrapping
// the underlying zap logger's With method. Converts axonet fields to zap fields.
func (l *Logger) WithFields(fields ...logger.Field) logger.Logger {
	zapFields := make([]zap.Field, len(fields))
	for i, f := range fields {
		zapFields[i] = zap.Any(f.Key, f.Value)
	}
	return &Logger{
		zap: l.zap.With(zapFields...),
	}
}

// GetLoggers sets up the logging infrastructure by creating timestamped log
// directories and initializing separate loggers for main application and database logging.
func GetLoggers(logDir string, level string, maxLogsToKeep int) (*zap.Logger, *zap.Logger, error) {
	// Ensure log directory exists
	err := os.MkdirAll(logDir, os.ModePerm)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create logs directory: %w", err)
	}

	// Clean up old log sessions before creating new ones
	err = rotateLogSessions(logDir, maxLogsToKeep)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to rotate log sessions: %w", err)
	}

	// Create timestamped directory for this session's logs
	sessionDir := filepath.Join(logDir, time.Now().Format("2006-01-02_15-04-05"))
	err = os.MkdirAll(sessionDir, os.ModePerm)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create session directory: %w", err)
	}

	// Initialize separate loggers for different concerns
	mainLogger, err := initLogger(filepath.Join(sessionDir, "main.log"), level)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to initialize main logger: %w", err)
	}

	dbLogger, err := initLogger(filepath.Join(sessionDir, "database.log"), level)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to initialize database logger: %w", err)
	}

	return mainLogger, dbLogger, nil
}

// initLogger creates a zap logger instance with development settings and file output.
// Uses atomic level control to allow log level changes.
func initLogger(logPath string, level string) (*zap.Logger, error) {
	zapLevel, err := zapcore.ParseLevel(level)
	if err != nil {
		return nil, fmt.Errorf("invalid log level: %w", err)
	}

	config := zap.NewDevelopmentConfig()
	config.OutputPaths = []string{logPath}
	config.Level = zap.NewAtomicLevelAt(zapLevel)

	logger, err := config.Build()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize logger: %w", err)
	}

	return logger, nil
}

// GetWorkerLogger creates a logger for background workers by placing their logs
// in the latest session directory. Falls back to no-op logger on errors.
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

// rotateLogSessions maintains the log directory by removing oldest sessions
// when the total number exceeds maxLogsToKeep. Uses file modification time
// to determine age.
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

	// Sort by modification time to identify oldest sessions
	sort.Slice(sessions, func(i, j int) bool {
		iInfo, _ := os.Stat(sessions[i])
		jInfo, _ := os.Stat(sessions[j])
		return iInfo.ModTime().Before(jInfo.ModTime())
	})

	// Remove oldest sessions to maintain limit
	for i := range len(sessions) - maxLogsToKeep {
		err := os.RemoveAll(sessions[i])
		if err != nil {
			return err
		}
	}

	return nil
}

// getLatestSessionDir finds the most recent log session by sorting directory
// modification times. Falls back to main log directory if no sessions exist.
func getLatestSessionDir(logDir string) string {
	sessions, err := filepath.Glob(filepath.Join(logDir, "*"))
	if err != nil || len(sessions) == 0 {
		return logDir
	}

	sort.Slice(sessions, func(i, j int) bool {
		iInfo, _ := os.Stat(sessions[i])
		jInfo, _ := os.Stat(sessions[j])
		return iInfo.ModTime().After(jInfo.ModTime())
	})

	return sessions[0]
}
