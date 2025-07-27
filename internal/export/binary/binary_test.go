package binary_test

import (
	"encoding/binary"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"testing"

	exportBinary "github.com/robalyx/rotector/internal/export/binary"

	"github.com/robalyx/rotector/internal/export/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// verifyBinaryFile reads a binary file and verifies its contents match the expected records.
func verifyBinaryFile(t *testing.T, filepath string, expectedRecords []*types.ExportRecord) {
	t.Helper()
	// Open file
	file, err := os.Open(filepath)
	require.NoError(t, err)

	defer file.Close()

	// Read record count
	var count uint32

	err = binary.Read(file, binary.LittleEndian, &count)
	require.NoError(t, err)
	assert.Equal(t, uint32(len(expectedRecords)), count)

	// Read and verify each record
	for _, expected := range expectedRecords {
		// Read hash
		hashBytes := make([]byte, 32) // SHA-256 hash is 32 bytes
		_, err = file.Read(hashBytes)
		require.NoError(t, err)
		assert.Equal(t, expected.Hash, hex.EncodeToString(hashBytes))

		// Read status
		var statusLen uint16

		err = binary.Read(file, binary.LittleEndian, &statusLen)
		require.NoError(t, err)

		statusBytes := make([]byte, statusLen)
		_, err = file.Read(statusBytes)
		require.NoError(t, err)
		assert.Equal(t, expected.Status, string(statusBytes))

		// Read reason
		var reasonLen uint16

		err = binary.Read(file, binary.LittleEndian, &reasonLen)
		require.NoError(t, err)

		reasonBytes := make([]byte, reasonLen)
		_, err = file.Read(reasonBytes)
		require.NoError(t, err)
		assert.Equal(t, expected.Reason, string(reasonBytes))

		// Read confidence
		var confidence float64

		err = binary.Read(file, binary.LittleEndian, &confidence)
		require.NoError(t, err)
		assert.InEpsilon(t, expected.Confidence, confidence, 0.01)
	}

	// Verify we're at EOF
	_, err = file.Read(make([]byte, 1))
	assert.Equal(t, err, io.EOF, "expected EOF after reading all records")
}

func TestExporter_Export(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		userRecords  []*types.ExportRecord
		groupRecords []*types.ExportRecord
		wantErr      bool
		errMsg       string
	}{
		{
			name: "basic export",
			userRecords: []*types.ExportRecord{
				{
					Hash:       "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
					Status:     "confirmed",
					Reason:     "test reason",
					Confidence: 0.95,
				},
			},
			groupRecords: []*types.ExportRecord{
				{
					Hash:       "fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210",
					Status:     "flagged",
					Reason:     "group test reason",
					Confidence: 0.85,
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
			name: "invalid hash",
			userRecords: []*types.ExportRecord{
				{
					Hash:   "invalid",
					Status: "confirmed",
					Reason: "test",
				},
			},
			groupRecords: []*types.ExportRecord{},
			wantErr:      true,
			errMsg:       "failed to export users: failed to decode hash: encoding/hex: invalid byte",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tempDir := t.TempDir()

			// Create new exporter
			e := exportBinary.New(tempDir)

			// Perform export
			err := e.Export(tt.userRecords, tt.groupRecords)

			if tt.wantErr {
				require.Error(t, err)

				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}

				return
			}

			require.NoError(t, err)

			// Verify users.bin
			if len(tt.userRecords) > 0 {
				verifyBinaryFile(t, filepath.Join(tempDir, "users.bin"), tt.userRecords)
			}

			// Verify groups.bin
			if len(tt.groupRecords) > 0 {
				verifyBinaryFile(t, filepath.Join(tempDir, "groups.bin"), tt.groupRecords)
			}
		})
	}
}
