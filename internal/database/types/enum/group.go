package enum

// GroupType represents the different states a group can be in.
//
//go:generate go tool enumer -type=GroupType -trimprefix=GroupType
type GroupType int

const (
	// GroupTypeMixed indicates a group has inappropriate content but also many innocent users.
	GroupTypeMixed GroupType = iota
	// GroupTypeFlagged indicates a group needs review for potential violations.
	GroupTypeFlagged
	// GroupTypeConfirmed indicates a group has been reviewed and confirmed as inappropriate.
	GroupTypeConfirmed
)
