package logger

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// LogRotator wraps an io.Writer and maintains a fixed number of lines.
type LogRotator struct {
	writer   io.Writer
	buffer   *RingBuffer
	filePath string
	mutex    sync.Mutex
}

// NewLogRotator creates a new LogRotator.
func NewLogRotator(writer io.Writer, maxLines int, filePath string) *LogRotator {
	return &LogRotator{
		writer:   writer,
		buffer:   NewRingBuffer(maxLines),
		filePath: filePath,
	}
}

// Write implements io.Writer and maintains the line buffer.
func (w *LogRotator) Write(p []byte) (n int, err error) {
	w.mutex.Lock()
	defer w.mutex.Unlock()

	// Write to the underlying writer first
	n, err = w.writer.Write(p)
	if err != nil {
		return n, err
	}

	// Split the input into lines
	data := string(p)
	newLines := strings.SplitSeq(strings.TrimRight(data, "\n"), "\n")

	// Add non-empty lines to the buffer
	for line := range newLines {
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
func (w *LogRotator) rotate() error {
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
