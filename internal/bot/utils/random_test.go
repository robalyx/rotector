package utils

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGenerateRandomWords(t *testing.T) {
	tests := []struct {
		name  string
		count int
	}{
		{
			name:  "single word",
			count: 1,
		},
		{
			name:  "multiple words",
			count: 3,
		},
		{
			name:  "zero words",
			count: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GenerateRandomWords(tt.count)
			words := strings.Split(got, " ")

			if tt.count == 0 {
				assert.Empty(t, got)
			} else {
				assert.Len(t, words, tt.count)
			}
		})
	}
}

func TestGenerateSecureToken(t *testing.T) {
	tests := []struct {
		name   string
		length int
	}{
		{
			name:   "short token",
			length: 10,
		},
		{
			name:   "medium token",
			length: 32,
		},
		{
			name:   "long token",
			length: 64,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GenerateSecureToken(tt.length)
			assert.Len(t, got, tt.length)

			// Generate another token to ensure they're different
			got2 := GenerateSecureToken(tt.length)
			assert.NotEqual(t, got, got2, "tokens should be random")
		})
	}
}
