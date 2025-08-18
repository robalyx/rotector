package views

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/robalyx/rotector/internal/tui/styles"
)

// Dashboard represents the dashboard view.
type Dashboard struct {
	workers  []WorkerData
	viewport viewport.Model
}

// NewDashboard creates a new dashboard view.
func NewDashboard() *Dashboard {
	vp := viewport.New(80, 24)

	return &Dashboard{
		workers:  make([]WorkerData, 0),
		viewport: vp,
	}
}

// SetWorkers updates the worker list.
func (d *Dashboard) SetWorkers(workers []WorkerData) {
	d.workers = make([]WorkerData, len(workers))
	copy(d.workers, workers)
}

// WorkerData represents the data needed to display a worker.
type WorkerData struct {
	Name    string
	Type    string
	Status  string
	Healthy bool
	Bar     ProgressBar
}

// ProgressBar interface for rendering progress.
type ProgressBar interface {
	View() string
}

// SetSize sets the dashboard size.
func (d *Dashboard) SetSize(width, height int) {
	d.viewport.Width = width
	d.viewport.Height = height
}

// Init initializes the dashboard view.
func (d *Dashboard) Init() tea.Cmd {
	return nil
}

// Update handles messages for the dashboard view.
func (d *Dashboard) Update(msg tea.Msg) (*Dashboard, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			d.viewport.ScrollUp(1)
		case "down", "j":
			d.viewport.ScrollDown(1)
		case "pgup", "ctrl+u":
			d.viewport.HalfPageUp()
		case "pgdown", "ctrl+d":
			d.viewport.HalfPageDown()
		case "home":
			d.viewport.GotoTop()
		case "end":
			d.viewport.GotoBottom()
		}
	}

	d.viewport, cmd = d.viewport.Update(msg)

	return d, cmd
}

// View renders the dashboard.
func (d *Dashboard) View() string {
	var content string

	if len(d.workers) == 0 {
		content = d.renderEmpty()
	} else {
		content = d.renderWorkers()
	}

	d.viewport.SetContent(content)

	return d.viewport.View()
}

// renderEmpty renders empty dashboard content.
func (d *Dashboard) renderEmpty() string {
	emptyStyle := lipgloss.NewStyle().
		Align(lipgloss.Center).
		Foreground(lipgloss.Color(styles.ColorMuted))

	return emptyStyle.Render("No workers running\n\nStart some workers to see them here")
}

// renderWorkers renders the worker grid.
func (d *Dashboard) renderWorkers() string {
	rows := make([]string, 0, len(d.workers))

	for _, worker := range d.workers {
		statusStyle := styles.StatusStyle(worker.Healthy)
		nameStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(styles.ColorSecondary))

		var lines []string

		lines = append(lines, fmt.Sprintf("%s (%s)", nameStyle.Render(worker.Name), worker.Type))
		lines = append(lines, "Status: "+statusStyle.Render(worker.Status))

		if worker.Bar != nil {
			lines = append(lines, worker.Bar.View())
		}

		// Add border around each worker
		boxStyle := lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color(styles.ColorBorder)).
			Padding(1).
			Margin(0, 1, 1, 0)

		workerBox := boxStyle.Render(strings.Join(lines, "\n"))
		rows = append(rows, workerBox)
	}

	return strings.Join(rows, "\n")
}
