package enum

// GroupType represents the different states a group can be in.
//
//go:generate go tool enumer -type=GroupType -trimprefix=GroupType
type GroupType int

const (
	// GroupTypeCleared indicates a group was reviewed and found to be appropriate.
	GroupTypeCleared GroupType = iota
	// GroupTypeFlagged indicates a group needs review for potential violations.
	GroupTypeFlagged
	// GroupTypeConfirmed indicates a group has been reviewed and confirmed as inappropriate.
	GroupTypeConfirmed
)
