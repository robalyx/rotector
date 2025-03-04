package translator

import "strings"

// TranslateMorse converts Morse code to text.
func (t *Translator) TranslateMorse(morse string) string {
	var result strings.Builder

	// Split into words by forward slash
	words := strings.Split(morse, "/")

	for i, word := range words {
		if i > 0 {
			result.WriteString(" ")
		}

		// Process each letter in the word
		word = strings.TrimSpace(word)
		letters := strings.SplitSeq(word, " ")

		for letter := range letters {
			letter = strings.TrimSpace(letter)
			if letter == "" {
				continue
			}
			if text, ok := t.morseToText[letter]; ok {
				result.WriteString(text)
			}
		}
	}

	return result.String()
}

// IsMorseFormat checks if text appears to be in Morse code format.
func IsMorseFormat(text string) bool {
	// Check if text contains only valid morse characters
	cleaned := strings.Map(func(r rune) rune {
		switch r {
		case '.', '-', '/', ' ':
			return -1
		default:
			return r
		}
	}, text)

	return cleaned == "" && (strings.Contains(text, ".") || strings.Contains(text, "-"))
}
