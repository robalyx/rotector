package utils

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Regular expression to clean up excessive newlines in descriptions.
var multipleNewlinesRegex = regexp.MustCompile(`\n{4,}`)

// TruncateString truncates a string to a maximum length.
func TruncateString(s string, maxLength int) string {
	if len(s) > maxLength {
		return s[:maxLength-3] + "..."
	}
	return s
}

// FormatString formats a string by trimming it, replacing backticks, and enclosing in markdown.
func FormatString(s string) string {
	s = multipleNewlinesRegex.ReplaceAllString(s, "\n")
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "`", "")
	return fmt.Sprintf("```\n%s\n```", s)
}

// FormatIDs formats a slice of user IDs into a readable string with mentions.
func FormatIDs(ids []uint64) string {
	if len(ids) == 0 {
		return "None"
	}

	mentions := make([]string, len(ids))
	for i, id := range ids {
		mentions[i] = fmt.Sprintf("<@%d>", id)
	}
	return strings.Join(mentions, ", ")
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
