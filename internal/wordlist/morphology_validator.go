package wordlist

import (
	"fmt"
	"strings"

	"github.com/robalyx/rotector/internal/setup/config"
	"github.com/robalyx/rotector/pkg/utils"
)

const (
	minTermLength     = 2
	maxTermLength     = 25
	minSuffixLength   = 2 // for -s suffix
	minEdSuffixLength = 4 // for -ed suffix
)

// MorphologyValidator handles morphological redundancy validation.
type MorphologyValidator struct{}

// NewMorphologyValidator creates a new MorphologyValidator instance.
func NewMorphologyValidator() *MorphologyValidator {
	return &MorphologyValidator{}
}

// Validate performs morphological redundancy validation.
func (v *MorphologyValidator) Validate(wordlist *config.Wordlist) []Issue {
	primaryTerms := make(map[string]int)
	for i, entry := range wordlist.Terms {
		primaryTerms[strings.ToLower(entry.Term)] = i
	}

	return v.checkMorphologicalRedundancy(wordlist, primaryTerms)
}

// checkMorphologicalRedundancy finds terms that are morphological variations of other terms.
func (v *MorphologyValidator) checkMorphologicalRedundancy(wordlist *config.Wordlist, primaryTerms map[string]int) []Issue {
	var issues []Issue

	for i, entry := range wordlist.Terms {
		issues = append(issues, v.checkTermQuality(entry, i)...)
		issues = append(issues, v.checkPrimaryTermSimplification(entry, i, primaryTerms)...)
		issues = append(issues, v.checkPrimaryTermRedundancy(entry, i, primaryTerms)...)
		issues = append(issues, v.checkRelatedTermRedundancy(entry, i, primaryTerms)...)
	}

	return issues
}

// checkPrimaryTermSimplification checks if a primary term could be simplified to a base form.
func (v *MorphologyValidator) checkPrimaryTermSimplification(
	entry config.WordlistEntry, location int, primaryTerms map[string]int,
) []Issue {
	var issues []Issue

	term := strings.ToLower(entry.Term)

	// Check for morphological suffixes
	suffixes := []struct {
		suffix    string
		minLength int
	}{
		{"s", minSuffixLength},
		{"ed", minEdSuffixLength},
	}

	for _, s := range suffixes {
		if base, exists := checkMorphologicalSuffix(term, s.suffix, s.minLength, primaryTerms); exists {
			issues = append(issues, Issue{
				Type: "morphological_redundancy",
				Description: fmt.Sprintf("Primary term '%s' is a morphological variation of existing term '%s' - "+
					"remove '%s' as checker handles this automatically", entry.Term, base, entry.Term),
				Term:     entry.Term,
				Location: location,
			})
		}
	}

	return issues
}

// checkPrimaryTermRedundancy checks if a primary term is a variation of another primary term.
func (v *MorphologyValidator) checkPrimaryTermRedundancy(
	entry config.WordlistEntry, location int, primaryTerms map[string]int,
) []Issue {
	var issues []Issue

	for otherTerm, otherIndex := range primaryTerms {
		if location != otherIndex {
			variations := utils.GenerateMorphologicalVariations(otherTerm)
			for _, variation := range variations {
				if strings.EqualFold(entry.Term, variation) && !strings.EqualFold(entry.Term, otherTerm) {
					issues = append(issues, Issue{
						Type: "morphological_redundancy",
						Description: fmt.Sprintf("Term '%s' is a morphological variation of '%s' - "+
							"remove '%s' as checker handles this automatically", entry.Term, otherTerm, entry.Term),
						Term:     entry.Term,
						Location: location,
					})

					break
				}
			}
		}
	}

	return issues
}

