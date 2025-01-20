package enum

// GroupType represents the different states a group can be in.
//
//go:generate enumer -type=GroupType -trimprefix=GroupType
type GroupType int

const (
	// GroupTypeConfirmed indicates a group has been reviewed and confirmed as inappropriate.
	GroupTypeConfirmed GroupType = iota
	// GroupTypeFlagged indicates a group needs review for potential violations.
	GroupTypeFlagged
	// GroupTypeCleared indicates a group was reviewed and found to be appropriate.
	GroupTypeCleared
	// GroupTypeUnflagged indicates a group was not found in the database.
	GroupTypeUnflagged
)
