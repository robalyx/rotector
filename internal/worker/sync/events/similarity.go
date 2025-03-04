package events

// calculateStringSimilarity computes a normalized similarity score between two strings.
// Returns a value between 0.0 (completely different) and 1.0 (identical).
func calculateStringSimilarity(s1, s2 string) float64 {
	// Handle edge cases
	if s1 == s2 {
		return 1.0
	}
	if len(s1) == 0 || len(s2) == 0 {
		return 0.0
	}

	// Calculate Levenshtein distance
	distance := levenshteinDistance(s1, s2)

	// Normalize the distance to a similarity score between 0 and 1
	maxLen := float64(max(len(s1), len(s2)))
	similarity := 1.0 - float64(distance)/maxLen

	return similarity
}

// levenshteinDistance calculates the edit distance between two strings.
// The distance is the minimum number of single-character edits
// (insertions, deletions, or substitutions) required to change one string into another.
func levenshteinDistance(s1, s2 string) int {
	// Convert strings to runes to handle Unicode correctly
	runes1 := []rune(s1)
	runes2 := []rune(s2)

	// Create distance matrix
	rows, cols := len(runes1)+1, len(runes2)+1
	dist := make([][]int, rows)
	for i := range dist {
		dist[i] = make([]int, cols)
		dist[i][0] = i
	}
	for j := 1; j < cols; j++ {
		dist[0][j] = j
	}

	// Fill in the distance matrix
	for i := 1; i < rows; i++ {
		for j := 1; j < cols; j++ {
			cost := 1
			if runes1[i-1] == runes2[j-1] {
				cost = 0
			}
			dist[i][j] = min(
				dist[i-1][j]+1,      // deletion
				dist[i][j-1]+1,      // insertion
				dist[i-1][j-1]+cost, // substitution
			)
		}
	}

	return dist[rows-1][cols-1]
}
