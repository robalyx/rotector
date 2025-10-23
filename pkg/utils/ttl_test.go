package utils_test

import (
	"testing"
	"time"

	"github.com/robalyx/rotector/pkg/utils"
	"github.com/stretchr/testify/assert"
)

func TestCalculateProcessingInterval(t *testing.T) {
	t.Parallel()

	now := time.Now()

	tests := []struct {
		name             string
		createdAt        time.Time
		expectedInterval time.Duration
		deltaPercent     float64 // Allow percentage deviation for approximate checks
	}{
		{
			name:             "very new account (12 hours old)",
			createdAt:        now.Add(-12 * time.Hour),
			expectedInterval: 24 * time.Hour, // minimum enforced
			deltaPercent:     0.01,
		},
		{
			name:             "account exactly 1 day old",
			createdAt:        now.Add(-24 * time.Hour),
			expectedInterval: time.Duration(24.81 * float64(time.Hour)), // ~1.03 days
			deltaPercent:     0.05,
		},
		{
			name:             "account 7 days old",
			createdAt:        now.Add(-7 * 24 * time.Hour),
			expectedInterval: time.Duration(39.1 * float64(time.Hour)), // ~1.63 days
			deltaPercent:     0.05,
		},
		{
			name:             "account 14 days old",
			createdAt:        now.Add(-14 * 24 * time.Hour),
			expectedInterval: time.Duration(66.7 * float64(time.Hour)), // ~2.78 days
			deltaPercent:     0.05,
		},
		{
			name:             "account 30 days old",
			createdAt:        now.Add(-30 * 24 * time.Hour),
			expectedInterval: time.Duration(157.6 * float64(time.Hour)), // ~6.57 days
			deltaPercent:     0.05,
		},
		{
			name:             "account 60 days old",
			createdAt:        now.Add(-60 * 24 * time.Hour),
			expectedInterval: time.Duration(402.6 * float64(time.Hour)), // ~16.78 days
			deltaPercent:     0.05,
		},
		{
			name:             "account exactly 90 days old (threshold)",
			createdAt:        now.Add(-90 * 24 * time.Hour),
			expectedInterval: 30 * 24 * time.Hour, // maximum
			deltaPercent:     0.01,
		},
		{
			name:             "account 120 days old (beyond threshold)",
			createdAt:        now.Add(-120 * 24 * time.Hour),
			expectedInterval: 30 * 24 * time.Hour, // maximum
			deltaPercent:     0.01,
		},
		{
			name:             "account 365 days old (1 year)",
			createdAt:        now.Add(-365 * 24 * time.Hour),
			expectedInterval: 30 * 24 * time.Hour, // maximum
			deltaPercent:     0.01,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := utils.CalculateProcessingInterval(tt.createdAt)

			// Use InDelta with percentage to allow for reasonable variance
			delta := float64(tt.expectedInterval) * tt.deltaPercent
			assert.InDelta(t, float64(tt.expectedInterval), float64(got), delta,
				"expected interval ~%v, got %v", tt.expectedInterval, got)
		})
	}
}

func TestCalculateProcessingInterval_Boundaries(t *testing.T) {
	t.Parallel()

	now := time.Now()

	t.Run("minimum interval enforced", func(t *testing.T) {
		t.Parallel()

		// Account less than 1 day old should get minimum interval
		createdAt := now.Add(-12 * time.Hour)
		interval := utils.CalculateProcessingInterval(createdAt)
		assert.Equal(t, 24*time.Hour, interval, "expected minimum interval of 1 day")
	})

	t.Run("maximum interval enforced", func(t *testing.T) {
		t.Parallel()

		// Account older than threshold should get maximum interval
		createdAt := now.Add(-180 * 24 * time.Hour)
		interval := utils.CalculateProcessingInterval(createdAt)
		assert.Equal(t, 30*24*time.Hour, interval, "expected maximum interval of 30 days")
	})

	t.Run("interval increases monotonically", func(t *testing.T) {
		t.Parallel()

		// Verify that older accounts always have longer or equal intervals
		ages := []time.Duration{
			1 * 24 * time.Hour,
			7 * 24 * time.Hour,
			14 * 24 * time.Hour,
			30 * 24 * time.Hour,
			60 * 24 * time.Hour,
			90 * 24 * time.Hour,
		}

		var previousInterval time.Duration

		for _, age := range ages {
			createdAt := now.Add(-age)
			interval := utils.CalculateProcessingInterval(createdAt)

			if previousInterval > 0 {
				assert.GreaterOrEqual(t, interval, previousInterval,
					"interval should increase or stay same as account age increases")
			}

			previousInterval = interval
		}
	})
}

func TestCalculateProcessingInterval_PowerCurve(t *testing.T) {
	t.Parallel()

	now := time.Now()

	// Verify the power curve behavior: intervals should grow faster for older accounts
	t.Run("early growth is slower than later growth", func(t *testing.T) {
		t.Parallel()

		// Compare growth rate in first 30 days vs next 30 days
		interval1Day := utils.CalculateProcessingInterval(now.Add(-1 * 24 * time.Hour))
		interval30Days := utils.CalculateProcessingInterval(now.Add(-30 * 24 * time.Hour))
		interval60Days := utils.CalculateProcessingInterval(now.Add(-60 * 24 * time.Hour))

		// Growth from day 1 to day 30
		earlyGrowth := interval30Days - interval1Day
		// Growth from day 30 to day 60
		laterGrowth := interval60Days - interval30Days

		assert.Greater(t, laterGrowth, earlyGrowth,
			"power curve should show acceleration: later growth should exceed early growth")
	})
}
