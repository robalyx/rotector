package utils

import (
	"github.com/disgoorg/disgo/discord"
	"github.com/robalyx/rotector/internal/bot/constants"
)

// GetContainerColor returns the appropriate container color based on streamer mode.
// This helps visually distinguish when streamer mode is active.
func GetContainerColor(streamerMode bool) int {
	if streamerMode {
		return constants.StreamerModeContainerColor
	}
	return constants.DefaultContainerColor
}

// CreateTimestampedTextDisplay creates a container component with a timestamped text display at the top.
// This is used to display timestamped messages in Discord's Components V2 format.
func CreateTimestampedTextDisplay(content string) discord.LayoutComponent {
	return discord.NewContainer(
		discord.NewTextDisplay(GetTimestampedSubtext(content)),
	)
}
