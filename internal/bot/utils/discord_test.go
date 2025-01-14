package utils

import (
	"testing"

	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/common/queue"
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

func TestGetPriorityFromCustomID(t *testing.T) {
	tests := []struct {
		name     string
		customID string
		want     string
	}{
		{
			name:     "high priority",
			customID: constants.QueueHighPriorityCustomID,
			want:     queue.HighPriority,
		},
		{
			name:     "normal priority",
			customID: constants.QueueNormalPriorityCustomID,
			want:     queue.NormalPriority,
		},
		{
			name:     "low priority",
			customID: constants.QueueLowPriorityCustomID,
			want:     queue.LowPriority,
		},
		{
			name:     "unknown custom ID",
			customID: "unknown",
			want:     queue.NormalPriority,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetPriorityFromCustomID(tt.customID)
			assert.Equal(t, tt.want, got)
		})
	}
}
