package utils

import (
	"math"
	"time"
)

const (
	// MinProcessingInterval is the minimum time between processing attempts for very new accounts.
	MinProcessingInterval = 24 * time.Hour // 1 day
	// MaxProcessingInterval is the maximum time between processing attempts for established accounts.
	MaxProcessingInterval = 30 * 24 * time.Hour // 30 days
	// ProcessingIntervalThreshold is the account age at which the maximum processing interval is reached.
	ProcessingIntervalThreshold = 90 * 24 * time.Hour // 90 days
	// ProcessingIntervalExponent controls the scaling curve shape.
	ProcessingIntervalExponent = 1.5
)

// CalculateProcessingInterval determines how long to wait before reprocessing a user
// based on their account age. Newer accounts are processed more frequently (high risk)
// while older accounts are processed less frequently (lower risk).
//
// The formula uses a power curve with exponent 1.5 to front-load checking on new accounts:
//
//	interval = min + (max - min) * (age / threshold) ^ exponent
func CalculateProcessingInterval(createdAt time.Time) time.Duration {
	accountAge := time.Since(createdAt)

	// Accounts older than threshold get maximum interval
	if accountAge >= ProcessingIntervalThreshold {
		return MaxProcessingInterval
	}

	// Accounts younger than minimum interval get minimum interval
	if accountAge < MinProcessingInterval {
		return MinProcessingInterval
	}

	// Calculate scaled interval using power curve
	ageRatio := float64(accountAge) / float64(ProcessingIntervalThreshold)
	scaleFactor := math.Pow(ageRatio, ProcessingIntervalExponent)
	intervalRange := float64(MaxProcessingInterval - MinProcessingInterval)
	scaledInterval := MinProcessingInterval + time.Duration(intervalRange*scaleFactor)

	return scaledInterval
}
