package utils

import (
	"math/rand"
	"strings"
)

// GenerateRandomWords generates a string of random words.
func GenerateRandomWords(count int) string {
	words := []string{
		"apple", "banana", "cherry", "dragon", "elephant",
		"flower", "guitar", "hammer", "island", "jungle",
		"kettle", "lemon", "mango", "needle", "orange",
		"pencil", "queen", "rabbit", "sunset", "tiger",
		"umbrella", "violin", "window", "yellow", "zebra",
	}

	selected := make([]string, count)
	for i := range count {
		selected[i] = words[rand.Intn(len(words))]
	}

	return strings.Join(selected, " ")
}
