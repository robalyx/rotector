package utils

import (
	"errors"
	"regexp"
	"strings"
)

var (
	userURLPattern = regexp.MustCompile(
		`(?i)(?:(?:\[.*?\]\()?(?:https?://)?(?:www\.)?roblox\.com/users/(\d+)(?:/.*?)?\)?|(?:^|\s)(\d+)(?:$|\s))`,
	)
	groupURLPattern = regexp.MustCompile(
		`(?i)(?:(?:\[.*?\]\()?(?:https?://)?(?:www\.)?roblox\.com/(?:groups|communities)/(\d+)(?:/.*?)?\)?|(?:^|\s)(\d+)(?:$|\s))`,
	)

	ErrInvalidProfileURL = errors.New("invalid Roblox profile URL format")
	ErrInvalidGroupURL   = errors.New("invalid Roblox group URL format")
)

// IsRobloxProfileURL checks if the given string is a Roblox profile URL.
func IsRobloxProfileURL(input string) bool {
	matches := userURLPattern.FindStringSubmatch(strings.TrimSpace(input))
	if len(matches) == 0 {
		return false
	}
	// Only return true if it's a proper URL match (first capture group)
	return matches[1] != ""
}

// IsRobloxGroupURL checks if the given string is a Roblox group URL.
func IsRobloxGroupURL(input string) bool {
	matches := groupURLPattern.FindStringSubmatch(strings.TrimSpace(input))
	if len(matches) == 0 {
		return false
	}
	// Only return true if it's a proper URL match (first capture group)
	return matches[1] != ""
}

// ExtractUserIDFromURL extracts the user ID from a Roblox profile URL or message.
func ExtractUserIDFromURL(url string) (string, error) {
	input := strings.TrimSpace(url)

	matches := userURLPattern.FindStringSubmatch(input)
	if len(matches) == 0 {
		return "", ErrInvalidProfileURL
	}

	// If it's an invalid URL format but contains numbers, reject it
	if strings.Contains(input, "roblox.com") && matches[1] == "" {
		return "", ErrInvalidProfileURL
	}

	// Return first non-empty capture group (either URL or raw ID)
	for i := 1; i < len(matches); i++ {
		if matches[i] != "" {
			return matches[i], nil
		}
	}

	return "", ErrInvalidProfileURL
}

// ExtractGroupIDFromURL extracts the group ID from a Roblox group URL or message.
func ExtractGroupIDFromURL(url string) (string, error) {
	input := strings.TrimSpace(url)

	matches := groupURLPattern.FindStringSubmatch(input)
	if len(matches) == 0 {
		return "", ErrInvalidGroupURL
	}

	// If it's an invalid URL format but contains numbers, reject it
	if strings.Contains(input, "roblox.com") && matches[1] == "" {
		return "", ErrInvalidGroupURL
	}

	// Return first non-empty capture group (either URL or raw ID)
	for i := 1; i < len(matches); i++ {
		if matches[i] != "" {
			return matches[i], nil
		}
	}

	return "", ErrInvalidGroupURL
}
