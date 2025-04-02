package translator_test

import (
	"testing"

	"github.com/robalyx/rotector/internal/translator"
	"github.com/stretchr/testify/assert"
)

func TestTranslateCaesar(t *testing.T) {
	t.Parallel()
	trans := &translator.Translator{}

	tests := []struct {
		name     string
		input    string
		shift    int
		expected string
	}{
		{
			name:     "simple shift 1",
			input:    "Hello",
			shift:    1,
			expected: "Ifmmp",
		},
		{
			name:     "simple shift 13 (ROT13)",
			input:    "Hello",
			shift:    13,
			expected: "Uryyb",
		},
		{
			name:     "with spaces and punctuation",
			input:    "Hello, World!",
			shift:    1,
			expected: "Ifmmp, Xpsme!",
		},
		{
			name:     "mixed case",
			input:    "HeLLo",
			shift:    1,
			expected: "IfMMp",
		},
		{
			name:     "numbers and special chars unchanged",
			input:    "Hello123!@#",
			shift:    1,
			expected: "Ifmmp123!@#",
		},
		{
			name:     "wrap around Z",
			input:    "Zebra",
			shift:    1,
			expected: "Afcsb",
		},
		{
			name:     "shift 25 (reverse of shift 1)",
			input:    "Hello",
			shift:    25,
			expected: "Gdkkn",
		},
		{
			name:     "empty string",
			input:    "",
			shift:    1,
			expected: "",
		},
		{
			name:     "non-alphabetic only",
			input:    "123!@#",
			shift:    1,
			expected: "123!@#",
		},
		{
			name:     "normalize large shift",
			input:    "Hello",
			shift:    27,
			expected: "Ifmmp",
		},
		{
			name:     "normalize negative shift",
			input:    "Hello",
			shift:    -1,
			expected: "Gdkkn",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := trans.TranslateCaesar(tt.input, tt.shift)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsCaesarFormat(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "all letters",
			input:    "HelloWorld",
			expected: true,
		},
		{
			name:     "mostly letters with spaces",
			input:    "Hello World",
			expected: true,
		},
		{
			name:     "mixed with numbers but mostly letters",
			input:    "Hello123World",
			expected: true,
		},
		{
			name:     "too short",
			input:    "Hi",
			expected: false,
		},
		{
			name:     "empty string",
			input:    "",
			expected: false,
		},
		{
			name:     "mostly numbers",
			input:    "123abc456",
			expected: false,
		},
		{
			name:     "special characters",
			input:    "!@#$%^&*()",
			expected: false,
		},
		{
			name:     "mixed content below threshold",
			input:    "Hi! 12345678",
			expected: false,
		},
		{
			name:     "long text with punctuation",
			input:    "This is a long text with some punctuation marks! But it's still mostly letters.",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := translator.IsCaesarFormat(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
