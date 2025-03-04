package utils_test

import (
	"testing"

	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/utils"
	"github.com/stretchr/testify/assert"
)

func TestGetMessageEmbedColor(t *testing.T) {
	t.Parallel()
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
			t.Parallel()
			got := utils.GetMessageEmbedColor(tt.streamerMode)
			assert.Equal(t, tt.want, got)
		})
	}
}
