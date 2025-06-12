package utils_test

import (
	"testing"

	"github.com/robalyx/rotector/pkg/utils"
	"github.com/stretchr/testify/assert"
)

func TestCompressAllWhitespace(t *testing.T) {
	t.Parallel()
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
			t.Parallel()
			got := utils.CompressAllWhitespace(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCompressWhitespacePreserveNewlines(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "single line",
			input: "hello    world",
			want:  "hello world",
		},
		{
			name: "multiple lines",
			input: `hello    world
				this  is  a  test
				preserve  newlines`,
			want: "hello world\nthis is a test\npreserve newlines",
		},
		{
			name: "empty lines",
			input: `
				hello    world

				this  is  a  test
				`,
			want: "hello world\n\nthis is a test",
		},
		{
			name:  "mixed line endings",
			input: "hello    world\r\nthis  is  a  test\rpreserve  newlines",
			want:  "hello world\nthis is a test\npreserve newlines",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "only whitespace",
			input: "   \n\t   \n   ",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := utils.CompressWhitespacePreserveNewlines(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestSplitLines(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input []string
		want  []string
	}{
		{
			name:  "empty input",
			input: []string{},
			want:  nil,
		},
		{
			name:  "no newlines",
			input: []string{"hello world", "test case"},
			want:  []string{"hello world", "test case"},
		},
		{
			name:  "with escaped newlines",
			input: []string{"hello\\nworld", "test\\ncase"},
			want:  []string{"hello", "world", "test", "case"},
		},
		{
			name:  "with regular newlines",
			input: []string{"hello\nworld", "test\ncase"},
			want:  []string{"hello", "world", "test", "case"},
		},
		{
			name:  "mixed types of newlines",
			input: []string{"hello\\nworld\ntest"},
			want:  []string{"hello", "world", "test"},
		},
		{
			name:  "with empty lines",
			input: []string{"hello\n\nworld", "\ntest\n\n"},
			want:  []string{"hello", "world", "test"},
		},
		{
			name:  "with whitespace",
			input: []string{"  hello  \n  world  "},
			want:  []string{"hello", "world"},
		},
		{
			name:  "complex example",
			input: []string{"male / bi\\nswitch (boys)\\ntop (girls)\\n\\n\\ngxy bottoms/switches or girls add me\\nrp ingame only"},
			want:  []string{"male / bi", "switch (boys)", "top (girls)", "gxy bottoms/switches or girls add me", "rp ingame only"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := utils.SplitLines(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseDelimitedInput(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		input     string
		delimiter string
		want      []string
	}{
		{
			name:      "empty input",
			input:     "",
			delimiter: ",",
			want:      nil,
		},
		{
			name:      "single item",
			input:     "item1",
			delimiter: ",",
			want:      []string{"item1"},
		},
		{
			name:      "multiple items with comma",
			input:     "item1,item2,item3",
			delimiter: ",",
			want:      []string{"item1", "item2", "item3"},
		},
		{
			name:      "items with spaces",
			input:     " item1 , item2 , item3 ",
			delimiter: ",",
			want:      []string{"item1", "item2", "item3"},
		},
		{
			name:      "empty items filtered out",
			input:     "item1,,item2,   ,item3",
			delimiter: ",",
			want:      []string{"item1", "item2", "item3"},
		},
		{
			name:      "newline delimiter",
			input:     "line1\nline2\nline3",
			delimiter: "\n",
			want:      []string{"line1", "line2", "line3"},
		},
		{
			name:      "newlines with spaces",
			input:     " line1 \n line2 \n line3 ",
			delimiter: "\n",
			want:      []string{"line1", "line2", "line3"},
		},
		{
			name:      "custom delimiter",
			input:     "item1|item2|item3",
			delimiter: "|",
			want:      []string{"item1", "item2", "item3"},
		},
		{
			name:      "only whitespace",
			input:     "   ,   ,   ",
			delimiter: ",",
			want:      nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := utils.ParseDelimitedInput(tt.input, tt.delimiter)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestValidateCommentText(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "valid basic text",
			input:    "This is a valid comment.",
			expected: true,
		},
		{
			name:     "valid with numbers",
			input:    "User123 has inappropriate content in outfit 456.",
			expected: true,
		},
		{
			name:     "valid with all allowed punctuation",
			input:    "User's profile contains inappropriate text, see description.",
			expected: true,
		},
		{
			name:     "valid with hyphens",
			input:    "Check the user's bio - it contains bad content.",
			expected: true,
		},
		{
			name:     "valid with newlines",
			input:    "First line.\nSecond line with valid content.",
			expected: true,
		},
		{
			name:     "empty string",
			input:    "",
			expected: false,
		},
		{
			name:     "invalid characters - special symbols",
			input:    "User has @#$% in their profile!",
			expected: false,
		},
		{
			name:     "invalid characters - unicode",
			input:    "User has émojis in their profile",
			expected: false,
		},
		{
			name:     "invalid characters - brackets",
			input:    "Check [this] user's profile",
			expected: false,
		},
		{
			name:     "invalid characters - question mark",
			input:    "Is this user appropriate?",
			expected: false,
		},
		{
			name:     "invalid characters - exclamation",
			input:    "This user is inappropriate!",
			expected: false,
		},
		{
			name:     "invalid characters - semicolon",
			input:    "User has bad content; check profile",
			expected: false,
		},
		{
			name:     "invalid characters - colon",
			input:    "Note: user has inappropriate content",
			expected: false,
		},
		{
			name:     "invalid characters - parentheses",
			input:    "User (ID: 123) has bad content",
			expected: false,
		},
		{
			name:     "mixed valid and invalid",
			input:    "Valid text with & invalid symbols",
			expected: false,
		},
		{
			name:     "only spaces",
			input:    "   ",
			expected: false,
		},
		{
			name:     "only punctuation",
			input:    "...,,,---'''",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := utils.ValidateCommentText(tt.input)
			if result != tt.expected {
				t.Errorf("ValidateCommentText(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}
