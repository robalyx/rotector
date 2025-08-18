package components

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/robalyx/rotector/internal/tui/styles"
)

// Tabs represents a tab navigation component.
type Tabs struct {
	tabs     []string
	active   int
	width    int
	maxWidth int
}

// NewTabs creates a new tabs component.
func NewTabs() *Tabs {
	return &Tabs{
		tabs:     make([]string, 0),
		active:   0,
		maxWidth: 20, // Maximum width per tab
	}
}

// SetTabs sets the tab names.
func (t *Tabs) SetTabs(tabs []string) {
	t.tabs = tabs
	if t.active >= len(tabs) {
		t.active = 0
	}
}

// SetActive sets the active tab index.
func (t *Tabs) SetActive(index int) {
	if index >= 0 && index < len(t.tabs) {
		t.active = index
	}
}

// GetActive returns the active tab index.
func (t *Tabs) GetActive() int {
	return t.active
}

// SetWidth sets the total width available for tabs.
func (t *Tabs) SetWidth(width int) {
	t.width = width
	if len(t.tabs) > 0 {
		t.maxWidth = max(8, (width-4)/len(t.tabs))
	}
}

// View renders the tabs.
func (t *Tabs) View() string {
	if len(t.tabs) == 0 {
		return ""
	}

	tabs := make([]string, 0, len(t.tabs))

	for i, tab := range t.tabs {
		// Truncate tab name if too long
		displayName := tab
		if len(displayName) > t.maxWidth-4 { // Account for padding
			displayName = displayName[:t.maxWidth-7] + "..."
		}

		var style lipgloss.Style
		if i == t.active {
			style = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color(styles.ColorPrimary)).
				Background(lipgloss.Color(styles.ColorBackground)).
				Padding(0, 2)
		} else {
			style = lipgloss.NewStyle().
				Foreground(lipgloss.Color(styles.ColorMuted)).
				Padding(0, 2)
		}

		tabs = append(tabs, style.Render(displayName))
	}

	tabsLine := strings.Join(tabs, "│")

	// Add border bottom
	borderStyle := lipgloss.NewStyle().
		BorderStyle(lipgloss.NormalBorder()).
		BorderBottom(true).
		BorderForeground(lipgloss.Color(styles.ColorBorder))

	return borderStyle.Width(t.width - 2).Render(tabsLine)
}

// NextTab moves to the next tab.
func (t *Tabs) NextTab() {
	if len(t.tabs) > 0 {
		t.active = (t.active + 1) % len(t.tabs)
	}
}

// PrevTab moves to the previous tab.
func (t *Tabs) PrevTab() {
	if len(t.tabs) > 0 {
		t.active = (t.active - 1 + len(t.tabs)) % len(t.tabs)
	}
}
