package translator

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTranslateMorse(t *testing.T) {
	translator := New(nil)

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple word",
			input:    ".... . .-.. .-.. ---",
			expected: "HELLO",
		},
		{
			name:     "multiple words",
			input:    ".... . .-.. .-.. --- / .-- --- .-. .-.. -..",
			expected: "HELLO WORLD",
		},
		{
			name:     "with numbers",
			input:    "... --- ... / .---- ..--- ...--",
			expected: "SOS 123",
		},
		{
			name:     "with punctuation",
			input:    ".... . .-.. .-.. --- -.-.-- / .-- --- .-. .-.. -.. .-.-.-",
			expected: "HELLO! WORLD.",
		},
		{
			name:     "empty input",
			input:    "",
			expected: "",
		},
		{
			name:     "invalid morse code",
			input:    ".... . .-.. xxx ---",
			expected: "HELO",
		},
		{
			name:     "extra spaces",
			input:    "....   .   .-..   .-..   ---",
			expected: "HELLO",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := translator.TranslateMorse(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsMorseFormat(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "valid morse code",
			input:    ".... . .-.. .-.. ---",
			expected: true,
		},
		{
			name:     "valid morse with slash",
			input:    "... --- ... / .... . .-.. .--.",
			expected: true,
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
		{
			name:     "numbers only",
			input:    "12345",
			expected: false,
		},
		{
			name:     "mixed invalid content",
			input:    ".... . x .-.. .-.. ---",
			expected: false,
		},
		{
			name:     "only dots",
			input:    "... ... ...",
			expected: true,
		},
		{
			name:     "only dashes",
			input:    "--- --- ---",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isMorseFormat(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
