package wordlist

import (
	"github.com/robalyx/rotector/internal/setup/config"
)

// Issue represents a validation issue found in the wordlist.
type Issue struct {
	Type        string
	Description string
	Term        string
	Location    int
}

// Validator defines the interface for all wordlist validators.
type Validator interface {
	Validate(wordlist *config.Wordlist) []Issue
}
