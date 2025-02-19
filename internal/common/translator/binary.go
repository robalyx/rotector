package translator

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ErrInvalidBinary is returned when the binary input is malformed or incomplete.
var ErrInvalidBinary = errors.New("invalid binary string")

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

// isBinaryFormat checks if text appears to be in binary format.
func isBinaryFormat(text string) bool {
	// Remove spaces and check for valid binary string
	cleaned := strings.ReplaceAll(text, " ", "")
	if len(cleaned) == 0 || len(cleaned)%8 != 0 {
		return false
	}

	return strings.IndexFunc(cleaned, func(r rune) bool {
		return r != '0' && r != '1'
	}) == -1
}
