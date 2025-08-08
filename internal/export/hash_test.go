package export_test

import (
	"encoding/hex"
	"testing"

	"github.com/robalyx/rotector/internal/export"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHashID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		id         int64
		salt       string
		hashType   export.HashType
		iterations uint32
		memory     uint32
		want       string
	}{
		{
			name:       "SHA256 basic test",
			id:         12345,
			salt:       "test_salt",
			hashType:   export.HashTypeSHA256,
			iterations: 1,
			memory:     1,
			want:       "ce3807a728757fad6c9eb6f3934c71363857bca5f8f9d7a67452543acf47ac42",
		},
		{
			name:       "SHA256 multiple iterations",
			id:         12345,
			salt:       "test_salt",
			hashType:   export.HashTypeSHA256,
			iterations: 3,
			memory:     1,
			want:       "2f9ed488c8e0ccce3329b47ebb9c6b7870448da2ef857c9b9b1543c29bfd1d82",
		},
		{
			name:       "Argon2id basic test",
			id:         12345,
			salt:       "test_salt",
			hashType:   export.HashTypeArgon2id,
			iterations: 1,
			memory:     1,
			want:       "70734f36c4da16b8322f487906015143b6fd316b76b2e2dfd627b60f819702d6",
		},
		{
			name:       "Argon2id with more memory",
			id:         12345,
			salt:       "test_salt",
			hashType:   export.HashTypeArgon2id,
			iterations: 1,
			memory:     4,
			want:       "c775a52a3984ea346d40a413080403431d4afedd0998beb0d57c2408be1ec0b3",
		},
		{
			name:       "Different salt",
			id:         12345,
			salt:       "different_salt",
			hashType:   export.HashTypeSHA256,
			iterations: 1,
			memory:     1,
			want:       "a2a6313d0071b80edb96373d37d623f38b8fa062a596e690e2189a5242b92ce6",
		},
		{
			name:       "Different ID",
			id:         54321,
			salt:       "test_salt",
			hashType:   export.HashTypeSHA256,
			iterations: 1,
			memory:     1,
			want:       "c81079f1df424a4563c3a79a4557e8d0c3735f57cb110825955f85c4d8902511",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := export.HashID(tt.id, tt.salt, tt.hashType, tt.iterations, tt.memory)

			_, err := hex.DecodeString(got)
			require.NoError(t, err, "hashID() should produce valid hex string")
			assert.Equal(t, tt.want, got, "hashID() produced incorrect hash")
		})
	}
}

func TestHashResult(t *testing.T) {
	t.Parallel()

	result := export.HashResult{
		Index: 1,
		Hash:  "abc123",
	}

	assert.Equal(t, 1, result.Index, "HashResult.Index should match")
	assert.Equal(t, "abc123", result.Hash, "HashResult.Hash should match")
}

func TestHashType(t *testing.T) {
	t.Parallel()
	assert.Equal(t, export.HashTypeArgon2id, export.HashType("argon2id"), "HashTypeArgon2id constant should match")
	assert.Equal(t, export.HashTypeSHA256, export.HashType("sha256"), "HashTypeSHA256 constant should match")
}
