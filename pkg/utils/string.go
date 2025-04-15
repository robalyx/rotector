package utils

import (
	"regexp"
	"strings"
)

// MultipleSpaces matches any sequence of whitespace (including newlines).
var MultipleSpaces = regexp.MustCompile(`\s+`)

// ThinkPatternRegex matches thought process content enclosed in <think> tags.
var ThinkPatternRegex = regexp.MustCompile(`<think>(.*?)</think>`)

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

// ExtractThoughtProcess extracts the thought process from an AI response.
func ExtractThoughtProcess(response string) (thought string, cleanText string) {
	// Find the first match of the think pattern
	matches := ThinkPatternRegex.FindStringSubmatch(response)
	if len(matches) > 1 {
		// Extract and trim the thought content
		thought = strings.TrimSpace(matches[1])

		// Get the full matched pattern including tags
		fullMatch := matches[0]

		// Remove the thought process from the response
		cleanText = strings.Replace(response, fullMatch, "", 1)

		// Normalize all whitespace including newlines
		cleanText = CompressAllWhitespace(cleanText)
	} else {
		// No thought process found
		thought = ""
		cleanText = strings.TrimSpace(response)
	}

	return thought, cleanText
}
