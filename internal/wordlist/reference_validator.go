package wordlist

import (
	"fmt"
	"strings"

	"github.com/robalyx/rotector/internal/setup/config"
)

// ReferenceValidator handles cross-reference and self-reference validation.
type ReferenceValidator struct{}

// NewReferenceValidator creates a new ReferenceValidator instance.
func NewReferenceValidator() *ReferenceValidator {
	return &ReferenceValidator{}
}

// Validate performs cross-reference and self-reference validation.
func (v *ReferenceValidator) Validate(wordlist *config.Wordlist) []Issue {
	return v.checkCrossReferences(wordlist)
}

// checkCrossReferences finds cross-reference duplicates and self-references.
func (v *ReferenceValidator) checkCrossReferences(wordlist *config.Wordlist) []Issue {
	var issues []Issue

	// Build set of all terms
	termExists := make(map[string]struct{})
	for _, entry := range wordlist.Terms {
		termExists[entry.Term] = struct{}{}
	}

	for i, entry := range wordlist.Terms {
		for _, relatedTerm := range entry.RelatedTerms {
			// Self-reference check
			if strings.EqualFold(entry.Term, relatedTerm) {
				issues = append(issues, Issue{
					Type:        "self_reference",
					Description: fmt.Sprintf("Term '%s' lists itself as a related term", entry.Term),
					Term:        entry.Term,
					Location:    i,
				})
			}

			// Cross-reference duplicate check
			if _, exists := termExists[relatedTerm]; exists {
				issues = append(issues, Issue{
					Type:        "cross_reference_duplicate",
					Description: fmt.Sprintf("Related term '%s' in entry '%s' also exists as primary term", relatedTerm, entry.Term),
					Term:        entry.Term,
					Location:    i,
				})
			}
		}
	}

	return issues
}
