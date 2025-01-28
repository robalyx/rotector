package utils

import (
	"errors"
	"regexp"
	"strings"
)

var (
	userURLPattern  = regexp.MustCompile(`(?i)(?:https?://)?(?:www\.)?roblox\.com/users/(\d+)(?:/.*)?`)
	groupURLPattern = regexp.MustCompile(`(?i)(?:https?://)?(?:www\.)?roblox\.com/(?:groups|communities)/(\d+)(?:/.*)?`)

	ErrInvalidProfileURL = errors.New("invalid Roblox profile URL format")
	ErrInvalidGroupURL   = errors.New("invalid Roblox group URL format")
)

// IsRobloxProfileURL checks if the given string is a Roblox profile URL.
func IsRobloxProfileURL(input string) bool {
	return userURLPattern.MatchString(strings.TrimSpace(input))
}

// IsRobloxGroupURL checks if the given string is a Roblox group URL.
func IsRobloxGroupURL(input string) bool {
	return groupURLPattern.MatchString(strings.TrimSpace(input))
}

// ExtractUserIDFromURL extracts the user ID from a Roblox profile URL.
func ExtractUserIDFromURL(url string) (string, error) {
	matches := userURLPattern.FindStringSubmatch(strings.TrimSpace(url))
	if len(matches) < 2 {
		return "", ErrInvalidProfileURL
	}
	return matches[1], nil
}

// ExtractGroupIDFromURL extracts the group ID from a Roblox group URL.
func ExtractGroupIDFromURL(url string) (string, error) {
	matches := groupURLPattern.FindStringSubmatch(strings.TrimSpace(url))
	if len(matches) < 2 {
		return "", ErrInvalidGroupURL
	}
	return matches[1], nil
}
