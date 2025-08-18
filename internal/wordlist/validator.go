package wordlist

import (
	"github.com/robalyx/rotector/internal/setup/config"
)

// ValidateWordlist performs all validation checks on the wordlist.
func ValidateWordlist(wordlist *config.Wordlist) []Issue {
	var issues []Issue

	if wordlist == nil || len(wordlist.Terms) == 0 {
		issues = append(issues, Issue{
			Type:        "empty_wordlist",
			Description: "Wordlist is empty or could not be loaded",
			Term:        "",
			Location:    -1,
		})

		return issues
	}

	// Create validators
	validators := []Validator{
		NewDuplicateValidator(),
		NewReferenceValidator(),
		NewFieldValidator(),
		NewMorphologyValidator(),
		NewRelatedValidator(),
	}

	// Run all validation checks
	for _, validator := range validators {
		issues = append(issues, validator.Validate(wordlist)...)
	}

	return issues
}
