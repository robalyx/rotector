package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/robalyx/rotector/internal/tui/components"
	"github.com/robalyx/rotector/internal/tui/views"
)

const RefreshInterval = 2 * time.Second

const (
	DashboardView ViewType = iota
	WorkerView
	HelpView
)

// TickMsg is sent every refresh interval.
type TickMsg struct{}

// ViewType represents different TUI views.
type ViewType int

// Model represents the main TUI model.
type Model struct {
	ctx          context.Context
	currentView  ViewType
	activeWorker int
	workers      []*WorkerInfo
	logDir       string
	width        int
	height       int

	// Views
	dashboard *views.Dashboard
	worker    *views.Worker
	help      *views.Help

	// Components
	tabs *components.Tabs

	// Key bindings
	keys KeyMap

	// Quit flag
	quitting bool
}

// KeyMap defines key bindings for the TUI.
type KeyMap struct {
	Quit       key.Binding
	Help       key.Binding
	Dashboard  key.Binding
	Worker     key.Binding
	NextTab    key.Binding
	PrevTab    key.Binding
	ScrollUp   key.Binding
	ScrollDown key.Binding
	PageUp     key.Binding
	PageDown   key.Binding
}

// DefaultKeyMap returns default key bindings.
func DefaultKeyMap() KeyMap {
	return KeyMap{
		Quit: key.NewBinding(
			key.WithKeys("q", "ctrl+c"),
			key.WithHelp("q", "quit"),
		),
		Help: key.NewBinding(
			key.WithKeys("?", "h"),
			key.WithHelp("?", "help"),
		),
		Dashboard: key.NewBinding(
			key.WithKeys("d"),
			key.WithHelp("d", "dashboard"),
		),
		Worker: key.NewBinding(
			key.WithKeys("l"),
			key.WithHelp("l", "logs"),
		),
		NextTab: key.NewBinding(
			key.WithKeys("tab", "right"),
			key.WithHelp("tab", "next worker"),
		),
		PrevTab: key.NewBinding(
			key.WithKeys("shift+tab", "left"),
			key.WithHelp("shift+tab", "prev worker"),
		),
		ScrollUp: key.NewBinding(
			key.WithKeys("up", "k"),
			key.WithHelp("↑/k", "scroll up"),
		),
		ScrollDown: key.NewBinding(
			key.WithKeys("down", "j"),
			key.WithHelp("↓/j", "scroll down"),
		),
		PageUp: key.NewBinding(
			key.WithKeys("pgup", "ctrl+u"),
			key.WithHelp("pgup", "page up"),
		),
		PageDown: key.NewBinding(
			key.WithKeys("pgdown", "ctrl+d"),
			key.WithHelp("pgdown", "page down"),
		),
	}
}

// NewModel creates a new TUI model.
func NewModel(ctx context.Context, logDir string) *Model {
	m := &Model{
		ctx:         ctx,
		currentView: DashboardView,
		workers:     make([]*WorkerInfo, 0),
		logDir:      logDir,
		keys:        DefaultKeyMap(),
		width:       80,
		height:      24,
	}

	// Initialize views
	m.dashboard = views.NewDashboard()
	m.worker = views.NewWorker()
	m.worker.SetContext(ctx)
	m.help = views.NewHelp()

	// Initialize components
	m.tabs = components.NewTabs()

	return m
}

// AddWorker adds a new worker to track.
func (m *Model) AddWorker(id int, workerType, name, logPath string, bar *components.ProgressBar) {
	worker := &WorkerInfo{
		ID:          id,
		Name:        name,
		Type:        workerType,
		LogPath:     logPath,
		Bar:         bar,
		Status:      "Starting",
		Healthy:     true,
		LastUpdated: time.Now(),
	}

	if bar != nil {
		bar.SetSize(m.width)
	}

	m.workers = append(m.workers, worker)

	// Update tabs
	tabNames := make([]string, len(m.workers))
	for i, w := range m.workers {
		tabNames[i] = w.Name
	}

	m.tabs.SetTabs(tabNames)

	// Update views
	workerData := make([]views.WorkerData, len(m.workers))
	for i, w := range m.workers {
		workerData[i] = views.WorkerData{
			Name:    w.Name,
			Type:    w.Type,
			Status:  w.Status,
			Healthy: w.Healthy,
			Bar:     w.Bar,
		}
	}

	m.dashboard.SetWorkers(workerData)

	if len(m.workers) == 1 {
		m.worker.SetWorker(worker.Name, worker.Type, worker.Status, worker.LogPath, worker.Healthy, worker.Bar)
	}
}

// UpdateWorkerStatus updates a worker's status.
func (m *Model) UpdateWorkerStatus(id int, status string, healthy bool) {
	for _, w := range m.workers {
		if w.ID == id {
			w.Status = status
			w.Healthy = healthy
			w.LastUpdated = time.Now()

			break
		}
	}

	// Update views
	workerData := make([]views.WorkerData, len(m.workers))
	for i, w := range m.workers {
		workerData[i] = views.WorkerData{
			Name:    w.Name,
			Type:    w.Type,
			Status:  w.Status,
			Healthy: w.Healthy,
			Bar:     w.Bar,
		}
	}

	m.dashboard.SetWorkers(workerData)

	if m.activeWorker < len(m.workers) {
		w := m.workers[m.activeWorker]
		m.worker.SetWorker(w.Name, w.Type, w.Status, w.LogPath, w.Healthy, w.Bar)
	}
}

// Init initializes the model.
func (m *Model) Init() tea.Cmd {
	return tea.Batch(
		m.worker.Init(),
		tea.Tick(RefreshInterval, func(time.Time) tea.Msg {
			return TickMsg{}
		}),
	)
}

