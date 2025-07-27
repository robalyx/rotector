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
	gameURLPattern = regexp.MustCompile(
		`(?i)(?:(?:\[.*?\]\()?(?:https?://)?(?:www\.)?roblox\.com/games/(\d+)(?:/.*?)?\)?|(?:^|\s)(\d+)(?:$|\s))`,
	)

	ErrInvalidProfileURL = errors.New("invalid Roblox profile URL format")
	ErrInvalidGroupURL   = errors.New("invalid Roblox group URL format")
	ErrInvalidGameURL    = errors.New("invalid Roblox game URL format")
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

// IsRobloxGameURL checks if the given string is a Roblox game URL.
func IsRobloxGameURL(input string) bool {
	matches := gameURLPattern.FindStringSubmatch(strings.TrimSpace(input))
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

// ExtractGameIDFromURL extracts the game ID from a Roblox game URL or message.
func ExtractGameIDFromURL(url string) (string, error) {
	input := strings.TrimSpace(url)

	matches := gameURLPattern.FindStringSubmatch(input)
	if len(matches) == 0 {
		return "", ErrInvalidGameURL
	}

	// If it's an invalid URL format but contains numbers, reject it
	if strings.Contains(input, "roblox.com") && matches[1] == "" {
		return "", ErrInvalidGameURL
	}

	// Return first non-empty capture group (either URL or raw ID)
	for i := 1; i < len(matches); i++ {
		if matches[i] != "" {
			return matches[i], nil
		}
	}

	return "", ErrInvalidGameURL
}
