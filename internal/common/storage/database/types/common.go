package types

import "errors"

// SortBy represents different ways to sort items in the system.
type SortBy string

const (
	// SortByRandom orders items randomly to ensure even distribution of reviews.
	SortByRandom SortBy = "random"
	// SortByConfidence orders items by their confidence score from highest to lowest.
	SortByConfidence SortBy = "confidence"
	// SortByLastUpdated orders items by their last update time from oldest to newest.
	SortByLastUpdated SortBy = "last_updated"
	// SortByReputation orders items by their community reputation (upvotes - downvotes).
	SortByReputation SortBy = "reputation"
	// SortByFlaggedUsers orders groups by their number of flagged members.
	SortByFlaggedUsers SortBy = "flagged_users"
)

// FormatDisplay returns a user-friendly display string for the SortBy.
func (s SortBy) FormatDisplay() string {
	switch s {
	case SortByRandom:
		return "Random"
	case SortByConfidence:
		return "Confidence"
	case SortByLastUpdated:
		return "Last Updated"
	case SortByReputation:
		return "Bad Reputation"
	case SortByFlaggedUsers:
		return "Flagged Users"
	default:
		return "Unknown Sort"
	}
}

// ReviewMode represents different modes of reviewing items.
type ReviewMode string

const (
	// TrainingReviewMode is for practicing reviews without affecting the system.
	TrainingReviewMode ReviewMode = "training"
	// StandardReviewMode is for normal review operations.
	StandardReviewMode ReviewMode = "standard"
)

// FormatDisplay returns a user-friendly display string for the ReviewMode.
func (r ReviewMode) FormatDisplay() string {
	switch r {
	case TrainingReviewMode:
		return "Training Mode"
	case StandardReviewMode:
		return "Standard Mode"
	default:
		return "Unknown Mode"
	}
}

// ReviewTargetMode represents different types of items to review.
type ReviewTargetMode string

const (
	// FlaggedReviewTarget is for reviewing newly flagged items.
	FlaggedReviewTarget ReviewTargetMode = "flagged"
	// ConfirmedReviewTarget is for re-reviewing previously confirmed items.
	ConfirmedReviewTarget ReviewTargetMode = "confirmed"
)

// FormatDisplay returns a user-friendly display string for the ReviewTargetMode.
func (r ReviewTargetMode) FormatDisplay() string {
	switch r {
	case FlaggedReviewTarget:
		return "Flagged Items"
	case ConfirmedReviewTarget:
		return "Confirmed Items"
	default:
		return "Unknown Target"
	}
}

// Common errors for database operations.
var (
	// ErrInvalidSortBy is returned when an invalid sort method is provided.
	ErrInvalidSortBy = errors.New("invalid sort by value")
	// ErrUnsupportedModel is returned when an operation is attempted on an unsupported model type.
	ErrUnsupportedModel = errors.New("unsupported model type")
	// ErrNoGroupsToReview is returned when there are no groups available for review.
	ErrNoGroupsToReview = errors.New("no groups available to review")
	// ErrNoUsersToReview is returned when there are no users available for review.
	ErrNoUsersToReview = errors.New("no users available to review")
	// ErrInvalidIDFormat is returned when a provided ID is not in the correct format.
	ErrInvalidIDFormat = errors.New("invalid Discord ID format")
	// ErrSelfAssignment is returned when a user tries to add/remove themselves.
	ErrSelfAssignment = errors.New("you cannot add/remove yourself")
	// ErrInvalidOption is returned when an invalid option is selected.
	ErrInvalidOption = errors.New("invalid option selected")
	// ErrInvalidBoolValue is returned when a value that should be boolean is not.
	ErrInvalidBoolValue = errors.New("value must be true or false")
	// ErrWelcomeMessageTooLong is returned when the welcome message exceeds the character limit.
	ErrWelcomeMessageTooLong = errors.New("welcome message cannot exceed 512 characters")
	// ErrNoLogsFound is returned when no activity logs match the search criteria.
	ErrNoLogsFound = errors.New("no logs found")
)
