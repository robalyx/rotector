package utils

import (
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/common/queue"
)

// GetMessageEmbedColor returns the appropriate embed color based on streamer mode.
// This helps visually distinguish when streamer mode is active.
func GetMessageEmbedColor(streamerMode bool) int {
	if streamerMode {
		return constants.StreamerModeEmbedColor
	}
	return constants.DefaultEmbedColor
}

// GetPriorityFromCustomID maps Discord component custom IDs to queue priority levels.
// Returns NormalPriority if the custom ID is not recognized.
func GetPriorityFromCustomID(customID string) string {
	switch customID {
	case constants.QueueHighPriorityCustomID:
		return queue.HighPriority
	case constants.QueueNormalPriorityCustomID:
		return queue.NormalPriority
	case constants.QueueLowPriorityCustomID:
		return queue.LowPriority
	default:
		return queue.NormalPriority
	}
}
