package utils

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rotector/rotector/internal/bot/constants"
	"github.com/rotector/rotector/internal/common/queue"
)

// Regular expression to clean up excessive newlines in descriptions.
var multipleNewlinesRegex = regexp.MustCompile(`\n{4,}`)

var (
	// ErrInvalidDateRangeFormat indicates that the date range string is not in the format "YYYY-MM-DD to YYYY-MM-DD".
	ErrInvalidDateRangeFormat = errors.New("invalid date range format")
	// ErrInvalidStartDate indicates that the start date could not be parsed from the provided string.
	ErrInvalidStartDate = errors.New("invalid start date")
	// ErrInvalidEndDate indicates that the end date could not be parsed from the provided string.
	ErrInvalidEndDate = errors.New("invalid end date")
	// ErrEndDateBeforeStartDate indicates that the end date occurs before the start date.
	ErrEndDateBeforeStartDate = errors.New("end date cannot be before start date")
)

// FormatString formats a string by trimming it, replacing backticks, and enclosing in markdown.
func FormatString(s string) string {
	s = multipleNewlinesRegex.ReplaceAllString(s, "\n")
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "`", "")
	return fmt.Sprintf("```\n%s\n```", s)
}

// NormalizeString sanitizes text by replacing newlines with spaces and removing backticks
// to prevent Discord markdown formatting issues.
func NormalizeString(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\n", " "), "`", "")
}

// GetTimestampedSubtext formats a message with a Discord timestamp and prefix.
// The timestamp shows relative time (e.g., "2 minutes ago") using Discord's timestamp format.
func GetTimestampedSubtext(message string) string {
	if message != "" {
		return fmt.Sprintf("-# `%s` <t:%d:R>", message, time.Now().Unix())
	}
	return ""
}

// FormatNumber formats a number with K/M/B suffixes.
func FormatNumber(n uint64) string {
	if n < 1000 {
		return strconv.FormatUint(n, 10)
	}
	if n < 1000000 {
		return fmt.Sprintf("%.1fK", float64(n)/1000)
	}
	if n < 1000000000 {
		return fmt.Sprintf("%.1fM", float64(n)/1000000)
	}
	return fmt.Sprintf("%.1fB", float64(n)/1000000000)
}

// CensorString partially obscures text by replacing middle characters with 'X'.
// The amount censored is 30% of the string length, centered in the middle.
// Strings of 2 characters or less are fully censored.
func CensorString(s string, streamerMode bool) string {
	// If streamer mode is off, return the original string
	if !streamerMode {
		return s
	}

	// Convert string to runes for proper Unicode handling
	runes := []rune(s)
	length := len(runes)

	// Censor entire string if it's 2 characters or less
	if length <= 2 {
		return strings.Repeat("X", length)
	}

	// Calculate the length to censor (30% of the string)
	censorLength := int(math.Ceil(float64(length) * 0.3))

	// Determine the start and end positions for censoring
	startCensor := (length - censorLength) / 2
	endCensor := startCensor + censorLength

	// Replace middle characters with 'X'
	for i := startCensor; i < endCensor && i < length; i++ {
		runes[i] = 'X'
	}

	// Convert back to string and return
	return string(runes)
}

// CensorStringsInText censors specified strings within a larger text.
// It uses CensorString to censor each target string if streamerMode is enabled.
// The search is case-insensitive.
func CensorStringsInText(text string, streamerMode bool, targets ...string) string {
	if !streamerMode {
		return text
	}

	// Sort targets by length in descending order to handle longer strings first
	// This prevents partial matches of shorter strings within longer ones
	sort.Slice(targets, func(i, j int) bool {
		return len(targets[i]) > len(targets[j])
	})

	// Create a map to store censored versions of targets
	censoredMap := make(map[string]string, len(targets))
	for _, target := range targets {
		if target == "" {
			continue
		}
		// Create case-insensitive regex pattern
		pattern := "(?i)" + regexp.QuoteMeta(target)
		censoredMap[pattern] = CensorString(target, true)
	}

	// Replace each target with its censored version using regex
	result := text
	for pattern, censored := range censoredMap {
		re := regexp.MustCompile(pattern)
		result = re.ReplaceAllString(result, censored)
	}

	return result
}

// GetMessageEmbedColor returns the appropriate embed color based on streamer mode.
// This helps visually distinguish when streamer mode is active.
func GetMessageEmbedColor(streamerMode bool) int {
	if streamerMode {
		return constants.StreamerModeEmbedColor
	}
	return constants.DefaultEmbedColor
}

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

// GetPriorityFromCustomID maps Discord component custom IDs to queue priority levels.
// Returns NormalPriority if the custom ID is not recognized.
func GetPriorityFromCustomID(customID string) string {
	switch customID {
	case constants.QueueHighPriorityCustomID:
		return queue.HighPriority
	case constants.QueueNormalPriorityCustomID:
		return queue.NormalPriority
	case constants.QueueLowPriorityCustomID:
		return queue.LowPriority
	default:
		return queue.NormalPriority
	}
}