// Update handles messages.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var (
		cmd  tea.Cmd
		cmds []tea.Cmd
	)

	switch msg := msg.(type) {
	case TickMsg:
		// Refresh dashboard data and worker logs, then schedule next tick
		switch m.currentView {
		case DashboardView:
			m.refreshDashboard()
		case WorkerView:
			m.worker.Refresh()
		case HelpView:
			// No refresh needed for help view
		}

		return m, tea.Tick(RefreshInterval, func(time.Time) tea.Msg {
			return TickMsg{}
		})

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		// Update all views and components with new size
		headerHeight := calculateHeaderHeight(true, m.currentView == WorkerView && len(m.workers) > 0)
		contentHeight := calculateContentHeight(m.height, headerHeight)

		m.dashboard.SetSize(m.width, contentHeight)
		m.worker.SetSize(m.width, contentHeight)
		m.tabs.SetWidth(m.width)

		// Update progress bars
		for _, worker := range m.workers {
			if worker.Bar != nil {
				worker.Bar.SetSize(m.width)
			}
		}

	case tea.KeyMsg:
		switch {
		case key.Matches(msg, m.keys.Quit):
			m.quitting = true
			return m, tea.Quit

		case key.Matches(msg, m.keys.Help):
			m.currentView = HelpView

		case key.Matches(msg, m.keys.Dashboard):
			m.currentView = DashboardView

		case key.Matches(msg, m.keys.Worker):
			m.currentView = WorkerView

		case key.Matches(msg, m.keys.NextTab):
			if len(m.workers) > 0 {
				m.activeWorker = (m.activeWorker + 1) % len(m.workers)
				m.tabs.SetActive(m.activeWorker)
				w := m.workers[m.activeWorker]
				m.worker.SetWorker(w.Name, w.Type, w.Status, w.LogPath, w.Healthy, w.Bar)
			}

		case key.Matches(msg, m.keys.PrevTab):
			if len(m.workers) > 0 {
				m.activeWorker = (m.activeWorker - 1 + len(m.workers)) % len(m.workers)
				m.tabs.SetActive(m.activeWorker)
				w := m.workers[m.activeWorker]
				m.worker.SetWorker(w.Name, w.Type, w.Status, w.LogPath, w.Healthy, w.Bar)
			}
		}
	}

	// Update current view
	switch m.currentView {
	case DashboardView:
		_, cmd = m.dashboard.Update(msg)
		cmds = append(cmds, cmd)
	case WorkerView:
		_, cmd = m.worker.Update(msg)
		cmds = append(cmds, cmd)
	case HelpView:
		_, cmd = m.help.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

// View renders the current view.
func (m *Model) View() string {
	if m.quitting {
		return "Goodbye!\n"
	}

	if m.width == 0 || m.height == 0 {
		return "Loading..."
	}

	// Check if terminal is too small
	if isTerminalTooSmall(m.width) {
		return fmt.Sprintf("Terminal too small!\nMinimum width required: %d columns\nCurrent size: %dx%d\n\nPlease resize your terminal window.",
			MinTerminalWidth, m.width, m.height)
	}

	// Render header with navigation and tabs
	header := m.renderHeader()

	// Generate content for current view
	var content string

	switch m.currentView {
	case DashboardView:
		content = m.dashboard.View()
	case WorkerView:
		content = m.worker.View()
	case HelpView:
		content = m.help.View()
	}

	// Combine header and content
	return lipgloss.JoinVertical(lipgloss.Left, header, content)
}

// renderHeader renders the header with navigation and tabs.
func (m *Model) renderHeader() string {
	var viewName string

	switch m.currentView {
	case DashboardView:
		viewName = "Dashboard"
	case WorkerView:
		viewName = "Worker Logs"
	case HelpView:
		viewName = "Help"
	}

	titleText := "Rotector Worker Monitor - " + viewName
	shortcutsText := "d: dashboard • l: logs • ?: help • q: quit"

	// Truncate if needed
	maxLineWidth := m.width - 8
	if !shouldShowDetails(m.width) {
		shortcutsText = "d/l/?: nav • q: quit"
	}

	if len(titleText) > maxLineWidth/2 {
		titleText = truncateText(titleText, maxLineWidth/2)
	}

	if len(shortcutsText) > maxLineWidth/2 {
		shortcutsText = truncateText(shortcutsText, maxLineWidth/2)
	}

	// Calculate spacing
	spacePadding := maxLineWidth - len(titleText) - len(shortcutsText)
	if spacePadding < 1 {
		spacePadding = 1
	}

	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("86")).Render(titleText)
	shortcuts := lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Render(shortcutsText)

	headerLine := lipgloss.JoinHorizontal(lipgloss.Left, title, strings.Repeat(" ", spacePadding), shortcuts)

	headerStyle := lipgloss.NewStyle().
		BorderStyle(lipgloss.NormalBorder()).
		BorderBottom(true).
		BorderForeground(lipgloss.Color("238")).
		Padding(0, 2)

	header := headerStyle.Width(m.width - 4).Render(headerLine)

	// Add tabs for worker view
	if m.currentView == WorkerView && len(m.workers) > 0 {
		return lipgloss.JoinVertical(lipgloss.Left, header, m.tabs.View())
	}

	return header
}

// refreshDashboard updates dashboard data.
func (m *Model) refreshDashboard() {
	workerData := make([]views.WorkerData, len(m.workers))
	for i, w := range m.workers {
		workerData[i] = views.WorkerData{
			Name:    w.Name,
			Type:    w.Type,
			Status:  w.Status,
			Healthy: w.Healthy,
			Bar:     w.Bar,
		}
	}

	m.dashboard.SetWorkers(workerData)
}
