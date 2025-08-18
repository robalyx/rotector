package wordlist

import (
	"fmt"
	"strings"

	"github.com/robalyx/rotector/internal/setup/config"
)

// RelatedValidator handles related terms duplicate validation.
type RelatedValidator struct{}

// NewRelatedValidator creates a new RelatedValidator instance.
func NewRelatedValidator() *RelatedValidator {
	return &RelatedValidator{}
}

// Validate performs related terms duplicate validation.
func (v *RelatedValidator) Validate(wordlist *config.Wordlist) []Issue {
	return v.checkDuplicateRelatedTerms(wordlist)
}

// checkDuplicateRelatedTerms finds related terms that appear in multiple entries.
func (v *RelatedValidator) checkDuplicateRelatedTerms(wordlist *config.Wordlist) []Issue {
	var issues []Issue

	// Build a map of related terms to the indices that use them
	relatedTermUsage := make(map[string][]int)

	for i, entry := range wordlist.Terms {
		for _, relatedTerm := range entry.RelatedTerms {
			key := strings.ToLower(relatedTerm)
			relatedTermUsage[key] = append(relatedTermUsage[key], i)
		}
	}

	// Find related terms used in multiple entries
	for relatedTerm, indices := range relatedTermUsage {
		if len(indices) > 1 {
			// Build description with all primary terms that use this related term
			var primaryTerms []string
			for _, index := range indices {
				primaryTerms = append(primaryTerms, fmt.Sprintf("'%s'", wordlist.Terms[index].Term))
			}

			// Report on the first occurrence
			issues = append(issues, Issue{
				Type: "duplicate_related_term",
				Description: fmt.Sprintf("Related term '%s' appears in multiple entries (%s) - "+
					"consider making '%s' a primary term instead", relatedTerm, strings.Join(primaryTerms, ", "), relatedTerm),
				Term:     wordlist.Terms[indices[0]].Term, // Report on first occurrence
				Location: indices[0],
			})
		}
	}

	return issues
}
