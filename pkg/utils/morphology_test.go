package utils_test

import (
	"regexp"
	"testing"

	"github.com/robalyx/rotector/pkg/utils"
)

// assertContainsBaseTerm verifies that variations include the base term.
func assertContainsBaseTerm(t *testing.T, variations []string, baseTerm string) {
	t.Helper()

	for _, variation := range variations {
		if variation == baseTerm {
			return
		}
	}

	t.Errorf("Base term '%s' not found in variations: %v", baseTerm, variations)
}

// assertContainsExpectedForms verifies that all expected forms are present.
func assertContainsExpectedForms(t *testing.T, result []string, expected []string, testName string) {
	t.Helper()

	for _, expectedForm := range expected {
		found := false

		for _, actual := range result {
			if actual == expectedForm {
				found = true
				break
			}
		}

		if !found {
			t.Errorf("%s: Expected form '%s' not found in result: %v", testName, expectedForm, result)
		}
	}
}

// assertValidFormQuality verifies that all forms meet quality standards.
func assertValidFormQuality(t *testing.T, forms []string, testContext string) {
	t.Helper()

	for _, form := range forms {
		if len(form) > 25 {
			t.Errorf("%s: Form '%s' exceeds maximum length of 25 characters", testContext, form)
		}

		if len(form) < 2 {
			t.Errorf("%s: Form '%s' is too short (minimum 2 characters)", testContext, form)
		}

		// Check that form contains at least one letter
		letterRegex := regexp.MustCompile(`[a-zA-Z]`)
		if !letterRegex.MatchString(form) {
			t.Errorf("%s: Form '%s' contains no letters", testContext, form)
		}
	}
}

func TestGenerateMorphologicalVariations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		term     string
		expected []string
	}{
		{
			name:     "basic term",
			term:     "trade",
			expected: []string{"trade", "trades", "traded"},
		},
		{
			name:     "single character term",
			term:     "a",
			expected: []string{"a"},
		},
		{
			name:     "empty term",
			term:     "",
			expected: []string{""},
		},
		{
			name:     "short term",
			term:     "go",
			expected: []string{"go", "gos", "goed"},
		},
		{
			name:     "term ending with s",
			term:     "class",
			expected: []string{"class", "classs", "classed"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := utils.GenerateMorphologicalVariations(tt.term)

			// Check that base term is always included
			if len(tt.term) >= 2 {
				assertContainsBaseTerm(t, result, tt.term)
			}

			// Check expected forms are present
			assertContainsExpectedForms(t, result, tt.expected, tt.name)

			// Validate form quality for non-trivial cases
			if len(tt.term) >= 2 {
				assertValidFormQuality(t, result, tt.name)
			}

			// Should not contain duplicates
			seen := make(map[string]bool)
			for _, variation := range result {
				if seen[variation] {
					t.Errorf("Duplicate variation '%s' found in result: %v", variation, result)
				}

				seen[variation] = true
			}
		})
	}
}

func TestRemoveDuplicates(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name:     "no duplicates",
			input:    []string{"a", "b", "c"},
			expected: []string{"a", "b", "c"},
		},
		{
			name:     "with duplicates",
			input:    []string{"a", "b", "a", "c", "b"},
			expected: []string{"a", "b", "c"},
		},
		{
			name:     "empty slice",
			input:    []string{},
			expected: []string{},
		},
		{
			name:     "all same",
			input:    []string{"a", "a", "a"},
			expected: []string{"a"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := utils.RemoveDuplicates(tt.input)

			if len(result) != len(tt.expected) {
				t.Errorf("Expected length %d, got %d. Result: %v, Expected: %v",
					len(tt.expected), len(result), result, tt.expected)

				return
			}

			// Check that all expected items are present
			resultMap := make(map[string]bool)
			for _, item := range result {
				resultMap[item] = true
			}

			for _, expected := range tt.expected {
				if !resultMap[expected] {
					t.Errorf("Expected item '%s' not found in result: %v", expected, result)
				}
			}
		})
	}
}

func TestMorphologyEdgeCases(t *testing.T) {
	t.Parallel()

	t.Run("very short terms", func(t *testing.T) {
		t.Parallel()

		shortTerms := []string{"", "a"}
		for _, term := range shortTerms {
			result := utils.GenerateMorphologicalVariations(term)
			if len(result) != 1 || result[0] != term {
				t.Errorf("Short term '%s' should return only itself, got: %v", term, result)
			}
		}
	})

	t.Run("special characters", func(t *testing.T) {
		t.Parallel()

		specialTerms := []string{"test123", "user@domain"}
		for _, term := range specialTerms {
			result := utils.GenerateMorphologicalVariations(term)
			assertContainsBaseTerm(t, result, term)

			// Should generate expected simple variations
			expectedCount := 3 // base + "s" + "ed"
			if len(result) != expectedCount {
				t.Errorf("Expected %d variations for '%s', got %d: %v",
					expectedCount, term, len(result), result)
			}
		}
	})
}
