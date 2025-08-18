package components

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/fsnotify/fsnotify"
	"github.com/robalyx/rotector/internal/tui/styles"
)

// FileChangedMsg indicates a log file has been modified.
type FileChangedMsg struct {
	Path string
}

// LogViewer displays log files with automatic file watching.
type LogViewer struct {
	logPath       string
	lines         []string
	maxLines      int
	viewport      viewport.Model
	logLevelRegex *regexp.Regexp
	watcher       *fsnotify.Watcher
	done          chan bool
}

// NewLogViewer creates a new log viewer.
func NewLogViewer() *LogViewer {
	vp := viewport.New(80, 24)

	return &LogViewer{
		lines:         make([]string, 0),
		maxLines:      1000, // Keep last 1000 lines
		viewport:      vp,
		logLevelRegex: regexp.MustCompile(`(?i)(ERROR|WARN|INFO|DEBUG)`),
		done:          make(chan bool),
	}
}

// StartWatching begins watching the current log file for changes.
func (lv *LogViewer) StartWatching() tea.Cmd {
	if lv.logPath == "" || lv.watcher != nil {
		return nil
	}

	return func() tea.Msg {
		watcher, err := fsnotify.NewWatcher()
		if err != nil {
			return nil
		}

		lv.watcher = watcher

		// Add the file to the watcher
		if err := watcher.Add(lv.logPath); err != nil {
			watcher.Close()
			lv.watcher = nil

			return nil
		}

		// Start watching in a goroutine that sends messages
		go func() {
			defer watcher.Close()

			for {
				select {
				case event, ok := <-watcher.Events:
					if !ok {
						return
					}

					if event.Has(fsnotify.Write) {
						go lv.loadLogContent()
					}

				case <-watcher.Errors:
					// Ignore watcher errors

				case <-lv.done:
					return
				}
			}
		}()

		return nil
	}
}

// StopWatching stops watching the current log file.
func (lv *LogViewer) StopWatching() {
	if lv.watcher != nil {
		close(lv.done)
		lv.watcher.Close()
		lv.watcher = nil
		lv.done = make(chan bool)
	}
}

// SetLogPath sets the log file path and starts watching it.
func (lv *LogViewer) SetLogPath(path string) {
	// Stop watching the old file
	lv.StopWatching()

	lv.logPath = path
	lv.loadLogContent()

	// Start watching the new file
	if path != "" {
		lv.StartWatching()
	}
}

// SetSize sets the log viewer size.
func (lv *LogViewer) SetSize(width, height int) {
	lv.viewport.Width = width
	lv.viewport.Height = height

	// Adjust max lines based on height
	if height > 0 {
		lv.maxLines = max(height*10, 100) // Keep more lines in buffer than display
	}
}

// Init initializes the log viewer.
func (lv *LogViewer) Init() tea.Cmd {
	return nil
}

// Update handles messages.
func (lv *LogViewer) Update(msg tea.Msg) (*LogViewer, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			lv.viewport.ScrollUp(1)
		case "down", "j":
			lv.viewport.ScrollDown(1)
		case "pgup", "ctrl+u":
			lv.viewport.HalfPageUp()
		case "pgdown", "ctrl+d":
			lv.viewport.HalfPageDown()
		case "home":
			lv.viewport.GotoTop()
		case "end":
			lv.viewport.GotoBottom()
		}
	}

	lv.viewport, cmd = lv.viewport.Update(msg)

	return lv, cmd
}

// Refresh reloads the log content.
func (lv *LogViewer) Refresh() {
	lv.loadLogContent()
}

// View renders the log viewer.
func (lv *LogViewer) View() string {
	if lv.logPath == "" {
		return lipgloss.NewStyle().
			Foreground(lipgloss.Color(styles.ColorMuted)).
			Render("No log file selected")
	}

	// Show file info
	fileName := filepath.Base(lv.logPath)
	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(styles.ColorSecondary))

	header := headerStyle.Render(fileName)

	// Set viewport content
	content := strings.Join(lv.lines, "\n")
	if content == "" {
		content = "Waiting for logs..."
	}

	lv.viewport.SetContent(content)
	lv.viewport.GotoBottom() // Always scroll to bottom

	return lipgloss.JoinVertical(lipgloss.Left, header, "", lv.viewport.View())
}

// loadLogContent reads the entire log file and updates the display.
func (lv *LogViewer) loadLogContent() {
	if lv.logPath == "" {
		return
	}

	// Try to read the entire file
	content, err := os.ReadFile(lv.logPath)
	if err != nil {
		lv.lines = []string{}
		return
	}

	// Split into lines and format
	lines := strings.Split(string(content), "\n")

	// Remove empty last line if present
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	// Format lines and keep only last maxLines
	var formattedLines []string

	for _, line := range lines {
		if line != "" {
			formattedLines = append(formattedLines, lv.formatLogLine(line))
		}
	}

	// Keep only the last maxLines
	if len(formattedLines) > lv.maxLines {
		formattedLines = formattedLines[len(formattedLines)-lv.maxLines:]
	}

	lv.lines = formattedLines
}

// formatLogLine formats a log line with appropriate styling and wrapping.
func (lv *LogViewer) formatLogLine(line string) string {
	// Wrap long lines if width is available
	maxLineWidth := lv.viewport.Width
	if maxLineWidth > 0 && len(line) > maxLineWidth {
		line = lv.wrapText(line, maxLineWidth)
	}

	// Color code based on log level
	if matches := lv.logLevelRegex.FindStringSubmatch(line); len(matches) > 0 {
		level := strings.ToUpper(matches[1])
		return styles.LogLevelStyle(level).Render(line)
	}

	return line
}

// wrapText wraps long text lines to fit within the specified width.
func (lv *LogViewer) wrapText(text string, width int) string {
	if width <= 0 || len(text) <= width {
		return text
	}

	var wrapped []string

	currentLine := ""
	words := strings.Fields(text)

	for _, word := range words {
		// If adding this word would exceed width, start new line
		if len(currentLine)+len(word)+1 > width {
			if currentLine != "" {
				wrapped = append(wrapped, currentLine)
			}

			// If single word is longer than width, break it
			if len(word) > width {
				for len(word) > width {
					wrapped = append(wrapped, word[:width])
					word = word[width:]
				}

				currentLine = word
			} else {
				currentLine = word
			}
		} else {
			if currentLine == "" {
				currentLine = word
			} else {
				currentLine += " " + word
			}
		}
	}

	// Add remaining line
	if currentLine != "" {
		wrapped = append(wrapped, currentLine)
	}

	return strings.Join(wrapped, "\n")
}
