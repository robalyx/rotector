package types

import (
	"strings"
	"testing"

	"github.com/robalyx/rotector/internal/database/types/enum"
)

func TestAddWithSource_NewReason(t *testing.T) {
	reasons := make(Reasons[enum.UserReasonType])

	reason := &Reason{
		Message:    "Inappropriate profile content detected",
		Confidence: 0.9,
		Evidence:   []string{"evidence1"},
	}

	reasons.AddWithSource(enum.UserReasonTypeProfile, reason, "ProfileChecker")

	result := reasons[enum.UserReasonTypeProfile]
	expected := "[ProfileChecker] Inappropriate profile content detected"

	if result.Message != expected {
		t.Errorf("Expected message %q, got %q", expected, result.Message)
	}

	if result.Confidence != 0.9 {
		t.Errorf("Expected confidence 0.9, got %f", result.Confidence)
	}
}

func TestAddWithSource_UpdateExistingSource(t *testing.T) {
	reasons := make(Reasons[enum.UserReasonType])

	// Add initial reason
	reason1 := &Reason{
		Message:    "First detection",
		Confidence: 0.7,
		Evidence:   []string{"evidence1"},
	}
	reasons.AddWithSource(enum.UserReasonTypeProfile, reason1, "ProfileChecker")

	// Update with same source
	reason2 := &Reason{
		Message:    "Updated detection with more details",
		Confidence: 0.95,
		Evidence:   []string{"evidence2"},
	}
	reasons.AddWithSource(enum.UserReasonTypeProfile, reason2, "ProfileChecker")

	result := reasons[enum.UserReasonTypeProfile]
	expected := "[ProfileChecker] Updated detection with more details"

	if result.Message != expected {
		t.Errorf("Expected message %q, got %q", expected, result.Message)
	}

	// Confidence should be max
	if result.Confidence != 0.95 {
		t.Errorf("Expected confidence 0.95, got %f", result.Confidence)
	}

	// Evidence should be merged
	if len(result.Evidence) != 2 {
		t.Errorf("Expected 2 evidence items, got %d", len(result.Evidence))
	}
}

func TestAddWithSource_MultipleSources(t *testing.T) {
	reasons := make(Reasons[enum.UserReasonType])

	// Add from ProfileChecker
	reason1 := &Reason{
		Message:    "Inappropriate username",
		Confidence: 0.8,
		Evidence:   []string{"username"},
	}
	reasons.AddWithSource(enum.UserReasonTypeProfile, reason1, "ProfileChecker")

	// Add from AIAnalyzer
	reason2 := &Reason{
		Message:    "Suspicious display name",
		Confidence: 0.9,
		Evidence:   []string{"displayname"},
	}
	reasons.AddWithSource(enum.UserReasonTypeProfile, reason2, "AIAnalyzer")

	// Add from CondoChecker
	reason3 := &Reason{
		Message:    "Description contains coded language",
		Confidence: 0.85,
		Evidence:   []string{"description"},
	}
	reasons.AddWithSource(enum.UserReasonTypeProfile, reason3, "CondoChecker")

	result := reasons[enum.UserReasonTypeProfile]

	// Should have all three sources
	if !strings.Contains(result.Message, "[ProfileChecker] Inappropriate username") {
		t.Error("Missing ProfileChecker message")
	}
	if !strings.Contains(result.Message, "[AIAnalyzer] Suspicious display name") {
		t.Error("Missing AIAnalyzer message")
	}
	if !strings.Contains(result.Message, "[CondoChecker] Description contains coded language") {
		t.Error("Missing CondoChecker message")
	}

	// Confidence should be max
	if result.Confidence != 0.9 {
		t.Errorf("Expected confidence 0.9, got %f", result.Confidence)
	}

	// Evidence should have all 3 items
	if len(result.Evidence) != 3 {
		t.Errorf("Expected 3 evidence items, got %d", len(result.Evidence))
	}
}

func TestAddWithSource_UpdateMiddleSource(t *testing.T) {
	reasons := make(Reasons[enum.UserReasonType])

	// Add three sources
	reasons.AddWithSource(enum.UserReasonTypeProfile, &Reason{Message: "First", Confidence: 0.7}, "Source1")
	reasons.AddWithSource(enum.UserReasonTypeProfile, &Reason{Message: "Second", Confidence: 0.8}, "Source2")
	reasons.AddWithSource(enum.UserReasonTypeProfile, &Reason{Message: "Third", Confidence: 0.9}, "Source3")

	// Update middle source
	reasons.AddWithSource(enum.UserReasonTypeProfile, &Reason{Message: "Updated Second", Confidence: 0.95}, "Source2")

	result := reasons[enum.UserReasonTypeProfile]

	lines := strings.Split(result.Message, "\n")
	if len(lines) != 3 {
		t.Errorf("Expected 3 lines, got %d", len(lines))
	}

	// Check order preserved and middle updated
	if !strings.Contains(lines[0], "[Source1] First") {
		t.Errorf("First line incorrect: %q", lines[0])
	}
	if !strings.Contains(lines[1], "[Source2] Updated Second") {
		t.Errorf("Second line incorrect: %q", lines[1])
	}
	if !strings.Contains(lines[2], "[Source3] Third") {
		t.Errorf("Third line incorrect: %q", lines[2])
	}
}

