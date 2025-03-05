package types

import (
	"fmt"

	"github.com/robalyx/rotector/internal/common/storage/database/types/enum"
)

// Reason represents a structured reason for flagging a user or group.
type Reason struct {
	Message    string   `json:"message"`    // The actual reason message
	Confidence float64  `json:"confidence"` // Confidence score for this specific reason
	Evidence   []string `json:"evidence"`   // Any evidence (like flagged content) specific to this reason
}

// Reasons maps reason types to their corresponding reason details.
type Reasons[T interface {
	enum.UserReasonType | enum.GroupReasonType
	fmt.Stringer
}] map[T]*Reason

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
