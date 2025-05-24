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
	// ErrInvalidDurationFormat indicates that the duration string is not in the correct format.
	ErrInvalidDurationFormat = errors.New("invalid duration format")
	// ErrInvalidNumberFormat indicates that the number string is not in the correct format.
	ErrInvalidNumberFormat = errors.New("invalid number format")
	// ErrInvalidTimeFormat indicates that the time string is not in the correct format.
	ErrInvalidTimeFormat = errors.New("invalid time format")
	// ErrInvalidTimezone indicates that the timezone string is not valid.
	ErrInvalidTimezone = errors.New("invalid timezone")
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

// ParseCombinedDuration parses a duration string that may contain days (d) along with
// other time units. It supports formats like "1d", "24h", "1d12h", "1d12h30m", etc.
// This is more flexible than Go's standard time.ParseDuration which doesn't support days.
func ParseCombinedDuration(s string) (time.Duration, error) {
	// Trim spaces and convert to lowercase
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return 0, ErrInvalidDurationFormat
	}

	// Remove all whitespace for easier parsing
	s = strings.ReplaceAll(s, " ", "")

	// Simple case: standard Go duration without days
	if !strings.Contains(s, "d") {
		return time.ParseDuration(s)
	}

	// Simple case: just days
	if strings.HasSuffix(s, "d") && !strings.Contains(s[:len(s)-1], "d") {
		daysStr := s[:len(s)-1]
		days, err := parseFloat(daysStr)
		if err != nil {
			return 0, fmt.Errorf("invalid days value: %w", err)
		}
		return time.Duration(days * 24 * float64(time.Hour)), nil
	}

	// For combined durations with days, use token-based parsing
	var totalDuration time.Duration
	currentNumber := ""

	for i := 0; i < len(s); i++ {
		char := s[i]

		// Collect digits for the current number
		if isDigit(char) || char == '.' {
			currentNumber += string(char)
			continue
		}

		// We've reached a unit designator
		if currentNumber == "" {
			return 0, fmt.Errorf("%w: unexpected character %c", ErrInvalidDurationFormat, char)
		}

		// Identify the unit (support for multi-character units)
		unit := string(char)
		if i+1 < len(s) && !isDigit(s[i+1]) && s[i+1] != '.' {
			unit += string(s[i+1])
			i++
		}

		// Parse the number value
		value, err := parseFloat(currentNumber)
		if err != nil {
			return 0, fmt.Errorf("invalid duration value: %w", err)
		}
		currentNumber = ""

		// Add to total duration based on unit
		switch unit {
		case "d":
			totalDuration += time.Duration(value * 24 * float64(time.Hour))
		case "h":
			totalDuration += time.Duration(value * float64(time.Hour))
		case "m":
			totalDuration += time.Duration(value * float64(time.Minute))
		case "s":
			totalDuration += time.Duration(value * float64(time.Second))
		case "ms":
			totalDuration += time.Duration(value * float64(time.Millisecond))
		case "us", "µs":
			totalDuration += time.Duration(value * float64(time.Microsecond))
		case "ns":
			totalDuration += time.Duration(value * float64(time.Nanosecond))
		default:
			return 0, fmt.Errorf("%w: unknown unit %s", ErrInvalidDurationFormat, unit)
		}
	}

	// Check if we ended with a number without a unit
	if currentNumber != "" {
		return 0, fmt.Errorf("%w: missing unit after number", ErrInvalidDurationFormat)
	}

	return totalDuration, nil
}

func isDigit(c byte) bool {
	return '0' <= c && c <= '9'
}

