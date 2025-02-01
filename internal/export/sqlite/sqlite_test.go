package sqlite

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/robalyx/rotector/internal/export/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
)

// verifySQLiteFile reads a SQLite database file and verifies its contents match the expected records.
func verifySQLiteFile(t *testing.T, filepath string, tableName string, expectedRecords []*types.ExportRecord) {
	// Open database
	conn, err := sqlite.OpenConn(filepath, sqlite.OpenReadOnly)
	require.NoError(t, err)
	defer conn.Close()

	// Query all records
	var records []*types.ExportRecord
	err = sqlitex.ExecuteTransient(conn, "SELECT hash, status, reason FROM "+tableName+" ORDER BY hash", &sqlitex.ExecOptions{
		ResultFunc: func(stmt *sqlite.Stmt) error {
			records = append(records, &types.ExportRecord{
				Hash:   stmt.ColumnText(0),
				Status: stmt.ColumnText(1),
				Reason: stmt.ColumnText(2),
			})
			return nil
		},
	})
	require.NoError(t, err)

	// Verify record count
	assert.Equal(t, len(expectedRecords), len(records), "record count mismatch")

	// Verify each record
	for i, expected := range expectedRecords {
		assert.Equal(t, expected.Hash, records[i].Hash)
		assert.Equal(t, expected.Status, records[i].Status)
		assert.Equal(t, expected.Reason, records[i].Reason)
	}
}

func TestExporter_Export(t *testing.T) {
	tempDir := t.TempDir()

	tests := []struct {
		name         string
		userRecords  []*types.ExportRecord
		groupRecords []*types.ExportRecord
		wantErr      bool
	}{
		{
			name: "basic export",
			userRecords: []*types.ExportRecord{
				{
					Hash:   "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
					Status: "confirmed",
					Reason: "test reason",
				},
				{
					Hash:   "fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210",
					Status: "flagged",
					Reason: "another reason",
				},
			},
			groupRecords: []*types.ExportRecord{
				{
					Hash:   "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa1",
					Status: "flagged",
					Reason: "group test reason",
				},
				{
					Hash:   "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb2",
					Status: "confirmed",
					Reason: "another group reason",
				},
			},
			wantErr: false,
		},
		{
			name:         "empty records",
			userRecords:  []*types.ExportRecord{},
			groupRecords: []*types.ExportRecord{},
			wantErr:      false,
		},
		{
			name: "records with special characters",
			userRecords: []*types.ExportRecord{
				{
					Hash:   "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc3",
					Status: "confirmed",
					Reason: "reason with ' single quote",
				},
				{
					Hash:   "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd4",
					Status: "flagged",
					Reason: "reason with \" double quote",
				},
			},
			groupRecords: []*types.ExportRecord{},
			wantErr:      false,
		},
		{
			name: "duplicate hash",
			userRecords: []*types.ExportRecord{
				{
					Hash:   "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
					Status: "confirmed",
					Reason: "test reason",
				},
				{
					Hash:   "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
					Status: "flagged",
					Reason: "duplicate hash",
				},
			},
			groupRecords: []*types.ExportRecord{},
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create new exporter
			e := New(tempDir)

			// Perform export
			err := e.Export(tt.userRecords, tt.groupRecords)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)

			// Verify users.db
			if len(tt.userRecords) > 0 {
				verifySQLiteFile(t, filepath.Join(tempDir, "users.db"), "users", tt.userRecords)
			}

			// Verify groups.db
			if len(tt.groupRecords) > 0 {
				verifySQLiteFile(t, filepath.Join(tempDir, "groups.db"), "groups", tt.groupRecords)
			}
		})
	}
}

func TestExporter_ExistingFiles(t *testing.T) {
	tempDir := t.TempDir()

	// Create existing files
	files := []string{"users.db", "groups.db"}
	for _, file := range files {
		err := os.WriteFile(filepath.Join(tempDir, file), []byte("invalid sqlite db"), 0o644)
		require.NoError(t, err)
	}

	e := New(tempDir)

	records := []*types.ExportRecord{
		{
			Hash:   "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			Status: "confirmed",
			Reason: "test reason",
		},
	}

	// Export should overwrite existing files
	err := e.Export(records, records)
	require.NoError(t, err)

	// Verify both files were overwritten
	verifySQLiteFile(t, filepath.Join(tempDir, "users.db"), "users", records)
	verifySQLiteFile(t, filepath.Join(tempDir, "groups.db"), "groups", records)
}

func TestExporter_DatabaseSchema(t *testing.T) {
	tempDir := t.TempDir()
	e := New(tempDir)

	// Create a test record
	records := []*types.ExportRecord{
		{
			Hash:   "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			Status: "confirmed",
			Reason: "test reason",
		},
	}

	// Export the record
	err := e.Export(records, nil)
	require.NoError(t, err)

	// Open the database
	conn, err := sqlite.OpenConn(filepath.Join(tempDir, "users.db"), sqlite.OpenReadOnly)
	require.NoError(t, err)
	defer conn.Close()

	// Query table schema
	var columns []string
	err = sqlitex.ExecuteTransient(conn, "PRAGMA table_info(users)", &sqlitex.ExecOptions{
		ResultFunc: func(stmt *sqlite.Stmt) error {
			columns = append(columns, stmt.ColumnText(1)) // Column name is at index 1
			return nil
		},
	})
	require.NoError(t, err)

	// Verify schema
	expectedColumns := []string{"hash", "status", "reason"}
	assert.Equal(t, expectedColumns, columns)

	// Verify primary key
	var pkColumn string
	err = sqlitex.ExecuteTransient(conn, "SELECT name FROM pragma_table_info('users') WHERE pk = 1", &sqlitex.ExecOptions{
		ResultFunc: func(stmt *sqlite.Stmt) error {
			pkColumn = stmt.ColumnText(0)
			return nil
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "hash", pkColumn)
}