// checkRelatedTermRedundancy checks if related terms are morphological variations.
func (v *MorphologyValidator) checkRelatedTermRedundancy(
	entry config.WordlistEntry, location int, primaryTerms map[string]int,
) []Issue {
	var issues []Issue

	// Check if related terms are variations of the primary term
	primaryVariations := utils.GenerateMorphologicalVariations(entry.Term)

	primaryVariationSet := make(map[string]struct{})
	for _, variation := range primaryVariations {
		primaryVariationSet[strings.ToLower(variation)] = struct{}{}
	}

	for _, relatedTerm := range entry.RelatedTerms {
		if _, exists := primaryVariationSet[strings.ToLower(relatedTerm)]; exists {
			issues = append(issues, Issue{
				Type: "morphological_redundancy",
				Description: fmt.Sprintf("Related term '%s' in entry '%s' is a morphological variation - "+
					"remove it as checker handles this automatically", relatedTerm, entry.Term),
				Term:     entry.Term,
				Location: location,
			})
		}

		// Check if related term should be a base term
		issues = append(issues, v.checkRelatedTermBaseForms(relatedTerm, entry.Term, location, primaryTerms)...)
	}

	return issues
}

// checkRelatedTermBaseForms checks if related terms might be morphological variations that should be base terms.
func (v *MorphologyValidator) checkRelatedTermBaseForms(
	relatedTerm, primaryTerm string, location int, primaryTerms map[string]int,
) []Issue {
	var issues []Issue

	term := strings.ToLower(relatedTerm)

	// Check for morphological suffixes
	suffixes := []struct {
		suffix    string
		minLength int
	}{
		{"s", minSuffixLength},
		{"ed", minEdSuffixLength},
	}

	for _, s := range suffixes {
		if base, exists := checkMorphologicalSuffix(term, s.suffix, s.minLength, primaryTerms); exists {
			issues = append(issues, Issue{
				Type: "morphological_redundancy",
				Description: fmt.Sprintf("Related term '%s' in entry '%s' is a morphological variation of existing term '%s' - "+
					"remove it as checker handles this automatically", relatedTerm, primaryTerm, base),
				Term:     primaryTerm,
				Location: location,
			})
		}
	}

	return issues
}

// checkTermQuality validates basic term quality and morphological generation.
func (v *MorphologyValidator) checkTermQuality(entry config.WordlistEntry, location int) []Issue {
	var issues []Issue

	// Check primary term length bounds
	if len(entry.Term) > maxTermLength {
		issues = append(issues, Issue{
			Type: "term_too_long",
			Description: fmt.Sprintf("Primary term '%s' is too long (%d characters) - may cause issues with morphological generation",
				entry.Term, len(entry.Term)),
			Term:     entry.Term,
			Location: location,
		})
	}

	if len(entry.Term) < minTermLength {
		issues = append(issues, Issue{
			Type: "term_too_short",
			Description: fmt.Sprintf("Primary term '%s' is too short (%d characters) - may not generate meaningful variations",
				entry.Term, len(entry.Term)),
			Term:     entry.Term,
			Location: location,
		})
	}

	// Check related terms for similar issues
	for _, relatedTerm := range entry.RelatedTerms {
		if len(relatedTerm) > maxTermLength {
			issues = append(issues, Issue{
				Type: "related_term_too_long",
				Description: fmt.Sprintf("Related term '%s' in entry '%s' is too long (%d characters)",
					relatedTerm, entry.Term, len(relatedTerm)),
				Term:     entry.Term,
				Location: location,
			})
		}

		if len(relatedTerm) < minTermLength {
			issues = append(issues, Issue{
				Type: "related_term_too_short",
				Description: fmt.Sprintf("Related term '%s' in entry '%s' is too short (%d characters)",
					relatedTerm, entry.Term, len(relatedTerm)),
				Term:     entry.Term,
				Location: location,
			})
		}
	}

	return issues
}

// checkMorphologicalSuffix checks if term ends with suffix and returns base form if it exists in primaryTerms.
func checkMorphologicalSuffix(term, suffix string, minLength int, primaryTerms map[string]int) (string, bool) {
	if strings.HasSuffix(term, suffix) && len(term) > minLength {
		base := term[:len(term)-len(suffix)]
		_, exists := primaryTerms[base]

		return base, exists
	}

	return "", false
}
