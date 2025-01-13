package utils

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	// ErrInvalidDateRangeFormat indicates that the date range string is not in the format "YYYY-MM-DD to YYYY-MM-DD".
	ErrInvalidDateRangeFormat = errors.New("invalid date range format")
	// ErrInvalidStartDate indicates that the start date could not be parsed from the provided string.
	ErrInvalidStartDate = errors.New("invalid start date")
	// ErrInvalidEndDate indicates that the end date could not be parsed from the provided string.
	ErrInvalidEndDate = errors.New("invalid end date")
	// ErrEndDateBeforeStartDate indicates that the end date occurs before the start date.
	ErrEndDateBeforeStartDate = errors.New("end date cannot be before start date")
	// ErrPermanentBan indicates that no duration was specified, meaning a permanent ban.
	ErrPermanentBan = errors.New("permanent ban")
)

// ParseDateRange converts a date range string into start and end time.Time values.
// The input format must be "YYYY-MM-DD to YYYY-MM-DD".
// The end date is automatically set to the end of the day (23:59:59).
func ParseDateRange(dateRangeStr string) (time.Time, time.Time, error) {
	// Split the date range string into start and end parts
	parts := strings.Split(dateRangeStr, "to")
	if len(parts) != 2 {
		return time.Time{}, time.Time{}, ErrInvalidDateRangeFormat
	}

	// Trim spaces from the start and end parts
	startStr := strings.TrimSpace(parts[0])
	endStr := strings.TrimSpace(parts[1])

	// Parse the start date
	startDate, err := time.Parse("2006-01-02", startStr)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("%w: %w", ErrInvalidStartDate, err)
	}

	// Parse the end date
	endDate, err := time.Parse("2006-01-02", endStr)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("%w: %w", ErrInvalidEndDate, err)
	}

	// If the end date is before the start date, return an error
	if endDate.Before(startDate) {
		return time.Time{}, time.Time{}, ErrEndDateBeforeStartDate
	}

	// If the dates are the same, set the end date to the end of the day
	if startDate.Equal(endDate) {
		endDate = endDate.Add(23*time.Hour + 59*time.Minute + 59*time.Second)
	} else {
		// Otherwise, set the end date to the end of the day
		endDate = endDate.Add(24*time.Hour - 1*time.Second)
	}

	return startDate, endDate, nil
}

// ParseBanDuration parses a duration string like "7d" or "24h" into a time.Duration.
// Returns ErrPermanentBan if the duration is empty.
func ParseBanDuration(durationStr string) (*time.Time, error) {
	// Check for permanent ban
	if durationStr == "" {
		return nil, ErrPermanentBan
	}

	// Parse the duration string
	duration, err := time.ParseDuration(strings.ToLower(strings.TrimSpace(durationStr)))
	if err != nil {
		return nil, fmt.Errorf("invalid duration format: %w", err)
	}

	// Calculate expiration time
	expiresAt := time.Now().Add(duration)
	return &expiresAt, nil
}

// FormatTimeAgo returns a human-readable string representing how long ago a time was.
func FormatTimeAgo(t time.Time) string {
	if t.IsZero() {
		return "never"
	}

	duration := time.Since(t)
	return formatDuration(duration) + " ago"
}

// FormatTimeUntil returns a human-readable string representing how long until a time.
func FormatTimeUntil(t time.Time) string {
	if t.IsZero() {
		return "unknown"
	}

	duration := time.Until(t)
	return "in " + formatDuration(duration)
}

// formatDuration converts a duration to a human-readable string.
func formatDuration(d time.Duration) string {
	seconds := int(d.Seconds())

	if seconds < 60 {
		return "moments"
	}

	minutes := seconds / 60
	if minutes < 60 {
		if minutes == 1 {
			return "1 minute"
		}
		return fmt.Sprintf("%d minutes", minutes)
	}

	hours := minutes / 60
	if hours < 24 {
		if hours == 1 {
			return "1 hour"
		}
		return fmt.Sprintf("%d hours", hours)
	}

	days := hours / 24
	if days == 1 {
		return "1 day"
	}
	return fmt.Sprintf("%d days", days)
}
