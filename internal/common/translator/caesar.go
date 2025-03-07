package translator

import "strings"

// TranslateCaesar performs a Caesar cipher shift on alphabetic characters.
// The shift parameter specifies how many positions to shift (1-25).
func (t *Translator) TranslateCaesar(text string, shift int) string {
	// Normalize shift to be between 1 and 25
	shift = ((shift % 26) + 26) % 26
	if shift == 0 {
		return text
	}

	var result strings.Builder
	result.Grow(len(text))

	for _, char := range text {
		switch {
		case char >= 'A' && char <= 'Z':
			// Shift uppercase letters
			shifted := 'A' + (char-'A'+rune(shift))%26
			result.WriteRune(shifted)
		case char >= 'a' && char <= 'z':
			// Shift lowercase letters
			shifted := 'a' + (char-'a'+rune(shift))%26
			result.WriteRune(shifted)
		default:
			// Keep non-alphabetic characters unchanged
			result.WriteRune(char)
		}
	}

	return result.String()
}

// IsCaesarFormat checks if the text might be encoded with a Caesar cipher.
// This is a heuristic check that looks for a high proportion of alphabetic characters.
func IsCaesarFormat(text string) bool {
	if len(text) < 4 {
		return false
	}

	alphaCount := 0
	for _, char := range text {
		if (char >= 'A' && char <= 'Z') || (char >= 'a' && char <= 'z') {
			alphaCount++
		}
	}

	// Consider it a potential Caesar cipher if at least 70% of characters are letters
	return float64(alphaCount)/float64(len(text)) >= 0.7
}
