package enum

// UserType represents the different states a user can be in.
//
//go:generate go tool enumer -type=UserType -trimprefix=UserType
type UserType int

const (
	// UserTypeConfirmed indicates a user has been reviewed and confirmed as inappropriate.
	UserTypeConfirmed UserType = iota
	// UserTypeFlagged indicates a user needs review for potential violations.
	UserTypeFlagged
	// UserTypeCleared indicates a user was reviewed and found to be appropriate.
	UserTypeCleared
)
