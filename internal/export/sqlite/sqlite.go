package sqlite

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/robalyx/rotector/internal/export/types"
	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
)

// Exporter handles exporting hashes to SQLite databases.
type Exporter struct {
	outDir string
}

// New creates a new SQLite exporter instance.
func New(outDir string) *Exporter {
	return &Exporter{outDir: outDir}
}

// Export writes user and group records to separate SQLite databases.
func (e *Exporter) Export(userRecords, groupRecords []*types.ExportRecord) error {
	// Remove existing files if they exist
	files := []string{"users.db", "groups.db"}
	for _, file := range files {
		path := filepath.Join(e.outDir, file)
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove existing file %s: %w", file, err)
		}
	}

	if err := e.createDB("users.db", "users", userRecords); err != nil {
		return fmt.Errorf("failed to export users: %w", err)
	}

	if err := e.createDB("groups.db", "groups", groupRecords); err != nil {
		return fmt.Errorf("failed to export groups: %w", err)
	}

	return nil
}

// createDB creates a SQLite database with a single table containing records.
func (e *Exporter) createDB(filename, table string, records []*types.ExportRecord) error {
	// Open database
	conn, err := sqlite.OpenConn(filepath.Join(e.outDir, filename), sqlite.OpenCreate|sqlite.OpenReadWrite)
	if err != nil {
		return fmt.Errorf("failed to open SQLite database: %w", err)
	}
	defer conn.Close()

	// Create table
	err = sqlitex.Execute(conn, fmt.Sprintf(`
		CREATE TABLE %s (
			hash TEXT PRIMARY KEY,
			status TEXT NOT NULL,
			reason TEXT NOT NULL,
			confidence REAL NOT NULL
		)
	`, table), nil)
	if err != nil {
		return fmt.Errorf("failed to create table: %w", err)
	}

	// Insert records in batches
	const batchSize = 1000
	for i := 0; i < len(records); i += batchSize {
		end := min(i+batchSize, len(records))

		// Begin transaction
		err = sqlitex.Execute(conn, "BEGIN TRANSACTION", nil)
		if err != nil {
			return fmt.Errorf("failed to begin transaction: %w", err)
		}

		// Insert batch
		for _, record := range records[i:end] {
			err = sqlitex.Execute(conn, fmt.Sprintf(
				"INSERT INTO %s (hash, status, reason, confidence) VALUES (?, ?, ?, ?)", table,
			), &sqlitex.ExecOptions{
				Args: []any{record.Hash, record.Status, record.Reason, record.Confidence},
			})
			if err != nil {
				return fmt.Errorf("failed to insert record: %w", err)
			}
		}

		// Commit transaction
		err = sqlitex.Execute(conn, "COMMIT", nil)
		if err != nil {
			return fmt.Errorf("failed to commit transaction: %w", err)
		}
	}

	return nil
}
