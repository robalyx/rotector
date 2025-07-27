package session

import (
	"slices"

	"github.com/robalyx/rotector/internal/bot/constants"
)

// ReviewHistoryType represents the type of review history being managed.
type ReviewHistoryType int

const (
	UserReviewHistoryType ReviewHistoryType = iota
	GroupReviewHistoryType
)

// AddToReviewHistory adds an ID to the specified review history and updates the index in the session.
// It trims the history if it exceeds the maximum size.
func AddToReviewHistory(s *Session, historyType ReviewHistoryType, id uint64) {
	var (
		historyKey Key[[]uint64]
		indexKey   Key[int]
	)

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

// RemoveFromReviewHistory removes an item at the specified index from the review history
// and adjusts the index accordingly.
func RemoveFromReviewHistory(s *Session, historyType ReviewHistoryType, index int) {
	var (
		historyKey Key[[]uint64]
		indexKey   Key[int]
	)

	switch historyType {
	case UserReviewHistoryType:
		historyKey = UserReviewHistory
		indexKey = UserReviewHistoryIndex
	case GroupReviewHistoryType:
		historyKey = GroupReviewHistory
		indexKey = GroupReviewHistoryIndex
	}

	history := historyKey.Get(s)
	currentIndex := indexKey.Get(s)

	// Remove the item at the specified index
	if index >= 0 && index < len(history) {
		history = slices.Delete(history, index, index+1)
		historyKey.Set(s, history)

		// Adjust the current index if needed
		if currentIndex >= len(history) && len(history) > 0 {
			currentIndex = len(history) - 1
		} else if len(history) == 0 {
			currentIndex = 0
		}

		indexKey.Set(s, currentIndex)
	}
}
