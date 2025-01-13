package utils

import (
	cryptoRand "crypto/rand"
	"encoding/base64"
	"math/rand"
	"strings"
)

// GenerateRandomWords generates a string of random words.
func GenerateRandomWords(count int) string {
	words := []string{
		"apple", "banana", "cherry", "dragon", "elephant",
		"flower", "guitar", "hammer", "island", "jungle",
		"kettle", "lemon", "monkey", "needle", "orange",
		"pencil", "queen", "rabbit", "sunset", "tiger",
		"umbrella", "violin", "window", "yellow", "zebra",
	}

	selected := make([]string, count)
	for i := range count {
		selected[i] = words[rand.Intn(len(words))]
	}

	return strings.Join(selected, " ")
}

// GenerateSecureToken generates a cryptographically secure random token of the specified length.
// The resulting string is URL-safe base64 encoded.
func GenerateSecureToken(length int) string {
	b := make([]byte, length)
	if _, err := cryptoRand.Read(b); err != nil {
		panic(err) // This should never happen
	}
	return base64.URLEncoding.EncodeToString(b)[:length]
}
