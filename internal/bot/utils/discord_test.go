package utils

import (
	"testing"

	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/stretchr/testify/assert"
)

func TestGetMessageEmbedColor(t *testing.T) {
	tests := []struct {
		name         string
		streamerMode bool
		want         int
	}{
		{
			name:         "streamer mode enabled",
			streamerMode: true,
			want:         constants.StreamerModeEmbedColor,
		},
		{
			name:         "streamer mode disabled",
			streamerMode: false,
			want:         constants.DefaultEmbedColor,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetMessageEmbedColor(tt.streamerMode)
			assert.Equal(t, tt.want, got)
		})
	}
}
