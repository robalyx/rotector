package types

import (
	"fmt"
	"strings"

	"github.com/robalyx/rotector/internal/database/types/enum"
)

// Reason represents a structured reason for flagging a user or group.
type Reason struct {
	Message    string   `json:"message"`    // The actual reason message
	Confidence float64  `json:"confidence"` // Confidence score for this specific reason
	Evidence   []string `json:"evidence"`   // Any evidence (like flagged content) specific to this reason
}

// ReasonType represents a type that can be used as a reason identifier.
type ReasonType interface {
	enum.UserReasonType | enum.GroupReasonType
	fmt.Stringer
}

// Reasons maps reason types to their corresponding reason details.
type Reasons[T ReasonType] map[T]*Reason

// Add adds or updates a reason in the reasons map.
// If the reason type already exists, it updates the existing entry.
func (r Reasons[T]) Add(reasonType T, reason *Reason) {
	r[reasonType] = reason
}

// AddWithSource adds or updates a reason with a source prefix.
// When the same reason type exists, it parses the message by [prefix] format,
// replaces the line with matching prefix, or appends if new.
// This allows multiple detectors to contribute to the same reason type without overwriting.
//
// Developer Note: We use prefix-based parsing instead of adding a separate Sources field
// to the Reason struct because:
// 1. Avoids complex database migration for existing reasons in production
// 2. Keeps the Reason struct simple and backward compatible
// 3. Message field already serves as both storage and display format
// 4. Parsing overhead is negligible compared to database/network operations
//
// The [prefix] format allows multiple detectors (e.g., CondoChecker and Discord Scanner)
// to flag the same reason type without overwriting each other's findings. When reprocessed,
// each detector updates only its own line while preserving other detectors' contributions.
func (r Reasons[T]) AddWithSource(reasonType T, reason *Reason, sourcePrefix string) {
	existing, exists := r[reasonType]

	if !exists {
		// First time so just add with prefix
		reason.Message = fmt.Sprintf("[%s] %s", sourcePrefix, normalizeMessage(reason.Message))
		r[reasonType] = reason

		return
	}

	// Parse existing lines preserving order
	type prefixedLine struct {
		prefix  string
		message string
	}

	lines := strings.Split(existing.Message, "\n")

	var prefixedLines []prefixedLine

	foundPrefix := false

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Extract prefix
		if strings.HasPrefix(line, "[") {
			endBracket := strings.Index(line, "]")
			if endBracket > 0 {
				prefix := line[1:endBracket]
				message := strings.TrimSpace(line[endBracket+1:])

				if prefix == sourcePrefix {
					// Replace this line
					prefixedLines = append(prefixedLines, prefixedLine{sourcePrefix, normalizeMessage(reason.Message)})
					foundPrefix = true
				} else {
					prefixedLines = append(prefixedLines, prefixedLine{prefix, message})
				}
			} else {
				// Malformed bracket - treat as legacy
				prefixedLines = append(prefixedLines, prefixedLine{"Legacy", line})
			}
		} else {
			// Handle unprefixed lines from old data
			prefixedLines = append(prefixedLines, prefixedLine{"Legacy", line})
		}
	}

	// If we didn't find this prefix, append it
	if !foundPrefix {
		prefixedLines = append(prefixedLines, prefixedLine{sourcePrefix, normalizeMessage(reason.Message)})
	}

	// Rebuild message
	newLines := make([]string, 0, len(prefixedLines))
	for _, pl := range prefixedLines {
		newLines = append(newLines, fmt.Sprintf("[%s] %s", pl.prefix, pl.message))
	}

	existing.Message = strings.Join(newLines, "\n")

	// Update confidence to max
	existing.Confidence = max(existing.Confidence, reason.Confidence)

	// Merge evidence and deduplicate
	evidenceSet := make(map[string]struct{})

	for _, e := range existing.Evidence {
		evidenceSet[e] = struct{}{}
	}

	for _, e := range reason.Evidence {
		evidenceSet[e] = struct{}{}
	}

	// Convert back to slice
	existing.Evidence = make([]string, 0, len(evidenceSet))
	for e := range evidenceSet {
		existing.Evidence = append(existing.Evidence, e)
	}
}

// Messages returns an array of all reason messages.
func (r Reasons[T]) Messages() []string {
	messages := make([]string, 0, len(r))
	for _, reason := range r {
		messages = append(messages, reason.Message)
	}

	return messages
}

// Types returns an array of all reason types.
func (r Reasons[T]) Types() []string {
	types := make([]string, 0, len(r))
	for reasonType := range r {
		types = append(types, reasonType.String())
	}

	return types
}

// ReasonInfos returns an array of ReasonInfo structs containing both type and message.
// This is used for AI analysis where both the type and detailed message are needed.
func (r Reasons[T]) ReasonInfos() []ReasonInfo {
	infos := make([]ReasonInfo, 0, len(r))
	for reasonType, reason := range r {
		infos = append(infos, ReasonInfo{
			Type:    reasonType.String(),
			Message: reason.Message,
		})
	}

	return infos
}

// ReasonInfo represents a reason with both type and message for AI analysis.
type ReasonInfo struct {
	Type    string `json:"type"`    // The type of reason (e.g., Profile, Friend, Group, etc.)
	Message string `json:"message"` // The detailed reason message explaining why this was flagged
}

// normalizeMessage converts a multi-line message to a single line.
// Replaces newlines and carriage returns with spaces, collapses repeated
// whitespace, and trims leading/trailing spaces.
func normalizeMessage(msg string) string {
	// Replace newlines and carriage returns with spaces
	msg = strings.ReplaceAll(msg, "\n", " ")
	msg = strings.ReplaceAll(msg, "\r", " ")

	// Collapse repeated whitespace and trim
	return strings.Join(strings.Fields(msg), " ")
}
