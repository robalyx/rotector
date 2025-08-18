package utils

// GenerateMorphologicalVariations generates morphological variations of a base term.
// Only generates -s (plural) and -ed (past tense) variations.
func GenerateMorphologicalVariations(baseTerm string) []string {
	if len(baseTerm) < 2 {
		return []string{baseTerm}
	}

	variations := []string{baseTerm}

	// Add simple plural form
	variations = append(variations, baseTerm+"s")

	// Add simple past tense form with basic rules
	if len(baseTerm) > 0 && baseTerm[len(baseTerm)-1] == 'e' {
		// If ends with 'e', just add 'd' (trade -> traded)
		variations = append(variations, baseTerm+"d")
	} else {
		// Otherwise add 'ed' (play -> played)
		variations = append(variations, baseTerm+"ed")
	}

	return RemoveDuplicates(variations)
}

// RemoveDuplicates removes duplicate strings from a slice.
func RemoveDuplicates(strs []string) []string {
	seen := make(map[string]struct{})

	var result []string

	for _, str := range strs {
		if _, exists := seen[str]; !exists {
			result = append(result, str)
			seen[str] = struct{}{}
		}
	}

	return result
}
