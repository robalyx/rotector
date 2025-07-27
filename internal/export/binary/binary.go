package binary

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"github.com/robalyx/rotector/internal/export/types"
)

// Exporter handles exporting hashes to binary files.
type Exporter struct {
	outDir string
}

// New creates a new binary exporter instance.
func New(outDir string) *Exporter {
	return &Exporter{outDir: outDir}
}

// Export writes user and group records to separate binary files.
func (e *Exporter) Export(userRecords, groupRecords []*types.ExportRecord) error {
	// Remove existing files if they exist
	files := []string{"users.bin", "groups.bin"}
	for _, file := range files {
		path := filepath.Join(e.outDir, file)
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove existing file %s: %w", file, err)
		}
	}

	if err := e.writeFile("users.bin", userRecords); err != nil {
		return fmt.Errorf("failed to export users: %w", err)
	}

	if err := e.writeFile("groups.bin", groupRecords); err != nil {
		return fmt.Errorf("failed to export groups: %w", err)
	}

	return nil
}

// writeFile writes records to a binary file.
func (e *Exporter) writeFile(filename string, records []*types.ExportRecord) error {
	// Write number of records
	file, err := os.Create(filepath.Join(e.outDir, filename))
	if err != nil {
		return fmt.Errorf("failed to create binary file: %w", err)
	}
	defer file.Close()

	count := uint32(len(records)) //nolint:gosec // unlikely to overflow
	if err := binary.Write(file, binary.LittleEndian, count); err != nil {
		return fmt.Errorf("failed to write record count: %w", err)
	}

	// Write each record
	for _, record := range records {
		// Write hash
		hashBytes, err := hex.DecodeString(record.Hash)
		if err != nil {
			return fmt.Errorf("failed to decode hash: %w", err)
		}

		if _, err := file.Write(hashBytes); err != nil {
			return fmt.Errorf("failed to write hash: %w", err)
		}

		// Write status length and string
		statusBytes := []byte(record.Status)

		statusLen := uint16(len(statusBytes)) //nolint:gosec // unlikely to overflow
		if err := binary.Write(file, binary.LittleEndian, statusLen); err != nil {
			return fmt.Errorf("failed to write status length: %w", err)
		}

		if _, err := file.Write(statusBytes); err != nil {
			return fmt.Errorf("failed to write status: %w", err)
		}

		// Write reason length and string
		reasonBytes := []byte(record.Reason)

		reasonLen := uint16(len(reasonBytes)) //nolint:gosec // unlikely to overflow
		if err := binary.Write(file, binary.LittleEndian, reasonLen); err != nil {
			return fmt.Errorf("failed to write reason length: %w", err)
		}

		if _, err := file.Write(reasonBytes); err != nil {
			return fmt.Errorf("failed to write reason: %w", err)
		}

		// Write confidence
		if err := binary.Write(file, binary.LittleEndian, record.Confidence); err != nil {
			return fmt.Errorf("failed to write confidence: %w", err)
		}
	}

	return nil
}
