package wordlist

import (
	"fmt"
	"sort"
	"strings"

	"github.com/robalyx/rotector/internal/setup/config"
)

// DuplicateValidator handles exact duplicate and substring redundancy validation.
type DuplicateValidator struct{}

// NewDuplicateValidator creates a new DuplicateValidator instance.
func NewDuplicateValidator() *DuplicateValidator {
	return &DuplicateValidator{}
}

// Validate performs duplicate and substring redundancy validation.
func (v *DuplicateValidator) Validate(wordlist *config.Wordlist) []Issue {
	var issues []Issue

	issues = append(issues, v.checkExactDuplicates(wordlist)...)
	issues = append(issues, v.checkSubstringRedundancy(wordlist)...)

	return issues
}

// checkExactDuplicates finds exact duplicate terms.
func (v *DuplicateValidator) checkExactDuplicates(wordlist *config.Wordlist) []Issue {
	var issues []Issue

	seen := make(map[string]int)

	for i, entry := range wordlist.Terms {
		if prevIndex, exists := seen[entry.Term]; exists {
			issues = append(issues, Issue{
				Type:        "exact_duplicate",
				Description: fmt.Sprintf("Term '%s' appears multiple times (positions %d and %d)", entry.Term, prevIndex, i),
				Term:        entry.Term,
				Location:    i,
			})
		} else {
			seen[entry.Term] = i
		}
	}

	return issues
}

// checkSubstringRedundancy finds terms that are redundant because they're substrings of other terms.
func (v *DuplicateValidator) checkSubstringRedundancy(wordlist *config.Wordlist) []Issue {
	var issues []Issue

	// Create a slice of entries with their original indices, sorted by term length
	type indexedEntry struct {
		entry config.WordlistEntry
		index int
	}

	entries := make([]indexedEntry, len(wordlist.Terms))
	for i, entry := range wordlist.Terms {
		entries[i] = indexedEntry{entry, i}
	}

	// Sort by term length for more efficient comparison
	sort.Slice(entries, func(i, j int) bool {
		return len(entries[i].entry.Term) < len(entries[j].entry.Term)
	})

	// Check each term against longer terms
	for i, entry1 := range entries {
		for j := i + 1; j < len(entries); j++ {
			entry2 := entries[j]
			// Check if the shorter term appears as a complete word within the longer term
			if v.isCompleteWordSubstring(entry1.entry.Term, entry2.entry.Term) {
				issues = append(issues, Issue{
					Type:        "substring_redundancy",
					Description: fmt.Sprintf("Term '%s' is redundant because it appears as a complete word in '%s'", entry1.entry.Term, entry2.entry.Term),
					Term:        entry1.entry.Term,
					Location:    entry1.index,
				})

				break
			}
		}
	}

	return issues
}

// isCompleteWordSubstring checks if the shorter term appears as a complete word in the longer term.
func (v *DuplicateValidator) isCompleteWordSubstring(short, long string) bool {
	shortLower := strings.ToLower(short)
	longLower := strings.ToLower(long)

	// Split the long string into words and check each one
	words := strings.FieldsFunc(longLower, func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9')
	})

	for _, word := range words {
		if word == shortLower {
			return true
		}
	}

	return false
}
