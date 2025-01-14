package utils

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestParseDateRange(t *testing.T) {
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
			wantErr:   ErrInvalidDateRangeFormat,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, end, err := ParseDateRange(tt.dateRange)
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				return
			}

			assert.NoError(t, err)
			assert.Equal(t, tt.wantStart, start.UTC().String())
			assert.Equal(t, tt.wantEnd, end.UTC().String())
		})
	}
}

func TestParseBanDuration(t *testing.T) {
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
			wantErr:    ErrPermanentBan,
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
			expiry, err := ParseBanDuration(tt.duration)
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

func TestFormatTimeAgo(t *testing.T) {
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
			got := FormatTimeAgo(tt.time)
			assert.Equal(t, tt.want, got)
		})
	}
}
