package components

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/robalyx/rotector/internal/tui/styles"
)

// ProgressBar represents a simple progress bar.
type ProgressBar struct {
	current     int64
	total       int64
	message     string
	stepMessage string
	width       int
	title       string
	status      string
	healthy     bool
	lastUpdate  time.Time
}

// NewProgressBar creates a new progress bar component.
func NewProgressBar(total int64, title string) *ProgressBar {
	return &ProgressBar{
		current:    0,
		total:      total,
		width:      50,
		title:      title,
		status:     "Ready",
		healthy:    true,
		lastUpdate: time.Now(),
	}
}

// SetTotal sets the total progress value.
func (pb *ProgressBar) SetTotal(total int64) {
	pb.total = total
}

// SetCurrent sets the current progress value.
func (pb *ProgressBar) SetCurrent(current int64) {
	if current > pb.total {
		current = pb.total
	}

	pb.current = current
	pb.lastUpdate = time.Now()
}

// Increment adds to the current progress value.
func (pb *ProgressBar) Increment(n int64) {
	pb.current += n
	if pb.current > pb.total {
		pb.current = pb.total
	}

	pb.lastUpdate = time.Now()
}

// SetMessage sets the overall progress message.
func (pb *ProgressBar) SetMessage(message string) {
	pb.message = message
}

// SetStepMessage sets the current step message and updates progress.
func (pb *ProgressBar) SetStepMessage(message string, progress int64) {
	pb.stepMessage = message
	pb.SetCurrent(progress)
}

// Reset prepares the bar for a new operation.
func (pb *ProgressBar) Reset() {
	pb.current = 0
	pb.stepMessage = ""
	pb.lastUpdate = time.Now()
}

// GetProgress returns the current progress values.
func (pb *ProgressBar) GetProgress() (current, total int64, message string) {
	return pb.current, pb.total, pb.stepMessage
}

// GetPercentage returns the current percentage.
func (pb *ProgressBar) GetPercentage() float64 {
	if pb.total == 0 {
		return 0.0
	}

	return float64(pb.current) / float64(pb.total) * 100
}

// SetStatus updates the progress bar status.
func (pb *ProgressBar) SetStatus(status string, healthy bool) {
	pb.status = status
	pb.healthy = healthy
	pb.lastUpdate = time.Now()
}

// SetSize sets the progress bar width.
func (pb *ProgressBar) SetSize(width int) {
	if width > 20 {
		pb.width = width - 10
	} else {
		pb.width = 10
	}
}

// View renders the progress bar.
func (pb *ProgressBar) View() string {
	return pb.render()
}

// render renders the progress bar.
func (pb *ProgressBar) render() string {
	percent := pb.GetPercentage()

	filled := int(percent / 100.0 * float64(pb.width))
	if filled > pb.width {
		filled = pb.width
	}

	var bar strings.Builder
	for i := range pb.width {
		bar.WriteString(styles.ProgressBarChar(i < filled))
	}

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(styles.ColorPrimary))
	if !pb.healthy {
		titleStyle = titleStyle.Foreground(lipgloss.Color(styles.ColorError))
	}

	var lines []string

	lines = append(lines, fmt.Sprintf("%s - %s", titleStyle.Render(pb.title), pb.status))
	lines = append(lines, fmt.Sprintf("[%s] %.1f%%", bar.String(), percent))

	if pb.stepMessage != "" {
		lines = append(lines, "Step: "+pb.stepMessage)
	}

	return strings.Join(lines, "\n")
}
