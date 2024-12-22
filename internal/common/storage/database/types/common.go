package types

import "errors"

// AppealFilterBy represents different ways to filter appeals.
type AppealFilterBy string

const (
	// AppealFilterByPending shows only pending appeals.
	AppealFilterByPending AppealFilterBy = "pending"
	// AppealFilterByAccepted shows only accepted appeals.
	AppealFilterByAccepted AppealFilterBy = "accepted"
	// AppealFilterByRejected shows only rejected appeals.
	AppealFilterByRejected AppealFilterBy = "rejected"
	// AppealFilterByAll shows all appeals.
	AppealFilterByAll AppealFilterBy = "all"
)

// AppealSortBy represents different ways to sort appeals.
type AppealSortBy string

const (
	// AppealSortByNewest orders appeals by submission time, newest first.
	AppealSortByNewest AppealSortBy = "newest"
	// AppealSortByOldest orders appeals by submission time, oldest first.
	AppealSortByOldest AppealSortBy = "oldest"
	// AppealSortByClaimed orders appeals by claimed status and last activity.
	AppealSortByClaimed AppealSortBy = "claimed"
)

// FormatDisplay returns a user-friendly display string for the AppealSortBy.
func (s AppealSortBy) FormatDisplay() string {
	switch s {
	case AppealSortByNewest:
		return "Newest First"
	case AppealSortByOldest:
		return "Oldest First"
	case AppealSortByClaimed:
		return "My Claims"
	default:
		return "Unknown Sort"
	}
}

// Rename existing SortBy to ReviewSortBy.
type ReviewSortBy string

const (
	// ReviewSortByRandom orders reviews by random.
	ReviewSortByRandom ReviewSortBy = "random"
	// ReviewSortByConfidence orders reviews by confidence.
	ReviewSortByConfidence ReviewSortBy = "confidence"
	// ReviewSortByLastUpdated orders reviews by last updated.
	ReviewSortByLastUpdated ReviewSortBy = "last_updated"
	// ReviewSortByReputation orders reviews by reputation.
	ReviewSortByReputation ReviewSortBy = "reputation"
)

// FormatDisplay returns a user-friendly display string for the ReviewSortBy.
func (s ReviewSortBy) FormatDisplay() string {
	switch s {
	case ReviewSortByRandom:
		return "Random"
	case ReviewSortByConfidence:
		return "Confidence"
	case ReviewSortByLastUpdated:
		return "Last Updated"
	case ReviewSortByReputation:
		return "Bad Reputation"
	default:
		return "Unknown Sort"
	}
}

// ChatModel represents different chat models.
type ChatModel string

const (
	ChatModelGeminiPro     ChatModel = "gemini-1.5-pro-latest"
	ChatModelGeminiFlash   ChatModel = "gemini-1.5-flash-latest"
	ChatModelGeminiFlash8B ChatModel = "gemini-1.5-flash-8b-latest"
)

// FormatDisplay returns a user-friendly display string for the ChatModel.
func (c ChatModel) FormatDisplay() string {
	switch c {
	case ChatModelGeminiPro:
		return "Gemini Pro"
	case ChatModelGeminiFlash:
		return "Gemini Flash"
	case ChatModelGeminiFlash8B:
		return "Gemini Flash 8B"
	default:
		return "Unknown Model"
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

// ReviewTargetMode represents what type of items to review.
type ReviewTargetMode string

const (
	// FlaggedReviewTarget indicates reviewing newly flagged items.
	FlaggedReviewTarget ReviewTargetMode = "flagged"
	// ConfirmedReviewTarget indicates re-reviewing previously confirmed items.
	ConfirmedReviewTarget ReviewTargetMode = "confirmed"
	// ClearedReviewTarget indicates re-reviewing previously cleared items.
	ClearedReviewTarget ReviewTargetMode = "cleared"
	// BannedReviewTarget indicates re-reviewing banned/locked items.
	BannedReviewTarget ReviewTargetMode = "banned"
)

// FormatDisplay returns a human-readable string for the review target mode.
func (m ReviewTargetMode) FormatDisplay() string {
	switch m {
	case FlaggedReviewTarget:
		return "Flagged Items"
	case ConfirmedReviewTarget:
		return "Confirmed Items"
	case ClearedReviewTarget:
		return "Cleared Items"
	case BannedReviewTarget:
		return "Banned Items"
	default:
		return "Unknown"
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
