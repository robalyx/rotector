package utils

import (
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// MultipleSpaces matches any sequence of whitespace (including newlines).
var MultipleSpaces = regexp.MustCompile(`\s+`)

// normalizer combines multiple transformers to clean up text:
// 1. NFKC converts compatibility characters to their canonical forms
// 2. NFD separates characters and their diacritical marks
// 3. Remove diacritical marks (Mn category)
// 4. Remove spaces
// 5. Convert to lowercase
// 6. NFC recombines characters into their canonical forms.
var normalizer = transform.Chain( //nolint:gochecknoglobals
	norm.NFKC,
	norm.NFD,
	runes.Remove(runes.In(unicode.Mn)),
	runes.Remove(runes.In(unicode.Space)),
	cases.Lower(language.Und),
	norm.NFC,
)

// NormalizeString cleans up text by:
// 1. Removing diacritical marks (é -> e)
// 2. Removing all spaces
// 3. Converting to lowercase.
// Returns empty string if normalization fails.
func NormalizeString(s string) string {
	if s == "" {
		return ""
	}

	result, _, err := transform.String(normalizer, s)
	if err != nil {
		return s
	}
	return result
}

// ContainsNormalized checks if substr exists within s by:
// 1. Normalizing both strings
// 2. Using strings.Contains for comparison
// Empty strings or normalization failures return false.
func ContainsNormalized(s, substr string) bool {
	if s == "" || substr == "" {
		return false
	}

	normalizedS := NormalizeString(s)
	if normalizedS == "" {
		return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
	}

	normalizedSubstr := NormalizeString(substr)
	if normalizedSubstr == "" {
		return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
	}

	return strings.Contains(normalizedS, normalizedSubstr)
}

// Add new cleanup function
// CleanupText removes extra whitespace, newlines, and ensures proper sentence spacing:
// 1. Replaces all whitespace sequences (including newlines) with a single space
// 2. Trims leading/trailing whitespace
// 3. Ensures exactly one space after periods.
func CleanupText(s string) string {
	// Replace all whitespace sequences with a single space and trim
	s = strings.TrimSpace(MultipleSpaces.ReplaceAllString(s, " "))
	// Fix double spaces after periods
	s = strings.ReplaceAll(s, ".  ", ". ")
	return s
}
