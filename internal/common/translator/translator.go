package translator

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/bytedance/sonic"
	"github.com/jaxron/axonet/pkg/client"
)

// ErrInvalidBinary is returned when the binary input is malformed or incomplete.
var ErrInvalidBinary = errors.New("invalid binary string")

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

// TranslateLanguage translates text between natural languages using Google Translate API.
// sourceLang and targetLang should be ISO 639-1 language codes (e.g., "en" for English).
// Returns the translated text and any error encountered during translation.
func (t *Translator) TranslateLanguage(ctx context.Context, text, sourceLang, targetLang string) (string, error) {
	// Send request to Google Translate API
	resp, err := t.client.NewRequest().
		Method(http.MethodGet).
		URL("https://translate.google.com/translate_a/single").
		Query("client", "gtx").
		Query("sl", sourceLang).
		Query("tl", targetLang).
		Query("dt", "t").
		Query("q", text).
		Do(ctx)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	// Read and parse the response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var result []interface{}
	if err := sonic.Unmarshal(body, &result); err != nil {
		return "", err
	}

	// Extract translated text from the response
	var translatedText strings.Builder
	for _, slice := range result[0].([]interface{}) {
		translatedText.WriteString(slice.([]interface{})[0].(string))
	}

	return translatedText.String(), nil
}

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
		letters := strings.Split(word, " ")

		for _, letter := range letters {
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

// TranslateBinary converts binary strings to text.
// Input should be space-separated 8-bit binary sequences.
// Each sequence is converted to its ASCII character representation.
func (t *Translator) TranslateBinary(binary string) (string, error) {
	// Remove all spaces
	binary = strings.ReplaceAll(binary, " ", "")

	var result strings.Builder
	// Process 8 bits at a time
	for i := 0; i < len(binary); i += 8 {
		if i+8 > len(binary) {
			return "", fmt.Errorf("%w: incomplete byte", ErrInvalidBinary)
		}

		// Convert binary byte to character
		num, err := strconv.ParseUint(binary[i:i+8], 2, 8)
		if err != nil {
			return "", err
		}

		result.WriteRune(rune(num))
	}

	return result.String(), nil
}

// Translate automatically detects and translates mixed content in the input string.
// It first attempts to translate any morse code or binary segments, then performs
// a single language translation on the entire resulting text if languages are specified.
func (t *Translator) Translate(ctx context.Context, input, sourceLang, targetLang string) (string, error) {
	// Split input into lines
	lines := strings.Split(strings.TrimSpace(input), "\n")
	var result strings.Builder

	// First pass: translate morse and binary segments
	for i, line := range lines {
		if i > 0 {
			result.WriteString("\n")
		}

		// Split line into segments based on format boundaries
		segments := t.splitIntoSegments(line)

		// Translate each segment
		for j, segment := range segments {
			if j > 0 {
				result.WriteString(" ")
			}

			// Translate morse and binary segments
			segment = strings.TrimSpace(segment)
			if segment == "" {
				continue
			}

			if isMorseFormat(segment) {
				result.WriteString(t.TranslateMorse(segment))
				continue
			}

			if isBinaryFormat(segment) {
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
func (t *Translator) splitIntoSegments(line string) []string {
	var segments []string
	var currentSegment strings.Builder
	words := strings.Fields(line)

	for i, word := range words {
		// Compare current and previous word formats directly
		if i > 0 {
			prevIsMorse := isMorseFormat(words[i-1])
			prevIsBinary := isBinaryFormat(words[i-1])
			currIsMorse := isMorseFormat(word)
			currIsBinary := isBinaryFormat(word)

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

// isMorseFormat checks if text appears to be in Morse code format.
func isMorseFormat(text string) bool {
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

// isBinaryFormat checks if text appears to be in binary format.
func isBinaryFormat(text string) bool {
	// Remove spaces and check for valid binary string
	cleaned := strings.ReplaceAll(text, " ", "")
	if len(cleaned)%8 != 0 {
		return false
	}

	return strings.IndexFunc(cleaned, func(r rune) bool {
		return r != '0' && r != '1'
	}) == -1
}
