package utils_test

import (
	"testing"
	"time"

	"github.com/robalyx/rotector/internal/bot/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseDateRange(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		dateRange string
		wantErr   error
		wantStart string
		wantEnd   string
	}{
		{
			name:      "valid date range",
			dateRange: "2023-01-01 to 2023-01-02",
			wantStart: "2023-01-01 00:00:00 +0000 UTC",
			wantEnd:   "2023-01-02 23:59:59 +0000 UTC",
		},
		{
			name:      "same day",
			dateRange: "2023-01-01 to 2023-01-01",
			wantStart: "2023-01-01 00:00:00 +0000 UTC",
			wantEnd:   "2023-01-01 23:59:59 +0000 UTC",
		},
		{
			name:      "invalid format",
			dateRange: "invalid",
			wantErr:   utils.ErrInvalidDateRangeFormat,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			start, end, err := utils.ParseDateRange(tt.dateRange)
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantStart, start.UTC().String())
			assert.Equal(t, tt.wantEnd, end.UTC().String())
		})
	}
}

func TestParseBanDuration(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		duration   string
		wantErr    error
		wantExpiry bool
	}{
		{
			name:       "valid duration",
			duration:   "24h",
			wantExpiry: true,
		},
		{
			name:       "permanent ban",
			duration:   "",
			wantErr:    utils.ErrPermanentBan,
			wantExpiry: false,
		},
		{
			name:       "invalid duration",
			duration:   "invalid",
			wantErr:    nil,
			wantExpiry: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			expiry, err := utils.ParseBanDuration(tt.duration)
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				return
			}

			if tt.wantExpiry {
				assert.NotNil(t, expiry)
				assert.True(t, expiry.After(time.Now()))
			} else if err == nil {
				assert.Nil(t, expiry)
			}
		})
	}
}

func TestParseCombinedDuration(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		input    string
		expected time.Duration
		wantErr  bool
	}{
		// Simple cases
		{
			name:     "days only",
			input:    "2d",
			expected: 2 * 24 * time.Hour,
		},
		{
			name:     "hours only",
			input:    "5h",
			expected: 5 * time.Hour,
		},
		{
			name:     "minutes only",
			input:    "30m",
			expected: 30 * time.Minute,
		},

		// Combined cases
		{
			name:     "days and hours",
			input:    "1d12h",
			expected: 36 * time.Hour,
		},
		{
			name:     "days, hours and minutes",
			input:    "2d6h30m",
			expected: 54*time.Hour + 30*time.Minute,
		},
		{
			name:     "complex case",
			input:    "1d2h30m15s",
			expected: 26*time.Hour + 30*time.Minute + 15*time.Second,
		},
		{
			name:     "decimal days",
			input:    "1.5d",
			expected: 36 * time.Hour,
		},

		// Edge cases and errors
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
		{
			name:    "invalid format",
			input:   "1x2y",
			wantErr: true,
		},
		{
			name:    "invalid number",
			input:   "aa5h",
			wantErr: true,
		},
		{
			name:     "with whitespace",
			input:    " 1d 12h ",
			expected: 36 * time.Hour,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			duration, err := utils.ParseCombinedDuration(tt.input)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expected, duration)
		})
	}
}

func TestFormatTimeAgo(t *testing.T) {
	t.Parallel()
	now := time.Now()
	tests := []struct {
		name string
		time time.Time
		want string
	}{
		{
			name: "zero time",
			time: time.Time{},
			want: "never",
		},
		{
			name: "moments ago",
			time: now.Add(-30 * time.Second),
			want: "moments ago",
		},
		{
			name: "minutes ago",
			time: now.Add(-5 * time.Minute),
			want: "5 minutes ago",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := utils.FormatTimeAgo(tt.time)
			assert.Equal(t, tt.want, got)
		})
	}
}