func parseFloat(s string) (float64, error) {
	// Simple string to float conversion that handles leading/trailing spaces
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, ErrInvalidNumberFormat
	}

	var result float64
	var isNegative bool

	// Handle negative numbers
	if s[0] == '-' {
		isNegative = true
		s = s[1:]
	}

	// Parse digits before decimal point
	i := 0
	for i < len(s) && isDigit(s[i]) {
		result = result*10 + float64(s[i]-'0')
		i++
	}

	// Handle decimal point
	if i < len(s) && s[i] == '.' {
		i++
		divisor := 10.0
		for i < len(s) && isDigit(s[i]) {
			result += float64(s[i]-'0') / divisor
			divisor *= 10
			i++
		}
	}

	// Check if there are any remaining characters
	if i < len(s) {
		return 0, fmt.Errorf("%w: %s", ErrInvalidNumberFormat, s)
	}

	if isNegative {
		result = -result
	}

	return result, nil
}

// FormatTimeAgo returns a human-readable string representing how long ago a time was.
func FormatTimeAgo(t time.Time) string {
	if t.IsZero() {
		return "never"
	}

	duration := time.Since(t)
	return FormatDuration(duration) + " ago"
}

// FormatTimeUntil returns a human-readable string representing how long until a time.
func FormatTimeUntil(t time.Time) string {
	if t.IsZero() {
		return "unknown"
	}

	duration := time.Until(t)
	return "in " + FormatDuration(duration)
}

// FormatDuration converts a duration to a human-readable string.
func FormatDuration(d time.Duration) string {
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

// ParseTimeWithTimezone parses a time string with optional timezone support.
// Supported formats:
// - "2006-01-02 15:04:05" (assumes UTC if no timezone specified)
// - "2006-01-02 15:04:05 UTC"
// - "2006-01-02 15:04:05 America/New_York"
// - "2006-01-02T15:04:05Z" (RFC3339)
// - "2006-01-02T15:04:05-07:00" (RFC3339 with timezone)
// - "2006-01-02" (date only, assumes 00:00:00 UTC).
func ParseTimeWithTimezone(timeStr string) (time.Time, error) {
	timeStr = strings.TrimSpace(timeStr)
	if timeStr == "" {
		return time.Time{}, ErrInvalidTimeFormat
	}

	// Try RFC3339 format first (includes timezone)
	if t, err := time.Parse(time.RFC3339, timeStr); err == nil {
		return t, nil
	}

	// Try RFC3339 without nanoseconds
	if t, err := time.Parse("2006-01-02T15:04:05Z", timeStr); err == nil {
		return t, nil
	}

	// Try date only format (assume UTC)
	if t, err := time.Parse("2006-01-02", timeStr); err == nil {
		return t.UTC(), nil
	}

	// Try datetime with timezone name
	parts := strings.Fields(timeStr)
	if len(parts) >= 3 {
		// Format: "2006-01-02 15:04:05 America/New_York" or "2006-01-02 15:04:05 UTC"
		dateTimePart := strings.Join(parts[:2], " ")
		timezonePart := parts[2]

		// Parse the datetime part
		t, err := time.Parse("2006-01-02 15:04:05", dateTimePart)
		if err != nil {
			return time.Time{}, fmt.Errorf("%w: failed to parse datetime part: %w", ErrInvalidTimeFormat, err)
		}

		// Handle timezone
		if timezonePart == "UTC" || timezonePart == "utc" {
			return t.UTC(), nil
		}

		// Try to load the timezone
		loc, err := time.LoadLocation(timezonePart)
		if err != nil {
			return time.Time{}, fmt.Errorf("%w: %s: %w", ErrInvalidTimezone, timezonePart, err)
		}

		// Convert to the specified timezone and then to UTC
		return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), loc), nil
	}

	// Try datetime without timezone (assume UTC)
	if len(parts) == 2 {
		t, err := time.Parse("2006-01-02 15:04:05", timeStr)
		if err != nil {
			return time.Time{}, fmt.Errorf("%w: %w", ErrInvalidTimeFormat, err)
		}
		return t.UTC(), nil
	}

	return time.Time{}, fmt.Errorf("%w: unsupported format: %s", ErrInvalidTimeFormat, timeStr)
}
