package session

import "github.com/robalyx/rotector/internal/bot/constants"

// ReviewHistoryType represents the type of review history being managed.
type ReviewHistoryType int

const (
	UserReviewHistoryType ReviewHistoryType = iota
	GroupReviewHistoryType
)

// AddToReviewHistory adds an ID to the specified review history and updates the index in the session.
// It trims the history if it exceeds the maximum size.
func AddToReviewHistory(s *Session, historyType ReviewHistoryType, id uint64) {
	var historyKey Key[[]uint64]
	var indexKey Key[int]

	switch historyType {
	case UserReviewHistoryType:
		historyKey = UserReviewHistory
		indexKey = UserReviewHistoryIndex
	case GroupReviewHistoryType:
		historyKey = GroupReviewHistory
		indexKey = GroupReviewHistoryIndex
	}

	history := historyKey.Get(s)
	history = append(history, id)

	// Trim history if it exceeds the maximum size
	if len(history) > constants.MaxReviewHistorySize {
		history = history[len(history)-constants.MaxReviewHistorySize:]
	}

	historyKey.Set(s, history)
	indexKey.Set(s, len(history)-1)
}
