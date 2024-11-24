package setup

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jaxron/axonet/pkg/client/logger"
	"github.com/rotector/rotector/internal/common/config"
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
	maxLogLines       int    // Maximum number of lines to keep in each log file
}

// NewLogManager creates a new LogManager instance.
func NewLogManager(logDir string, cfg *config.Debug) *LogManager {
	return &LogManager{
		logDir:        logDir,
		level:         cfg.LogLevel,
		maxLogsToKeep: cfg.MaxLogsToKeep,
		maxLogLines:   cfg.MaxLogLines,
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

	// Create custom writer for each output path
	cores := make([]zapcore.Core, 0, len(logPaths))
	encoderConfig := zap.NewDevelopmentEncoderConfig()

	for _, path := range logPaths {
		file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return nil, fmt.Errorf("cannot open log file %s: %w", path, err)
		}

		// Create line counting writer
		lineWriter := NewLineCountingWriter(file, lm.maxLogLines, path)

		// Create custom core with our writer
		core := zapcore.NewCore(
			zapcore.NewConsoleEncoder(encoderConfig),
			zapcore.AddSync(lineWriter),
			zapLevel,
		)
		cores = append(cores, core)
	}

	// Create logger with all cores
	return zap.New(zapcore.NewTee(cores...)), nil
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

// LineCountingWriter wraps an io.Writer and maintains a fixed number of lines.
type LineCountingWriter struct {
	writer   io.Writer
	buffer   *RingBuffer
	filePath string
	mutex    sync.Mutex
}

// NewLineCountingWriter creates a new LineCountingWriter.
func NewLineCountingWriter(writer io.Writer, maxLines int, filePath string) *LineCountingWriter {
	return &LineCountingWriter{
		writer:   writer,
		buffer:   NewRingBuffer(maxLines),
		filePath: filePath,
	}
}

// Write implements io.Writer and maintains the line buffer.
func (w *LineCountingWriter) Write(p []byte) (n int, err error) {
	w.mutex.Lock()
	defer w.mutex.Unlock()

	// Write to the underlying writer first
	n, err = w.writer.Write(p)
	if err != nil {
		return n, err
	}

	// Split the input into lines
	data := string(p)
	newLines := strings.Split(strings.TrimRight(data, "\n"), "\n")

	// Add non-empty lines to the buffer
	for _, line := range newLines {
		if line != "" {
			w.buffer.add(line)

			// Only rotate when we've seen twice the capacity
			if w.buffer.totalSeen == w.buffer.capacity*2 {
				if err := w.rotate(); err != nil {
					return n, fmt.Errorf("failed to rotate log file: %w", err)
				}
				// Reset the total seen counter after rotation
				w.buffer.totalSeen = w.buffer.size
			}
		}
	}

	return n, nil
}

// rotate writes the current buffer to a new file.
func (w *LineCountingWriter) rotate() error {
	// Get all lines in chronological order
	lines := w.buffer.getLines()
	if len(lines) == 0 {
		return nil
	}

	// Create a temporary file
	temp, err := os.CreateTemp(filepath.Dir(w.filePath), "temp-log-")
	if err != nil {
		return err
	}
	tempPath := temp.Name()

	// Write all lines in one operation
	content := strings.Join(lines, "\n") + "\n"
	if _, err := temp.WriteString(content); err != nil {
		temp.Close()
		os.Remove(tempPath)
		return err
	}

	if err := temp.Sync(); err != nil {
		temp.Close()
		os.Remove(tempPath)
		return err
	}
	temp.Close()

	// Close the original writer if it implements io.Closer
	if closer, ok := w.writer.(io.Closer); ok {
		closer.Close()
	}

	// On Windows, remove the original file first
	os.Remove(w.filePath)

	// Rename temp file to original
	if err := os.Rename(tempPath, w.filePath); err != nil {
		return err
	}

	// Reopen the file for writing
	newFile, err := os.OpenFile(w.filePath, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}

	// Update the writer
	w.writer = newFile

	return nil
}

// RingBuffer implements a circular buffer for log lines.
type RingBuffer struct {
	lines     []string
	capacity  int
	head      int // Points to the next write position
	size      int // Current number of items in buffer
	totalSeen int // Total number of lines that have passed through
}

// NewRingBuffer creates a new ring buffer with the specified capacity.
func NewRingBuffer(capacity int) *RingBuffer {
	return &RingBuffer{
		lines:    make([]string, capacity),
		capacity: capacity,
	}
}

// add adds a line to the ring buffer.
func (rb *RingBuffer) add(line string) {
	rb.lines[rb.head] = line
	rb.head = (rb.head + 1) % rb.capacity
	if rb.size < rb.capacity {
		rb.size++
	}
	rb.totalSeen++
}

// getLines returns all lines in chronological order.
func (rb *RingBuffer) getLines() []string {
	if rb.size == 0 {
		return nil
	}

	result := make([]string, rb.size)
	start := (rb.head - rb.size + rb.capacity) % rb.capacity

	for i := range rb.size {
		idx := (start + i) % rb.capacity
		result[i] = rb.lines[idx]
	}

	return result
}
