package utils_test

import (
	"testing"
	"unicode"

	"github.com/robalyx/rotector/internal/common/utils"
	"github.com/stretchr/testify/assert"
	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

func newTestNormalizer() transform.Transformer {
	return transform.Chain(
		norm.NFKD,                             // Decompose with compatibility decomposition
		runes.Remove(runes.In(unicode.Mn)),    // Remove non-spacing marks
		runes.Remove(runes.In(unicode.P)),     // Remove punctuation
		runes.Map(unicode.ToLower),            // Convert to lowercase before normalization
		norm.NFKC,                             // Normalize with compatibility composition
		runes.Remove(runes.In(unicode.Space)), // Remove spaces last
	)
}

func TestNormalizeString(t *testing.T) {
	t.Parallel()
	normalizer := newTestNormalizer()
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
			want:  "helloworld",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := utils.NormalizeString(tt.input, normalizer)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestContainsNormalized(t *testing.T) {
	t.Parallel()
	normalizer := newTestNormalizer()
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
			t.Parallel()
			got := utils.ContainsNormalized(tt.s, tt.substr, normalizer)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCleanupText(t *testing.T) {
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
			got := utils.CleanupText(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestValidateFlaggedWords(t *testing.T) {
	t.Parallel()
	normalizer := newTestNormalizer()
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
			flaggedWords: []string{"hello world", "test case"},
			targetTexts:  []string{"hello testing world case"},
			want:         true,
		},
		{
			name:         "match with diacritics",
			flaggedWords: []string{"héllo wörld"},
			targetTexts:  []string{"hello world"},
			want:         true,
		},
		{
			name:         "below threshold match",
			flaggedWords: []string{"hello world test case"},
			targetTexts:  []string{"only hello here"},
			want:         false,
		},
		{
			name:         "match across multiple texts",
			flaggedWords: []string{"hello world test case"},
			targetTexts:  []string{"hello world", "test", "case"},
			want:         true,
		},
		{
			name:         "case insensitive match",
			flaggedWords: []string{"Hello World TEST case"},
			targetTexts:  []string{"hello world test CASE"},
			want:         true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := utils.ValidateFlaggedWords(tt.flaggedWords, normalizer, tt.targetTexts...)
			assert.Equal(t, tt.want, got)
		})
	}
}
