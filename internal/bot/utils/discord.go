package utils

import (
	"github.com/robalyx/rotector/internal/bot/constants"
)

// GetMessageEmbedColor returns the appropriate embed color based on streamer mode.
// This helps visually distinguish when streamer mode is active.
func GetMessageEmbedColor(streamerMode bool) int {
	if streamerMode {
		return constants.StreamerModeEmbedColor
	}
	return constants.DefaultEmbedColor
}
