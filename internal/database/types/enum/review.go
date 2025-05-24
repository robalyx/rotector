package enum

// ReviewSortBy represents different ways to sort reviews.
//
//go:generate go tool enumer -type=ReviewSortBy -trimprefix=ReviewSortBy
type ReviewSortBy int

const (
	// ReviewSortByRandom orders reviews by random.
	ReviewSortByRandom ReviewSortBy = iota
	// ReviewSortByConfidence orders reviews by confidence.
	ReviewSortByConfidence
	// ReviewSortByLastUpdated orders reviews by last updated.
	ReviewSortByLastUpdated
	// ReviewSortByRecentlyUpdated orders reviews by most recently updated.
	ReviewSortByRecentlyUpdated
	// ReviewSortByReputation orders reviews by reputation.
	ReviewSortByReputation
	// ReviewSortByLastViewed orders reviews by last viewed.
	ReviewSortByLastViewed
)

// ReviewMode represents different modes of reviewing items.
//
//go:generate go tool enumer -type=ReviewMode -trimprefix=ReviewMode
type ReviewMode int

const (
	// ReviewModeTraining is for practicing reviews without affecting the system.
	ReviewModeTraining ReviewMode = iota
	// ReviewModeStandard is for normal review operations.
	ReviewModeStandard
)

// ReviewTargetMode represents what type of items to review.
//
//go:generate go tool enumer -type=ReviewTargetMode -trimprefix=ReviewTargetMode
type ReviewTargetMode int

const (
	// ReviewTargetModeFlagged indicates reviewing newly flagged items.
	ReviewTargetModeFlagged ReviewTargetMode = iota
	// ReviewTargetModeConfirmed indicates re-reviewing previously confirmed items.
	ReviewTargetModeConfirmed
	// ReviewTargetModeCleared indicates re-reviewing previously cleared items.
	ReviewTargetModeCleared
)
