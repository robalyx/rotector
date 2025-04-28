package translator

import (
	"context"
	"strings"

	"github.com/jaxron/axonet/pkg/client"
)

// Translator handles text translation between different formats.
type Translator struct {
	client      *client.Client
	morseToText map[string]string
}

// New creates a Translator with the provided HTTP client.
func New(client *client.Client) *Translator {
	return &Translator{
		client: client,
		// Standard International Morse Code mapping including letters, numbers, and punctuation
		morseToText: map[string]string{
			".-": "A", "-...": "B", "-.-.": "C", "-..": "D", ".": "E",
			"..-.": "F", "--.": "G", "....": "H", "..": "I", ".---": "J",
			"-.-": "K", ".-..": "L", "--": "M", "-.": "N", "---": "O",
			".--.": "P", "--.-": "Q", ".-.": "R", "...": "S", "-": "T",
			"..-": "U", "...-": "V", ".--": "W", "-..-": "X", "-.--": "Y",
			"--..": "Z", ".----": "1", "..---": "2", "...--": "3", "....-": "4",
			".....": "5", "-....": "6", "--...": "7", "---..": "8", "----.": "9",
			"-----": "0", "..--..": "?", "-.-.--": "!", ".-.-.-": ".",
			"--..--": ",", "---...": ":", ".----.": "'", ".-..-.": "\"",
		},
	}
}

// Translate automatically detects and translates mixed content in the input string.
// It first attempts to translate any morse code or binary segments, then performs
// a single language translation on the entire resulting text if languages are specified.
func (t *Translator) Translate(ctx context.Context, input, sourceLang, targetLang string) (string, error) {
	// Skip translation for simple content
	if shouldSkipTranslation(input) {
		return input, nil
	}

	// Split input into lines
	lines := strings.Split(strings.TrimSpace(input), "\n")
	var result strings.Builder

	// First pass: translate morse, binary, and caesar cipher segments
	for i, line := range lines {
		if i > 0 {
			result.WriteString("\n")
		}

		// Split line into segments based on format boundaries
		segments := splitIntoSegments(line)

		// Translate each segment
		for j, segment := range segments {
			if j > 0 {
				result.WriteString(" ")
			}

			// Translate morse, binary, and caesar segments
			segment = strings.TrimSpace(segment)
			if segment == "" {
				continue
			}

			if IsMorseFormat(segment) {
				result.WriteString(t.TranslateMorse(segment))
				continue
			}

			if IsBinaryFormat(segment) {
				if translated, err := t.TranslateBinary(segment); err == nil {
					result.WriteString(translated)
					continue
				}
			}

			result.WriteString(segment)
		}
	}

	// Second pass: translate the entire text if language translation is requested
	if sourceLang != "" && targetLang != "" {
		translated, err := t.TranslateLanguage(ctx, result.String(), sourceLang, targetLang)
		if err != nil {
			return "", err
		}
		return translated, nil
	}

	return result.String(), nil
}

// splitIntoSegments splits a line into segments based on format boundaries.
// Spaces between segments are preserved in the output.
func splitIntoSegments(line string) []string {
	var segments []string
	var currentSegment strings.Builder
	words := strings.Fields(line)

	for i, word := range words {
		// Compare current and previous word formats directly
		if i > 0 {
			prevIsMorse := IsMorseFormat(words[i-1])
			prevIsBinary := IsBinaryFormat(words[i-1])
			currIsMorse := IsMorseFormat(word)
			currIsBinary := IsBinaryFormat(word)

			if (prevIsMorse != currIsMorse || prevIsBinary != currIsBinary) && currentSegment.Len() > 0 {
				segments = append(segments, strings.TrimSpace(currentSegment.String()))
				currentSegment.Reset()
			}
		}

		if currentSegment.Len() > 0 {
			currentSegment.WriteString(" ")
		}
		currentSegment.WriteString(word)
	}

	if currentSegment.Len() > 0 {
		segments = append(segments, strings.TrimSpace(currentSegment.String()))
	}

	return segments
}

// shouldSkipTranslation checks if the content is too simple or formatted in a way
// that doesn't require translation.
func shouldSkipTranslation(text string) bool {
	// Skip short content
	if len(text) <= 4 {
		return true
	}

	// Skip deleted content
	if strings.EqualFold(text, "[ Content Deleted ]") {
		return true
	}

	// Skip repeated characters
	if len(text) > 0 {
		firstChar := text[0]
		allSame := true
		for i := 1; i < len(text); i++ {
			if text[i] != firstChar {
				allSame = false
				break
			}
		}
		if allSame {
			return true
		}
	}

	return false
}
