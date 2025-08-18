package wordlist

import (
	"fmt"
	"slices"
	"strings"

	"github.com/robalyx/rotector/internal/setup/config"
)

// FieldValidator handles required field and enum validation.
type FieldValidator struct{}

// NewFieldValidator creates a new FieldValidator instance.
func NewFieldValidator() *FieldValidator {
	return &FieldValidator{}
}

// Validate performs required field and enum validation.
func (v *FieldValidator) Validate(wordlist *config.Wordlist) []Issue {
	return v.checkRequiredFields(wordlist)
}

// checkRequiredFields validates required fields and enum values.
func (v *FieldValidator) checkRequiredFields(wordlist *config.Wordlist) []Issue {
	var issues []Issue

	validSeverities := []string{"critical", "high", "medium", "low"}
	validCategories := []string{"inappropriate_content", "social_engineering", "technical_evasion"}

	for i, entry := range wordlist.Terms {
		// Empty term
		if strings.TrimSpace(entry.Term) == "" {
			issues = append(issues, Issue{
				Type:        "empty_required_field",
				Description: fmt.Sprintf("Entry at position %d has empty term", i),
				Term:        "",
				Location:    i,
			})
		}

		// Empty meaning
		if strings.TrimSpace(entry.Meaning) == "" {
			issues = append(issues, Issue{
				Type:        "empty_required_field",
				Description: fmt.Sprintf("Term '%s' has no meaning", entry.Term),
				Term:        entry.Term,
				Location:    i,
			})
		}

		// Invalid severity
		if !slices.Contains(validSeverities, entry.Severity) {
			issues = append(issues, Issue{
				Type: "invalid_severity",
				Description: fmt.Sprintf("Term '%s' has invalid severity '%s' (must be: %s)",
					entry.Term, entry.Severity, strings.Join(validSeverities, ", ")),
				Term:     entry.Term,
				Location: i,
			})
		}

		// Invalid category
		if !slices.Contains(validCategories, entry.Category) {
			issues = append(issues, Issue{
				Type: "invalid_category",
				Description: fmt.Sprintf("Term '%s' has invalid category '%s' (must be: %s)",
					entry.Term, entry.Category, strings.Join(validCategories, ", ")),
				Term:     entry.Term,
				Location: i,
			})
		}

		// Validate RelatedTerms field
		if entry.RelatedTerms == nil {
			issues = append(issues, Issue{
				Type: "nil_related_terms",
				Description: fmt.Sprintf("Term '%s' has nil RelatedTerms field (should be empty slice instead)",
					entry.Term),
				Term:     entry.Term,
				Location: i,
			})
		}

		// Check for empty related terms
		for j, relatedTerm := range entry.RelatedTerms {
			if strings.TrimSpace(relatedTerm) == "" {
				issues = append(issues, Issue{
					Type: "empty_related_term",
					Description: fmt.Sprintf("Term '%s' has empty related term at position %d",
						entry.Term, j),
					Term:     entry.Term,
					Location: i,
				})
			}
		}
	}

	return issues
}
