package utils

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var (
	// ErrInvalidRobloxMarkdown indicates the markdown format is invalid.
	ErrInvalidRobloxMarkdown = errors.New("invalid roblox markdown format")
	// ErrInvalidRobloxUserID indicates the user ID cannot be parsed.
	ErrInvalidRobloxUserID = errors.New("invalid roblox user id")
)

var (
	// MultipleSpaces matches any sequence of whitespace (including newlines).
	MultipleSpaces = regexp.MustCompile(`\s+`)
	// ValidCommentCharsRegex matches only allowed characters in community notes.
	ValidCommentCharsRegex = regexp.MustCompile(`^[a-zA-Z0-9\s.,''\-\n]+$`)
	// RobloxMarkdownRegex matches Roblox user information in markdown format.
	RobloxMarkdownRegex = regexp.MustCompile(`\[(.+)\]\(https://www\.roblox\.com/users/(\d+)/profile\)\s*\((\d+)\)`)
)

// CompressAllWhitespace replaces all whitespace sequences (including newlines) with a single space.
// This is useful for cases where you want to completely normalize whitespace.
func CompressAllWhitespace(s string) string {
	return strings.TrimSpace(MultipleSpaces.ReplaceAllString(s, " "))
}

// CompressWhitespacePreserveNewlines replaces multiple consecutive spaces with a single space
// while preserving newlines. This is useful for maintaining text formatting while cleaning up spacing.
func CompressWhitespacePreserveNewlines(s string) string {
	// First, normalize line endings to \n
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")

	// Split by newlines, trim and compress spaces on each line
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		// Compress multiple spaces into single space and trim
		lines[i] = strings.Join(strings.Fields(line), " ")
	}

	// Join lines back together and trim any leading/trailing empty lines
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// SplitLines takes a slice of strings and splits any strings containing newlines
// into separate entries. Handles both regular and escaped newlines.
// Empty lines after splitting are omitted from the result.
func SplitLines(content []string) []string {
	var result []string

	for _, item := range content {
		// Handle both escaped and unescaped newlines
		item = strings.ReplaceAll(item, "\\n", "\n")

		// Split on newlines and add each line as a separate entry
		lines := strings.SplitSeq(item, "\n")
		for line := range lines {
			// Trim spaces and add non-empty lines
			line = strings.TrimSpace(line)
			if line != "" {
				result = append(result, line)
			}
		}
	}

	return result
}

// ParseDelimitedInput takes a text input and splits it by the given delimiter.
func ParseDelimitedInput(input, delimiter string) []string {
	if input == "" {
		return nil
	}

	var result []string

	for item := range strings.SplitSeq(input, delimiter) {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			result = append(result, trimmed)
		}
	}

	return result
}

// ValidateCommentText checks if the text contains only allowed characters for community notes.
// Returns true if the text is valid, false otherwise.
func ValidateCommentText(text string) bool {
	if text == "" {
		return false
	}

	// Check if the text contains only whitespace
	if strings.TrimSpace(text) == "" {
		return false
	}

	// Check if all characters in the text are allowed
	return ValidCommentCharsRegex.MatchString(text)
}

// ParseRobloxMarkdown extracts Roblox user ID and username from markdown-formatted text.
// Expected format: [Username](https://www.roblox.com/users/USERID/profile) (USERID)
// Returns the user ID, username, and an error if the format is invalid.
func ParseRobloxMarkdown(content string) (int64, string, error) {
	matches := RobloxMarkdownRegex.FindStringSubmatch(content)

	if len(matches) < 4 {
		return 0, "", ErrInvalidRobloxMarkdown
	}

	username := matches[1]

	robloxID, err := strconv.ParseInt(matches[3], 10, 64)
	if err != nil {
		return 0, "", fmt.Errorf("%w: %s", ErrInvalidRobloxUserID, matches[3])
	}

	return robloxID, username, nil
}