func TestAddWithSource_MalformedBrackets(t *testing.T) {
	reasons := make(Reasons[enum.UserReasonType])

	// Create a reason with malformed bracket in existing message
	reasons[enum.UserReasonTypeProfile] = &Reason{
		Message:    "[ValidSource] Valid message\n[BrokenSource malformed line without closing bracket\n[AnotherValid] Another valid line",
		Confidence: 0.8,
	}

	// Add new source
	reason := &Reason{
		Message:    "New detection",
		Confidence: 0.9,
	}
	reasons.AddWithSource(enum.UserReasonTypeProfile, reason, "NewSource")

	result := reasons[enum.UserReasonTypeProfile]

	// Malformed line should be preserved as [Legacy]
	if !strings.Contains(result.Message, "[Legacy] [BrokenSource malformed line without closing bracket") {
		t.Errorf("Malformed line not preserved as legacy. Got: %q", result.Message)
	}

	// Valid lines should still be there
	if !strings.Contains(result.Message, "[ValidSource] Valid message") {
		t.Error("ValidSource message lost")
	}
	if !strings.Contains(result.Message, "[AnotherValid] Another valid line") {
		t.Error("AnotherValid message lost")
	}
	if !strings.Contains(result.Message, "[NewSource] New detection") {
		t.Error("NewSource message not added")
	}
}

func TestAddWithSource_MultilineMessage(t *testing.T) {
	reasons := make(Reasons[enum.UserReasonType])

	// Add reason with newlines in message
	reason := &Reason{
		Message:    "Line 1\nLine 2\nLine 3",
		Confidence: 0.8,
	}
	reasons.AddWithSource(enum.UserReasonTypeProfile, reason, "MultilineSource")

	result := reasons[enum.UserReasonTypeProfile]
	expected := "[MultilineSource] Line 1 Line 2 Line 3"

	if result.Message != expected {
		t.Errorf("Expected normalized message %q, got %q", expected, result.Message)
	}

	// Verify no newlines in stored message
	if strings.Contains(result.Message, "\n") {
		t.Error("Message should not contain newlines after normalization")
	}
}

func TestAddWithSource_MultilineMessageWithCarriageReturns(t *testing.T) {
	reasons := make(Reasons[enum.UserReasonType])

	// Add reason with various newline formats
	reason := &Reason{
		Message:    "Line 1\r\nLine 2\rLine 3\nLine 4",
		Confidence: 0.8,
	}
	reasons.AddWithSource(enum.UserReasonTypeProfile, reason, "MixedNewlines")

	result := reasons[enum.UserReasonTypeProfile]
	expected := "[MixedNewlines] Line 1 Line 2 Line 3 Line 4"

	if result.Message != expected {
		t.Errorf("Expected normalized message %q, got %q", expected, result.Message)
	}
}

func TestAddWithSource_MultilineMessageUpdate(t *testing.T) {
	reasons := make(Reasons[enum.UserReasonType])

	// Add initial single-line reason
	reasons.AddWithSource(enum.UserReasonTypeProfile, &Reason{Message: "Initial", Confidence: 0.7}, "Source1")
	reasons.AddWithSource(enum.UserReasonTypeProfile, &Reason{Message: "Second", Confidence: 0.8}, "Source2")

	// Update Source1 with multi-line message
	multilineReason := &Reason{
		Message:    "Updated\nwith\nmultiple\nlines",
		Confidence: 0.9,
	}
	reasons.AddWithSource(enum.UserReasonTypeProfile, multilineReason, "Source1")

	result := reasons[enum.UserReasonTypeProfile]

	// Should only have 2 lines total (not split by newlines in message)
	lines := strings.Split(result.Message, "\n")
	if len(lines) != 2 {
		t.Errorf("Expected 2 lines (2 sources), got %d: %q", len(lines), result.Message)
	}

	// Source1 message should be normalized
	if !strings.Contains(result.Message, "[Source1] Updated with multiple lines") {
		t.Errorf("Source1 not properly normalized. Got: %q", result.Message)
	}
}

func TestAddWithSource_LegacyUnprefixedLines(t *testing.T) {
	reasons := make(Reasons[enum.UserReasonType])

	// Simulate existing data with unprefixed lines
	reasons[enum.UserReasonTypeProfile] = &Reason{
		Message:    "[NewSource] Prefixed line\nOld unprefixed line from legacy data\nAnother unprefixed line",
		Confidence: 0.8,
	}

	// Add new source
	reason := &Reason{
		Message:    "New detection",
		Confidence: 0.9,
	}
	reasons.AddWithSource(enum.UserReasonTypeProfile, reason, "AnotherSource")

	result := reasons[enum.UserReasonTypeProfile]

	// Legacy lines should be preserved with [Legacy] prefix
	if !strings.Contains(result.Message, "[Legacy] Old unprefixed line from legacy data") {
		t.Errorf("First legacy line not preserved. Got: %q", result.Message)
	}
	if !strings.Contains(result.Message, "[Legacy] Another unprefixed line") {
		t.Errorf("Second legacy line not preserved. Got: %q", result.Message)
	}

	// New source should be added
	if !strings.Contains(result.Message, "[AnotherSource] New detection") {
		t.Error("New source not added")
	}

	// Original prefixed line should still exist
	if !strings.Contains(result.Message, "[NewSource] Prefixed line") {
		t.Error("Original prefixed line lost")
	}
}

