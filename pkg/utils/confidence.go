package utils

import (
	"math"

	"github.com/robalyx/rotector/internal/database/types"
)

// CalculateConfidence calculates the final confidence score from a set of reasons.
// Returns a value between 0 and 1, rounded to 2 decimal places.
func CalculateConfidence[T types.ReasonType](reasons types.Reasons[T]) float64 {
	if len(reasons) == 0 {
		return 0
	}

	var totalConfidence float64
	var maxConfidence float64

	// Sum up confidence from all reasons and track highest individual confidence
	for _, reason := range reasons {
		totalConfidence += reason.Confidence
		if reason.Confidence > maxConfidence {
			maxConfidence = reason.Confidence
		}
	}

	// Calculate average but weight it towards highest confidence
	// 70% highest confidence + 30% average confidence
	avgConfidence := totalConfidence / float64(len(reasons))
	finalConfidence := (maxConfidence * 0.7) + (avgConfidence * 0.3)

	// Round to 2 decimal places and ensure it's between 0 and 1
	finalConfidence = math.Round(finalConfidence*100) / 100
	return math.Max(0, math.Min(1, finalConfidence))
}
