package utils

import (
	"errors"
	"regexp"
	"strings"
)

const (
	// Number of matches required to extract a URL from a string.
	requiredURLMatches = 2
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
	if !IsRobloxProfileURL(url) {
		return "", ErrInvalidProfileURL
	}

	matches := userURLPattern.FindStringSubmatch(strings.TrimSpace(url))
	if len(matches) < requiredURLMatches {
		return "", ErrInvalidProfileURL
	}
	return matches[1], nil
}

// ExtractGroupIDFromURL extracts the group ID from a Roblox group URL.
func ExtractGroupIDFromURL(url string) (string, error) {
	if !IsRobloxGroupURL(url) {
		return "", ErrInvalidGroupURL
	}

	matches := groupURLPattern.FindStringSubmatch(strings.TrimSpace(url))
	if len(matches) < requiredURLMatches {
		return "", ErrInvalidGroupURL
	}
	return matches[1], nil
}
