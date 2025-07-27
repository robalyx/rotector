package translator_test

import (
	"testing"

	"github.com/robalyx/rotector/internal/translator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTranslateBinary(t *testing.T) {
	t.Parallel()

	translator := &translator.Translator{}

	tests := []struct {
		name        string
		input       string
		expected    string
		expectError bool
	}{
		{
			name:     "simple text",
			input:    "01001000 01100101 01101100 01101100 01101111",
			expected: "Hello",
		},
		{
			name:     "with spaces removed",
			input:    "0100100001100101011011000110110001101111",
			expected: "Hello",
		},
		{
			name:     "with special characters",
			input:    "01001000 01100101 01111001 00100001",
			expected: "Hey!",
		},
		{
			name:        "incomplete byte",
			input:       "0100100",
			expectError: true,
		},
		{
			name:        "invalid binary",
			input:       "01002000",
			expectError: true,
		},
		{
			name:     "multiple words",
			input:    "01001000 01101001 00100000 01110100 01101000 01100101 01110010 01100101",
			expected: "Hi there",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := translator.TranslateBinary(tt.input)
			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestIsBinaryFormat(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "valid binary string",
			input:    "01001000 01100101 01101100 01101100 01101111",
			expected: true,
		},
		{
			name:     "valid binary without spaces",
			input:    "0100100001100101011011000110110001101111",
			expected: true,
		},
		{
			name:     "invalid characters",
			input:    "01001000 0110210",
			expected: false,
		},
		{
			name:     "incomplete byte",
			input:    "0100100",
			expected: false,
		},
		{
			name:     "empty string",
			input:    "",
			expected: false,
		},
		{
			name:     "regular text",
			input:    "Hello World",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := translator.IsBinaryFormat(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
