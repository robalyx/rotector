package utils

import (
	"regexp"
	"strings"

	"golang.org/x/text/transform"
)

// MultipleSpaces matches any sequence of whitespace (including newlines).
var MultipleSpaces = regexp.MustCompile(`\s+`)

// NormalizeString cleans up text using the provided normalizer.
// Returns empty string if normalization fails or input is empty.
func NormalizeString(s string, normalizer transform.Transformer) string {
	if s == "" {
		return ""
	}

	if strings.TrimSpace(s) == "" {
		return ""
	}

	result, _, err := transform.String(normalizer, s)
	if err != nil || result == "" {
		return ""
	}
	return result
}

// ContainsNormalized checks if substr exists within s using the provided normalizer.
// Empty strings or normalization failures return false.
func ContainsNormalized(s, substr string, normalizer transform.Transformer) bool {
	if s == "" || substr == "" {
		return false
	}

	normalizedS := NormalizeString(s, normalizer)
	normalizedSubstr := NormalizeString(substr, normalizer)

	if normalizedS == "" || normalizedSubstr == "" {
		return strings.Contains(
			strings.ToLower(s),
			strings.ToLower(substr),
		)
	}

	return strings.Contains(normalizedS, normalizedSubstr)
}

// CleanupText removes extra whitespaces.
func CleanupText(s string) string {
	return strings.TrimSpace(MultipleSpaces.ReplaceAllString(s, " "))
}

// ValidateFlaggedWords checks if enough flagged words are found in the target text.
// Returns true if at least 30% of the flagged words are found in any of the target texts.
func ValidateFlaggedWords(flaggedContent []string, normalizer transform.Transformer, targetTexts ...string) bool {
	// Split all flagged content into words
	var allFlaggedWords []string
	for _, content := range flaggedContent {
		allFlaggedWords = append(allFlaggedWords, strings.Fields(content)...)
	}

	if len(allFlaggedWords) == 0 {
		return false
	}

	// Count how many flagged words are found in the target texts
	foundWords := 0
	for _, word := range allFlaggedWords {
		for _, text := range targetTexts {
			if ContainsNormalized(text, word, normalizer) {
				foundWords++
				break // Break inner loop once word is found in any text
			}
		}
	}

	// Check if at least 30% of the flagged words are found
	return float64(foundWords) >= 0.3*float64(len(allFlaggedWords))
}
