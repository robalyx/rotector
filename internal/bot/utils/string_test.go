package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTruncateString(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		maxLength int
		want      string
	}{
		{
			name:      "short string",
			input:     "hello",
			maxLength: 10,
			want:      "hello",
		},
		{
			name:      "long string",
			input:     "hello world this is a long string",
			maxLength: 10,
			want:      "hello w...",
		},
		{
			name:      "exact length",
			input:     "hello",
			maxLength: 5,
			want:      "hello",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TruncateString(tt.input, tt.maxLength)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestFormatString(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "simple string",
			input: "hello",
			want:  "```\nhello\n```",
		},
		{
			name:  "string with backticks",
			input: "hello `world`",
			want:  "```\nhello world\n```",
		},
		{
			name:  "string with multiple newlines",
			input: "hello\n\n\n\nworld",
			want:  "```\nhello\nworld\n```",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatString(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCensorString(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		streamerMode bool
		want         string
	}{
		{
			name:         "streamer mode off",
			input:        "sensitive",
			streamerMode: false,
			want:         "sensitive",
		},
		{
			name:         "short string",
			input:        "hi",
			streamerMode: true,
			want:         "XX",
		},
		{
			name:         "normal string",
			input:        "sensitive",
			streamerMode: true,
			want:         "senXXXive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CensorString(tt.input, tt.streamerMode)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCensorStringsInText(t *testing.T) {
	tests := []struct {
		name         string
		text         string
		streamerMode bool
		targets      []string
		want         string
	}{
		{
			name:         "streamer mode off",
			text:         "Hello World",
			streamerMode: false,
			targets:      []string{"Hello"},
			want:         "Hello World",
		},
		{
			name:         "single target",
			text:         "Hello World",
			streamerMode: true,
			targets:      []string{"Hello"},
			want:         "HXXlo World",
		},
		{
			name:         "multiple targets",
			text:         "Hello World Hello",
			streamerMode: true,
			targets:      []string{"Hello", "World"},
			want:         "HXXlo WXXld HXXlo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CensorStringsInText(tt.text, tt.streamerMode, tt.targets...)
			assert.Equal(t, tt.want, got)
		})
	}
}
