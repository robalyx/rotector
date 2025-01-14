package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizeString(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "string with diacritics",
			input: "héllo wörld",
			want:  "helloworld",
		},
		{
			name:  "string with spaces",
			input: "hello   world",
			want:  "helloworld",
		},
		{
			name:  "mixed case with spaces and diacritics",
			input: "HéLLo   WöRLD",
			want:  "helloworld",
		},
		{
			name:  "string with special characters",
			input: "héllo! @wörld#",
			want:  "hello!@world#",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeString(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestContainsNormalized(t *testing.T) {
	tests := []struct {
		name   string
		s      string
		substr string
		want   bool
	}{
		{
			name:   "empty strings",
			s:      "",
			substr: "",
			want:   false,
		},
		{
			name:   "simple match",
			s:      "hello world",
			substr: "hello",
			want:   true,
		},
		{
			name:   "case insensitive match",
			s:      "Hello World",
			substr: "hello",
			want:   true,
		},
		{
			name:   "diacritic match",
			s:      "héllo wörld",
			substr: "hello",
			want:   true,
		},
		{
			name:   "no match",
			s:      "hello world",
			substr: "goodbye",
			want:   false,
		},
		{
			name:   "substring with diacritics",
			s:      "hello world",
			substr: "wörld",
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ContainsNormalized(tt.s, tt.substr)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCleanupText(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "single space",
			input: "hello world",
			want:  "hello world",
		},
		{
			name:  "multiple spaces",
			input: "hello    world",
			want:  "hello world",
		},
		{
			name:  "newlines and spaces",
			input: "hello\n\n  world  \n\n",
			want:  "hello world",
		},
		{
			name:  "tabs and spaces",
			input: "hello\t\t  world",
			want:  "hello world",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "only whitespace",
			input: "   \n\t   ",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CleanupText(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}
