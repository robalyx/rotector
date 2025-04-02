package utils_test

import (
	"testing"

	"github.com/robalyx/rotector/pkg/utils"
	"github.com/stretchr/testify/assert"
)

func TestTextNormalizer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		want     string
		contains string
		hasMatch bool
	}{
		{
			name:     "empty string",
			input:    "",
			want:     "",
			contains: "test",
			hasMatch: false,
		},
		{
			name:     "basic string",
			input:    "Hello World",
			want:     "hello world",
			contains: "hello",
			hasMatch: true,
		},
		{
			name:     "string with diacritics",
			input:    "héllo wörld",
			want:     "hello world",
			contains: "world",
			hasMatch: true,
		},
		{
			name:     "string with punctuation",
			input:    "hello! @world#",
			want:     "hello! @world#",
			contains: "hello!",
			hasMatch: true,
		},
		{
			name:     "mixed case with spaces",
			input:    "HéLLo   WöRLD",
			want:     "hello world",
			contains: "HELLO",
			hasMatch: true,
		},
		{
			name:     "no match in string",
			input:    "hello world",
			want:     "hello world",
			contains: "goodbye",
			hasMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			normalizer := utils.NewTextNormalizer()

			// Test Normalize
			got := normalizer.Normalize(tt.input)
			assert.Equal(t, tt.want, got)

			// Test Contains
			hasMatch := normalizer.Contains(tt.input, tt.contains)
			assert.Equal(t, tt.hasMatch, hasMatch)
		})
	}
}

func TestTextNormalizer_ValidateWords(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		flaggedWords []string
		targetTexts  []string
		want         bool
	}{
		{
			name:         "empty inputs",
			flaggedWords: []string{},
			targetTexts:  []string{},
			want:         false,
		},
		{
			name:         "simple match above threshold",
			flaggedWords: []string{"hello! world", "test case"},
			targetTexts:  []string{"hello! testing world case"},
			want:         true,
		},
		{
			name:         "match with diacritics and punctuation",
			flaggedWords: []string{"héllo! wörld"},
			targetTexts:  []string{"hello! world"},
			want:         true,
		},
		{
			name:         "match across multiple texts with punctuation",
			flaggedWords: []string{"hello! world test, case"},
			targetTexts:  []string{"hello! world", "test,", "case"},
			want:         true,
		},
		{
			name:         "insufficient matches",
			flaggedWords: []string{"hello world", "test case", "not found", "missing text", "another one"},
			targetTexts:  []string{"hello only"},
			want:         false,
		},
		{
			name:         "partial matches below threshold",
			flaggedWords: []string{"hello", "world", "test", "case", "example", "missing", "not found"},
			targetTexts:  []string{"hello world"},
			want:         false,
		},
		{
			name:         "exact threshold match",
			flaggedWords: []string{"hello", "world", "test"},
			targetTexts:  []string{"hello world"},
			want:         true,
		},
		{
			name:         "single word match",
			flaggedWords: []string{"hello"},
			targetTexts:  []string{"hello world"},
			want:         true,
		},
		{
			name:         "empty target text",
			flaggedWords: []string{"hello", "world"},
			targetTexts:  []string{""},
			want:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			normalizer := utils.NewTextNormalizer()
			got := normalizer.ValidateWords(tt.flaggedWords, tt.targetTexts...)
			assert.Equal(t, tt.want, got)
		})
	}
}

func BenchmarkTextNormalizer(b *testing.B) {
	normalizer := utils.NewTextNormalizer()
	text := "Hello World! This is a test string with Diacritics: héllo wörld"

	b.Run("Normalize", func(b *testing.B) {
		for b.Loop() {
			normalizer.Normalize(text)
		}
	})

	b.Run("Contains", func(b *testing.B) {
		for b.Loop() {
			normalizer.Contains(text, "hello")
		}
	})

	b.Run("ValidateWords/Small", func(b *testing.B) {
		flaggedWords := []string{"hello world", "test case"}
		targetTexts := []string{"hello testing world case"}

		for b.Loop() {
			normalizer.ValidateWords(flaggedWords, targetTexts...)
		}
	})

	b.Run("ValidateWords/Medium", func(b *testing.B) {
		flaggedWords := []string{
			"hello world",
			"test case",
			"example text",
			"sample content",
			"more words",
		}
		targetTexts := []string{
			"hello testing world case",
			"this is an example text",
			"some sample content here",
		}

		for b.Loop() {
			normalizer.ValidateWords(flaggedWords, targetTexts...)
		}
	})

	b.Run("ValidateWords/Large", func(b *testing.B) {
		flaggedWords := []string{
			"hello world",
			"test case",
			"example text",
			"sample content",
			"more words",
			"additional phrases",
			"benchmark testing",
			"performance check",
			"large dataset",
			"multiple entries",
		}
		targetTexts := []string{
			"hello testing world case example",
			"this is an example text with sample",
			"some sample content here and more",
			"additional text for benchmark testing",
			"checking performance with large dataset",
		}

		for b.Loop() {
			normalizer.ValidateWords(flaggedWords, targetTexts...)
		}
	})

	b.Run("ValidateWords/NoMatch", func(b *testing.B) {
		flaggedWords := []string{
			"unique phrase",
			"no matches",
			"missing text",
			"not found",
			"absent content",
		}
		targetTexts := []string{
			"completely different content",
			"nothing matches here",
			"testing performance",
		}

		for b.Loop() {
			normalizer.ValidateWords(flaggedWords, targetTexts...)
		}
	})
}