func TestAddWithSource_EmptyLines(t *testing.T) {
	reasons := make(Reasons[enum.UserReasonType])

	// Create reason with empty lines
	reasons[enum.UserReasonTypeProfile] = &Reason{
		Message:    "[Source1] First\n\n\n[Source2] Second\n  \n[Source3] Third",
		Confidence: 0.8,
	}

	// Add new source
	reasons.AddWithSource(enum.UserReasonTypeProfile, &Reason{Message: "Fourth", Confidence: 0.9}, "Source4")

	result := reasons[enum.UserReasonTypeProfile]

	// Should only have 4 lines (empty lines ignored)
	lines := strings.Split(result.Message, "\n")
	if len(lines) != 4 {
		t.Errorf("Expected 4 lines (empty lines should be skipped), got %d: %q", len(lines), result.Message)
	}
}

func TestAddWithSource_EvidenceMerging(t *testing.T) {
	reasons := make(Reasons[enum.UserReasonType])

	// Add first source with evidence
	reason1 := &Reason{
		Message:    "First detection",
		Confidence: 0.8,
		Evidence:   []string{"evidence1", "evidence2"},
	}
	reasons.AddWithSource(enum.UserReasonTypeProfile, reason1, "Source1")

	// Add second source with overlapping evidence
	reason2 := &Reason{
		Message:    "Second detection",
		Confidence: 0.9,
		Evidence:   []string{"evidence2", "evidence3"},
	}
	reasons.AddWithSource(enum.UserReasonTypeProfile, reason2, "Source2")

	result := reasons[enum.UserReasonTypeProfile]

	// Should have 3 unique evidence items
	if len(result.Evidence) != 3 {
		t.Errorf("Expected 3 unique evidence items, got %d", len(result.Evidence))
	}

	// Verify all evidence present (order may vary due to map)
	evidenceMap := make(map[string]bool)
	for _, e := range result.Evidence {
		evidenceMap[e] = true
	}

	if !evidenceMap["evidence1"] || !evidenceMap["evidence2"] || !evidenceMap["evidence3"] {
		t.Errorf("Missing evidence items. Got: %v", result.Evidence)
	}
}

func TestAddWithSource_ConfidenceMaximum(t *testing.T) {
	reasons := make(Reasons[enum.UserReasonType])

	// Add with confidence 0.9
	reasons.AddWithSource(enum.UserReasonTypeProfile, &Reason{Message: "First", Confidence: 0.9}, "Source1")

	// Add with lower confidence 0.7
	reasons.AddWithSource(enum.UserReasonTypeProfile, &Reason{Message: "Second", Confidence: 0.7}, "Source2")

	result := reasons[enum.UserReasonTypeProfile]

	// Should keep maximum confidence
	if result.Confidence != 0.9 {
		t.Errorf("Expected confidence 0.9 (maximum), got %f", result.Confidence)
	}

	// Add with higher confidence 0.95
	reasons.AddWithSource(enum.UserReasonTypeProfile, &Reason{Message: "Third", Confidence: 0.95}, "Source3")

	result = reasons[enum.UserReasonTypeProfile]

	// Should update to new maximum
	if result.Confidence != 0.95 {
		t.Errorf("Expected confidence 0.95 (new maximum), got %f", result.Confidence)
	}
}

func TestNormalizeMessage(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Single line unchanged",
			input:    "Single line message",
			expected: "Single line message",
		},
		{
			name:     "Newlines converted to spaces",
			input:    "Line 1\nLine 2\nLine 3",
			expected: "Line 1 Line 2 Line 3",
		},
		{
			name:     "Carriage returns converted",
			input:    "Line 1\rLine 2\rLine 3",
			expected: "Line 1 Line 2 Line 3",
		},
		{
			name:     "Mixed newline formats",
			input:    "Line 1\r\nLine 2\nLine 3\rLine 4",
			expected: "Line 1 Line 2 Line 3 Line 4",
		},
		{
			name:     "Multiple spaces collapsed",
			input:    "Too    many     spaces",
			expected: "Too many spaces",
		},
		{
			name:     "Leading and trailing spaces trimmed",
			input:    "  Trimmed  ",
			expected: "Trimmed",
		},
		{
			name:     "Complex whitespace",
			input:    "  Line 1\n\n  Line 2  \r\n  Line 3  ",
			expected: "Line 1 Line 2 Line 3",
		},
		{
			name:     "Empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "Only whitespace",
			input:    "  \n\r\n  \t  ",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizeMessage(tt.input)
			if result != tt.expected {
				t.Errorf("normalizeMessage() = %q, want %q", result, tt.expected)
			}
		})
	}
}
