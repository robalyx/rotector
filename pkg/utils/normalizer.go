package utils

import (
	"math"
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// TextNormalizer wraps transform.Transformer to provide convenient string normalization methods.
// This is not safe for concurrent use.
type TextNormalizer struct {
	transformer transform.Transformer
}

// NewTextNormalizer creates a new TextNormalizer instance.
func NewTextNormalizer() *TextNormalizer {
	return &TextNormalizer{
		transformer: transform.Chain(
			norm.NFKD,                          // Decompose with compatibility decomposition
			runes.Remove(runes.In(unicode.Mn)), // Remove non-spacing marks
			runes.Map(unicode.ToLower),         // Convert to lowercase before normalization
			norm.NFKC,                          // Normalize with compatibility composition
		),
	}
}

// Normalize cleans up text using the normalizer.
// Returns empty string if normalization fails or input is empty.
func (n *TextNormalizer) Normalize(s string) string {
	// Return empty string if input is empty
	if s == "" {
		return ""
	}

	// Clean up whitespace while preserving newlines
	s = CompressWhitespacePreserveNewlines(s)
	if s == "" {
		return ""
	}

	// Normalize the text
	result, _, err := transform.String(n.transformer, s)
	if err != nil || result == "" {
		return ""
	}

	return result
}

// Contains checks if substr exists within s using the normalizer.
// Empty strings or normalization failures return false.
func (n *TextNormalizer) Contains(s, substr string) bool {
	if s == "" || substr == "" {
		return false
	}

	normalizedS := n.Normalize(s)
	normalizedSubstr := n.Normalize(substr)

	if normalizedS == "" || normalizedSubstr == "" {
		return strings.Contains(
			strings.ToLower(s),
			strings.ToLower(substr),
		)
	}

	return strings.Contains(normalizedS, normalizedSubstr)
}

// ValidateWords checks if a sufficient percentage of flagged words appear in target texts.
func (n *TextNormalizer) ValidateWords(flaggedContent []string, targetTexts ...string) bool {
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

		if norm := n.Normalize(text); norm != "" {
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
	threshold := int(math.Ceil(float64(totalWords) * 0.4)) // 40% of total words
	threshold = max(threshold, 2)                          // At least 2 matches required regardless of word count
	threshold = min(threshold, totalWords)                 // Ensure threshold doesn't exceed total words

	// Check each unique word
	matchCount := 0
	for word := range uniqueWords {
		// Normalize this word
		normalizedWord := n.Normalize(word)
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
