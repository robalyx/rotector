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

	var maxTargetLength int

	for _, text := range targetTexts {
		if text = strings.TrimSpace(text); text == "" {
			continue
		}

		if norm := n.Normalize(text); norm != "" {
			normalizedTargets = append(normalizedTargets, norm)
			if len(norm) > maxTargetLength {
				maxTargetLength = len(norm)
			}
		}
	}

	if len(normalizedTargets) == 0 {
		return false
	}

	// Check each flagged content length against the longest target
	for _, content := range flaggedContent {
		if content = strings.TrimSpace(content); content == "" {
			continue
		}

		if norm := n.Normalize(content); norm != "" {
			// Fail if any single piece of evidence is more than 2x the longest target
			if len(norm) > maxTargetLength*2 {
				return false
			}
		}
	}

	// Collect all unique words from flagged content
	allWords := make(map[string]struct{})
	// Track content that doesn't have valid words
	shortContent := make([]string, 0)

	for _, content := range flaggedContent {
		if content = strings.TrimSpace(content); content == "" {
			continue
		}

		hasValidWords := false

		for _, word := range strings.Fields(content) {
			if len(word) < 3 {
				continue
			}

			normalizedWord := n.Normalize(word)
			if normalizedWord == "" {
				continue
			}

			allWords[normalizedWord] = struct{}{}
			hasValidWords = true
		}

		// If content has no valid words (e.g., emoticons), track it for exact matching
		if !hasValidWords {
			if normalized := n.Normalize(content); normalized != "" {
				shortContent = append(shortContent, normalized)
			}
		}
	}

	// Check short content matches
	hasShortMatch := false

	if len(shortContent) > 0 {
		// Combine all targets into one string for efficient searching
		combinedTargets := strings.Join(normalizedTargets, " ")
		for _, short := range shortContent {
			if strings.Contains(combinedTargets, short) {
				hasShortMatch = true
				break // Found at least one match, that's enough
			}
		}
	}

	// If we only have short content, return based on exact matches
	if len(allWords) == 0 {
		return hasShortMatch
	}

	// For mixed content, check word matches
	totalWords := len(allWords)
	if totalWords == 0 {
		return hasShortMatch // Fallback to short content matches
	}

	// Calculate overall threshold for words
	threshold := int(math.Ceil(float64(totalWords) * 0.8)) // 80% of total words
	threshold = max(threshold, 3)                          // At least 3 matches required
	threshold = min(threshold, totalWords)                 // Don't exceed total words

	// Check matches across all word content
	wordMatches := 0

	for normalizedWord := range allWords {
		// Check if word exists in any target
		for _, target := range normalizedTargets {
			if strings.Contains(target, normalizedWord) {
				wordMatches++
				break // Found in one target, move to next word
			}
		}
	}

	// For mixed content, return true if either short content matches OR word threshold is met
	if len(shortContent) > 0 {
		return hasShortMatch || wordMatches >= threshold
	}

	// For word-only content, return based on threshold
	return wordMatches >= threshold
}
