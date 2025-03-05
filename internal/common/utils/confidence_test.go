package utils_test

import (
	"testing"

	"github.com/robalyx/rotector/internal/common/storage/database/types"
	"github.com/robalyx/rotector/internal/common/storage/database/types/enum"
	"github.com/robalyx/rotector/internal/common/utils"
	"github.com/stretchr/testify/assert"
)

func TestCalculateConfidence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		reasons types.Reasons[enum.UserReasonType]
		want    float64
	}{
		{
			name:    "empty reasons",
			reasons: types.Reasons[enum.UserReasonType]{},
			want:    0,
		},
		{
			name: "single reason",
			reasons: types.Reasons[enum.UserReasonType]{
				enum.UserReasonTypeDescription: {
					Message:    "test",
					Confidence: 0.8,
				},
			},
			want: 0.8,
		},
		{
			name: "multiple reasons same confidence",
			reasons: types.Reasons[enum.UserReasonType]{
				enum.UserReasonTypeDescription: {
					Message:    "test1",
					Confidence: 0.8,
				},
				enum.UserReasonTypeFriend: {
					Message:    "test2",
					Confidence: 0.8,
				},
			},
			want: 0.8,
		},
		{
			name: "multiple reasons different confidence",
			reasons: types.Reasons[enum.UserReasonType]{
				enum.UserReasonTypeDescription: {
					Message:    "test1",
					Confidence: 0.9,
				},
				enum.UserReasonTypeFriend: {
					Message:    "test2",
					Confidence: 0.6,
				},
			},
			want: 0.86,
		},
		{
			name: "three reasons different confidence",
			reasons: types.Reasons[enum.UserReasonType]{
				enum.UserReasonTypeDescription: {
					Message:    "test1",
					Confidence: 0.9,
				},
				enum.UserReasonTypeFriend: {
					Message:    "test2",
					Confidence: 0.6,
				},
				enum.UserReasonTypeOutfit: {
					Message:    "test3",
					Confidence: 0.3,
				},
			},
			want: 0.81,
		},
		{
			name: "confidence above 1.0",
			reasons: types.Reasons[enum.UserReasonType]{
				enum.UserReasonTypeDescription: {
					Message:    "test1",
					Confidence: 1.2,
				},
			},
			want: 1.0,
		},
		{
			name: "confidence below 0",
			reasons: types.Reasons[enum.UserReasonType]{
				enum.UserReasonTypeDescription: {
					Message:    "test1",
					Confidence: -0.2,
				},
			},
			want: 0.0,
		},
		{
			name: "mixed positive and negative confidence",
			reasons: types.Reasons[enum.UserReasonType]{
				enum.UserReasonTypeDescription: {
					Message:    "test1",
					Confidence: 0.8,
				},
				enum.UserReasonTypeFriend: {
					Message:    "test2",
					Confidence: -0.2,
				},
			},
			want: 0.65,
		},
	}

	for _, tt := range tests {
		// capture range variable
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := utils.CalculateConfidence(tt.reasons)
			assert.InEpsilon(t, tt.want, got, 0.01)
		})
	}
}

func TestCalculateConfidenceWithGroupReasons(t *testing.T) {
	t.Parallel()

	// Test with group reasons to verify generic type works
	groupReasons := types.Reasons[enum.GroupReasonType]{
		enum.GroupReasonTypeMember: {
			Message:    "test",
			Confidence: 0.8,
		},
	}

	got := utils.CalculateConfidence(groupReasons)
	assert.InEpsilon(t, 0.8, got, 0.01)
}

func TestCalculateConfidenceRounding(t *testing.T) {
	t.Parallel()

	reasons := types.Reasons[enum.UserReasonType]{
		enum.UserReasonTypeDescription: {
			Message:    "test1",
			Confidence: 0.823,
		},
		enum.UserReasonTypeFriend: {
			Message:    "test2",
			Confidence: 0.756,
		},
	}

	got := utils.CalculateConfidence(reasons)
	assert.InEpsilon(t, 0.81, got, 0.01)
}
