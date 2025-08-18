package styles

import (
	"github.com/charmbracelet/lipgloss"
)

// Styles used throughout the TUI.
var (
	StatusHealthyStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color(ColorSuccess)).
				Bold(true)

	StatusUnhealthyStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color(ColorError)).
				Bold(true)

	ProgressFilledStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color(ColorSuccess))

	ProgressEmptyStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color(ColorMuted))

	LogInfoStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(ColorWhite))

	LogWarnStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(ColorWarning))

	LogErrorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(ColorError))

	LogDebugStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(ColorMuted))
)

// StatusStyle returns appropriate style for status.
func StatusStyle(healthy bool) lipgloss.Style {
	if healthy {
		return StatusHealthyStyle
	}

	return StatusUnhealthyStyle
}

// LogLevelStyle returns appropriate style for log level.
func LogLevelStyle(level string) lipgloss.Style {
	switch level {
	case "ERROR", "error":
		return LogErrorStyle
	case "WARN", "warn", "WARNING", "warning":
		return LogWarnStyle
	case "DEBUG", "debug":
		return LogDebugStyle
	default:
		return LogInfoStyle
	}
}

// ProgressBarChar returns progress bar characters with styling.
func ProgressBarChar(filled bool) string {
	if filled {
		return ProgressFilledStyle.Render("█")
	}

	return ProgressEmptyStyle.Render("░")
}
