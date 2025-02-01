package export

import (
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHashID(t *testing.T) {
	tests := []struct {
		name       string
		id         uint64
		salt       string
		hashType   HashType
		iterations uint32
		memory     uint32
		want       string
	}{
		{
			name:       "SHA256 basic test",
			id:         12345,
			salt:       "test_salt",
			hashType:   HashTypeSHA256,
			iterations: 1,
			memory:     1,
			want:       "ce3807a728757fad6c9eb6f3934c71363857bca5f8f9d7a67452543acf47ac42",
		},
		{
			name:       "SHA256 multiple iterations",
			id:         12345,
			salt:       "test_salt",
			hashType:   HashTypeSHA256,
			iterations: 3,
			memory:     1,
			want:       "2f9ed488c8e0ccce3329b47ebb9c6b7870448da2ef857c9b9b1543c29bfd1d82",
		},
		{
			name:       "Argon2id basic test",
			id:         12345,
			salt:       "test_salt",
			hashType:   HashTypeArgon2id,
			iterations: 1,
			memory:     1,
			want:       "70734f36c4da16b8322f487906015143b6fd316b76b2e2dfd627b60f819702d6",
		},
		{
			name:       "Argon2id with more memory",
			id:         12345,
			salt:       "test_salt",
			hashType:   HashTypeArgon2id,
			iterations: 1,
			memory:     4,
			want:       "c775a52a3984ea346d40a413080403431d4afedd0998beb0d57c2408be1ec0b3",
		},
		{
			name:       "Different salt",
			id:         12345,
			salt:       "different_salt",
			hashType:   HashTypeSHA256,
			iterations: 1,
			memory:     1,
			want:       "a2a6313d0071b80edb96373d37d623f38b8fa062a596e690e2189a5242b92ce6",
		},
		{
			name:       "Different ID",
			id:         54321,
			salt:       "test_salt",
			hashType:   HashTypeSHA256,
			iterations: 1,
			memory:     1,
			want:       "c81079f1df424a4563c3a79a4557e8d0c3735f57cb110825955f85c4d8902511",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hashID(tt.id, tt.salt, tt.hashType, tt.iterations, tt.memory)

			_, err := hex.DecodeString(got)
			assert.NoError(t, err, "hashID() should produce valid hex string")
			assert.Equal(t, tt.want, got, "hashID() produced incorrect hash")
		})
	}
}

func TestHashResult(t *testing.T) {
	result := HashResult{
		Index: 1,
		Hash:  "abc123",
	}

	assert.Equal(t, 1, result.Index, "HashResult.Index should match")
	assert.Equal(t, "abc123", result.Hash, "HashResult.Hash should match")
}

func TestHashType(t *testing.T) {
	assert.Equal(t, HashType("argon2id"), HashTypeArgon2id, "HashTypeArgon2id constant should match")
	assert.Equal(t, HashType("sha256"), HashTypeSHA256, "HashTypeSHA256 constant should match")
}
