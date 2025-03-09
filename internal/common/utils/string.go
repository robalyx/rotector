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

// ValidateFlaggedWords checks if a sufficient percentage of flagged words appear in target texts.
// Returns true when at least 30% of unique words from flagged content are found in any target text.
func ValidateFlaggedWords(flaggedContent []string, normalizer transform.Transformer, targetTexts ...string) bool {
	// Quick check for empty inputs
	if len(flaggedContent) == 0 || len(targetTexts) == 0 {
		return false
	}

	// Filter and preprocess target texts once
	normalizedTargets := make([]string, 0, len(targetTexts))
	for _, text := range targetTexts {
		if text = strings.TrimSpace(text); text == "" {
			continue
		}

		if norm := NormalizeString(text, normalizer); norm != "" {
			normalizedTargets = append(normalizedTargets, norm)
		}
	}

	if len(normalizedTargets) == 0 {
		return false
	}

	// Collect unique words and count total
	uniqueWords := make(map[string]struct{})
	for _, content := range flaggedContent {
		if content = strings.TrimSpace(content); content == "" {
			continue
		}

		for _, word := range strings.Fields(content) {
			if len(word) < 2 {
				continue
			}
			uniqueWords[word] = struct{}{}
		}
	}

	// Check if there are any unique words to check
	totalWords := len(uniqueWords)
	if totalWords == 0 {
		return false
	}

	// Calculate threshold
	threshold := max(int(float64(totalWords)*0.3), 1)

	// Check each unique word
	matchCount := 0
	for word := range uniqueWords {
		// Normalize this word
		normalizedWord := NormalizeString(word, normalizer)
		if normalizedWord == "" {
			continue
		}

		// Check if this word exists in any target
		for _, target := range normalizedTargets {
			if strings.Contains(target, normalizedWord) {
				matchCount++

				// Early exit if threshold reached
				if matchCount >= threshold {
					return true
				}

				break // Found in at least one target, check next word
			}
		}
	}

	// Not enough matches found
	return false
}
