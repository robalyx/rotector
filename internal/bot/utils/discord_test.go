package utils_test

import (
	"testing"

	"github.com/disgoorg/disgo/discord"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/utils"
	"github.com/stretchr/testify/assert"
)

func TestGetMessageContainerColor(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		streamerMode bool
		want         int
	}{
		{
			name:         "streamer mode enabled",
			streamerMode: true,
			want:         constants.StreamerModeContainerColor,
		},
		{
			name:         "streamer mode disabled",
			streamerMode: false,
			want:         constants.DefaultContainerColor,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := utils.GetContainerColor(tt.streamerMode)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCreateTimestampedTextDisplay(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		content string
	}{
		{
			name:    "empty content",
			content: "",
		},
		{
			name:    "with content",
			content: "test message",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			component := utils.CreateTimestampedTextDisplay(tt.content)

			// Assert component type
			assert.Equal(t, discord.ComponentTypeTextDisplay, component.Type())

			// Assert component is a text display
			textDisplay, ok := component.(discord.TextDisplayComponent)
			assert.True(t, ok, "component should be a text display")

			// Check that content is timestamped
			expectedContent := utils.GetTimestampedSubtext(tt.content)
			assert.Equal(t, expectedContent, textDisplay.Content)
		})
	}
}
