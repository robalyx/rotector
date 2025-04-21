package utils_test

import (
	"testing"

	"github.com/robalyx/rotector/internal/bot/utils"
	"github.com/stretchr/testify/assert"
)

func TestTruncateString(t *testing.T) {
	t.Parallel()
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
			t.Parallel()
			got := utils.TruncateString(tt.input, tt.maxLength)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestFormatString(t *testing.T) {
	t.Parallel()
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
			t.Parallel()
			got := utils.FormatString(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCensorString(t *testing.T) {
	t.Parallel()
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
			t.Parallel()
			got := utils.CensorString(tt.input, tt.streamerMode)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCensorStringsInText(t *testing.T) {
	t.Parallel()
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
			t.Parallel()
			got := utils.CensorStringsInText(tt.text, tt.streamerMode, tt.targets...)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestNormalizeString(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "simple string",
			input: "hello world",
			want:  "hello world",
		},
		{
			name:  "string with backticks",
			input: "hello `world`",
			want:  "hello world",
		},
		{
			name:  "string with multiple newlines",
			input: "hello\n\n\nworld",
			want:  "hello world",
		},
		{
			name:  "string with mixed spaces and newlines",
			input: "hello   world\n\n  test",
			want:  "hello world test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := utils.NormalizeString(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCalculateDynamicTruncationLength(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		numReasons int
		want       int
	}{
		{
			name:       "no reasons",
			numReasons: 0,
			want:       1004, // discordFieldLimit(1024) - formattingOverhead(20)
		},
		{
			name:       "negative reasons",
			numReasons: -1,
			want:       1004, // discordFieldLimit(1024) - formattingOverhead(20)
		},
		{
			name:       "single reason",
			numReasons: 1,
			want:       1004, // (1024 - 20) / 1 = 1004
		},
		{
			name:       "two reasons",
			numReasons: 2,
			want:       492, // (1024 - 40) / 2 = 492
		},
		{
			name:       "five reasons",
			numReasons: 5,
			want:       184, // (1024 - 100) / 5 = 184.8 ≈ 184
		},
		{
			name:       "ten reasons",
			numReasons: 10,
			want:       82, // (1024 - 200) / 10 = 82.4 ≈ 82
		},
		{
			name:       "twenty reasons",
			numReasons: 20,
			want:       64, // (1024 - 400) / 20 = 31.2, floored to min 64
		},
		{
			name:       "fifty reasons",
			numReasons: 50,
			want:       64, // Would be too small, floored to min 64
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := utils.CalculateDynamicTruncationLength(tt.numReasons)
			assert.Equal(t, tt.want, got)
		})
	}
}
