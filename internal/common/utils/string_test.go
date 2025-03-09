//nolint:lll
package utils_test

import (
	"fmt"
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
			normalizer := newTestNormalizer()
			got := utils.NormalizeString(tt.input, normalizer)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestContainsNormalized(t *testing.T) {
	t.Parallel()

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
			normalizer := newTestNormalizer()
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
		{
			name:         "short word handling",
			flaggedWords: []string{"a b c hello world"},
			targetTexts:  []string{"hello world"},
			want:         true,
		},
		{
			name:         "empty target texts",
			flaggedWords: []string{"hello world"},
			targetTexts:  []string{"", "  "},
			want:         false,
		},
		{
			name:         "empty flagged words",
			flaggedWords: []string{"", "  "},
			targetTexts:  []string{"hello world"},
			want:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			normalizer := newTestNormalizer()
			got := utils.ValidateFlaggedWords(tt.flaggedWords, normalizer, tt.targetTexts...)
			assert.Equal(t, tt.want, got)
		})
	}
}

func BenchmarkValidateFlaggedWords(b *testing.B) {
	normalizer := newTestNormalizer()
	flaggedWords := []string{
		"suspicious content here",
		"inappropriate language example",
		"something fishy going on",
	}
	targetTexts := []string{
		"This is a long text with some suspicious content here and there.",
		"We need to check if inappropriate language appears in this sentence.",
		"Nothing wrong with this text, it's completely fine.",
	}

	b.ResetTimer()
	for b.Loop() {
		utils.ValidateFlaggedWords(flaggedWords, normalizer, targetTexts...)
	}
}

func BenchmarkValidateFlaggedWords_SmallInput(b *testing.B) {
	normalizer := newTestNormalizer()
	flaggedWords := []string{"hello world"}
	targetTexts := []string{"hello there world"}

	b.ResetTimer()
	for b.Loop() {
		utils.ValidateFlaggedWords(flaggedWords, normalizer, targetTexts...)
	}
}

func BenchmarkValidateFlaggedWords_LargeInput(b *testing.B) {
	normalizer := newTestNormalizer()

	// Generate large inputs
	flaggedWords := make([]string, 20)
	for i := range flaggedWords {
		flaggedWords[i] = fmt.Sprintf("flagged content %d with multiple words to test performance", i)
	}

	targetTexts := make([]string, 10)
	for i := range targetTexts {
		targetTexts[i] = fmt.Sprintf("This is target text %d which contains some flagged content %d with multiple words", i, i%5)
	}

	b.ResetTimer()
	for b.Loop() {
		utils.ValidateFlaggedWords(flaggedWords, normalizer, targetTexts...)
	}
}

func BenchmarkValidateFlaggedWords_RealWorld(b *testing.B) {
	normalizer := newTestNormalizer()

	// Simulate AI-flagged content from user descriptions
	flaggedWords := []string{
		"contact me on discord username#1234",
		"join my private server for exclusive content",
		"send me a message for special experiences",
	}

	// Simulate actual user descriptions with repeated words and mixed formats
	targetTexts := []string{
		"Hi everyone! I love playing games and making friends. Contact me on Discord Username#1234 if you want to play together. I also like drawing and swimming.",
		"Professional game developer with 5 years experience. Join my private server for exclusive content and game development tips. I also post tutorials on my YouTube channel.",
		"Just a regular player looking for friends. I enjoy roleplay games and building. Send me a message for special experiences in my popular games. Always looking for new ideas!",
	}

	b.ResetTimer()
	for b.Loop() {
		utils.ValidateFlaggedWords(flaggedWords, normalizer, targetTexts...)
	}
}
