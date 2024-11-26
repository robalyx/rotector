package utils

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/invopop/jsonschema"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

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

// GenerateSchema creates a JSON schema for validating data structures.
// It uses reflection to analyze the type T and build a schema that:
// - Disallows additional properties
// - Includes all fields directly (no references)
// - Adds descriptions from jsonschema tags.
func GenerateSchema[T any]() interface{} {
	reflector := jsonschema.Reflector{
		AllowAdditionalProperties: false,
		DoNotReference:            true,
	}
	var v T
	schema := reflector.Reflect(v)
	return schema
}

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
		return ""
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
	normalizedSubstr := NormalizeString(substr)

	if normalizedS == "" || normalizedSubstr == "" {
		return false
	}

	return strings.Contains(normalizedS, normalizedSubstr)
}

// FormatIDs formats a slice of user IDs into a readable string with mentions.
func FormatIDs(ids []uint64) string {
	if len(ids) == 0 {
		return "None"
	}

	mentions := make([]string, len(ids))
	for i, id := range ids {
		mentions[i] = fmt.Sprintf("<@%d>", id)
	}
	return strings.Join(mentions, ", ")
}
