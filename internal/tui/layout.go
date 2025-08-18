package tui

const (
	MinTerminalWidth = 80
	LargeWidth       = 120
)

// isTerminalTooSmall checks if terminal is usable.
func isTerminalTooSmall(width int) bool {
	return width < MinTerminalWidth
}

// shouldShowDetails determines if detailed info should be shown.
func shouldShowDetails(width int) bool {
	return width >= LargeWidth
}

// calculateHeaderHeight calculates header height.
func calculateHeaderHeight(hasNavigation, hasTabs bool) int {
	height := 1
	if hasNavigation {
		height += 2
	}

	if hasTabs {
		height++
	}

	return height
}

// calculateContentHeight calculates available content height.
func calculateContentHeight(totalHeight, headerHeight int) int {
	content := totalHeight - headerHeight - 2
	if content < 5 {
		return 5
	}

	return content
}

// truncateText truncates text if too long.
func truncateText(text string, maxWidth int) string {
	if len(text) <= maxWidth {
		return text
	}

	if maxWidth < 4 {
		return "..."
	}

	return text[:maxWidth-3] + "..."
}
