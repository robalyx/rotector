package csv

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"

	"github.com/robalyx/rotector/internal/export/types"
)

// Exporter handles exporting hashes to csv files.
type Exporter struct {
	outDir string
}

// New creates a new csv exporter instance.
func New(outDir string) *Exporter {
	return &Exporter{outDir: outDir}
}

// Export writes user and group records to separate csv files.
func (e *Exporter) Export(userRecords, groupRecords []*types.ExportRecord) error {
	// Remove existing files if they exist
	files := []string{"users.csv", "groups.csv"}
	for _, file := range files {
		path := filepath.Join(e.outDir, file)
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove existing file %s: %w", file, err)
		}
	}

	if err := e.writeFile("users.csv", userRecords); err != nil {
		return fmt.Errorf("failed to export users: %w", err)
	}

	if err := e.writeFile("groups.csv", groupRecords); err != nil {
		return fmt.Errorf("failed to export groups: %w", err)
	}

	return nil
}

// writeFile writes records to a csv file.
func (e *Exporter) writeFile(filename string, records []*types.ExportRecord) error {
	file, err := os.Create(filepath.Join(e.outDir, filename))
	if err != nil {
		return fmt.Errorf("failed to create csv file: %w", err)
	}
	defer file.Close()

	// Create CSV writer
	writer := csv.NewWriter(file)
	defer writer.Flush()

	// Write header
	if err := writer.Write([]string{"hash", "status", "reason", "confidence"}); err != nil {
		return fmt.Errorf("failed to write header: %w", err)
	}

	// Write each record
	for _, record := range records {
		if err := writer.Write([]string{
			record.Hash,
			record.Status,
			record.Reason,
			fmt.Sprintf("%.2f", record.Confidence),
		}); err != nil {
			return fmt.Errorf("failed to write record: %w", err)
		}
	}

	return nil
}
