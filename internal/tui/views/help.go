package views

import (
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/robalyx/rotector/internal/tui/styles"
)

// Help represents the help view.
type Help struct {
	width    int
	height   int
	viewport viewport.Model
}

// NewHelp creates a new help view.
func NewHelp() *Help {
	vp := viewport.New(80, 24)
	vp.Style = lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(styles.ColorBorder)).
		Padding(1, 2)

	return &Help{
		viewport: vp,
	}
}

// SetSize sets the help view size.
func (h *Help) SetSize(width, height int) {
	h.width = width
	h.height = height
	h.viewport.Width = width - 4
	h.viewport.Height = height - 4
}

// Init initializes the help view.
func (h *Help) Init() tea.Cmd {
	return nil
}

// Update handles messages for the help view.
func (h *Help) Update(msg tea.Msg) (*Help, tea.Cmd) {
	var cmd tea.Cmd

	// Handle viewport scrolling
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			h.viewport.ScrollUp(1)
		case "down", "j":
			h.viewport.ScrollDown(1)
		case "pgup", "ctrl+u":
			h.viewport.HalfPageUp()
		case "pgdown", "ctrl+d":
			h.viewport.HalfPageDown()
		case "home":
			h.viewport.GotoTop()
		case "end":
			h.viewport.GotoBottom()
		}
	}

	h.viewport, cmd = h.viewport.Update(msg)

	return h, cmd
}

// View renders the help view.
func (h *Help) View() string {
	// Styles
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(styles.ColorSecondary)).
		MarginBottom(1)

	sectionStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(styles.ColorPrimary)).
		MarginTop(1).
		MarginBottom(1)

	keyStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(styles.ColorWarning)).
		Width(15)

	descStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(styles.ColorWhite))

	// Content sections
	content := make([]string, 0, 50)

	// Title
	content = append(content, titleStyle.Render("🔧 Rotector Worker Monitor - Help"))
	content = append(content, "")

	// Navigation section
	content = append(content, sectionStyle.Render("📍 Navigation"))
	navItems := [][]string{
		{"d", "Switch to Dashboard view"},
		{"l", "Switch to Worker Logs view"},
		{"?", "Show this help screen"},
		{"q", "Quit the application"},
		{"Ctrl+C", "Force quit"},
	}

	for _, item := range navItems {
		line := lipgloss.JoinHorizontal(lipgloss.Left,
			keyStyle.Render(item[0]),
			descStyle.Render(item[1]))
		content = append(content, line)
	}

	content = append(content, "")

	// Worker navigation section
	content = append(content, sectionStyle.Render("👷 Worker Navigation"))
	workerItems := [][]string{
		{"Tab", "Switch to next worker"},
		{"Shift+Tab", "Switch to previous worker"},
		{"←/→", "Navigate between workers"},
	}

	for _, item := range workerItems {
		line := lipgloss.JoinHorizontal(lipgloss.Left,
			keyStyle.Render(item[0]),
			descStyle.Render(item[1]))
		content = append(content, line)
	}

	content = append(content, "")

	// Log viewer section
	content = append(content, sectionStyle.Render("📋 Log Viewer Controls"))
	logItems := [][]string{
		{"↑/k", "Scroll up one line"},
		{"↓/j", "Scroll down one line"},
		{"PageUp", "Scroll up one page"},
		{"PageDown", "Scroll down one page"},
		{"Ctrl+U", "Scroll up half page"},
		{"Ctrl+D", "Scroll down half page"},
		{"Home", "Go to top of logs"},
		{"End", "Go to bottom (resume following)"},
	}

	for _, item := range logItems {
		line := lipgloss.JoinHorizontal(lipgloss.Left,
			keyStyle.Render(item[0]),
			descStyle.Render(item[1]))
		content = append(content, line)
	}

	content = append(content, "")

	// Status indicators section
	content = append(content, sectionStyle.Render("🟢 Status Indicators"))
	statusItems := [][]string{
		{"🟢 Green", "Worker is healthy and operating normally"},
		{"🔴 Red", "Worker has encountered errors or is unhealthy"},
		{"🟡 Yellow", "Worker is processing or in transitional state"},
		{"⚪ Gray", "Worker is idle or waiting for tasks"},
	}

	for _, item := range statusItems {
		line := lipgloss.JoinHorizontal(lipgloss.Left,
			keyStyle.Render(item[0]),
			descStyle.Render(item[1]))
		content = append(content, line)
	}

	content = append(content, "")

	// Footer
	footerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(styles.ColorMuted)).
		MarginTop(2)

	content = append(content, footerStyle.Render("Press any navigation key to exit help"))

	// Join all content
	helpContent := strings.Join(content, "\n")

	// Set viewport content
	h.viewport.SetContent(helpContent)

	// Return viewport view
	return h.viewport.View()
}
