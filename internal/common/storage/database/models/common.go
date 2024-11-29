package models

import "errors"

const (
	// SortByRandom orders users randomly to ensure even distribution of reviews.
	SortByRandom = "random"
	// SortByConfidence orders users by their confidence score from highest to lowest.
	SortByConfidence = "confidence"
	// SortByLastUpdated orders users by their last update time from oldest to newest.
	SortByLastUpdated = "last_updated"
	// SortByReputation orders users by their community reputation (upvotes - downvotes).
	SortByReputation = "reputation"
	// SortByFlaggedUsers orders groups by their number of flagged members.
	SortByFlaggedUsers = "flagged_users"
)

// Common errors for user operations.
var (
	ErrInvalidSortBy    = errors.New("invalid sort by value")
	ErrUnsupportedModel = errors.New("unsupported model type")
	ErrNoGroupsToReview = errors.New("no groups available to review")
	ErrNoUsersToReview  = errors.New("no users available to review")
)
