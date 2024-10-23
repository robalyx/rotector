package utils

import (
	"strings"
	"unicode"

	"github.com/invopop/jsonschema"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// normalizer is a transform.Transformer that normalizes a string.
var normalizer = transform.Chain( //nolint:gochecknoglobals
	norm.NFKC,
	norm.NFD,
	runes.Remove(runes.In(unicode.Mn)),
	runes.Remove(runes.In(unicode.Space)),
	cases.Lower(language.Und),
	norm.NFC,
)

// GenerateSchema generates a JSON schema for the given type.
func GenerateSchema[T any]() interface{} {
	reflector := jsonschema.Reflector{
		AllowAdditionalProperties: false,
		DoNotReference:            true,
	}
	var v T
	schema := reflector.Reflect(v)
	return schema
}

// NormalizeString removes diacritics, spaces, and converts to lowercase.
func NormalizeString(s string) string {
	result, _, _ := transform.String(normalizer, s)
	return result
}

// ContainsNormalized checks if substr is in s, after normalizing both.
func ContainsNormalized(s, substr string) bool {
	if s == "" || substr == "" {
		return false
	}
	return strings.Contains(NormalizeString(s), NormalizeString(substr))
}
