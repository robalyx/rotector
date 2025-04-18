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
