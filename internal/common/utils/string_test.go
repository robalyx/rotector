package utils_test

import (
	"testing"

	"github.com/robalyx/rotector/internal/common/utils"
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
