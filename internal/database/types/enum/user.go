package enum

// UserType represents the different states a user can be in.
//
//go:generate go tool enumer -type=UserType -trimprefix=UserType
type UserType int

const (
	// UserTypeCleared indicates a user was reviewed and found to be appropriate.
	UserTypeCleared UserType = iota
	// UserTypeFlagged indicates a user needs review for potential violations.
	UserTypeFlagged
	// UserTypeConfirmed indicates a user has been reviewed and confirmed as inappropriate.
	UserTypeConfirmed
	// UserTypeQueued indicates a user is queued for processing.
	UserTypeQueued
	// UserTypeBloxDB indicates a user is flagged from BloxDB data source.
	UserTypeBloxDB
	// UserTypeMixed indicates a user has mixed status or conflicting signals.
	UserTypeMixed
	// UserTypePastOffender indicates a user with previous violations.
	UserTypePastOffender
)
