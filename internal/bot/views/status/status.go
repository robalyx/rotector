package status

import (
	"fmt"
	"strings"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/worker/core"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

const (
	healthyEmoji   = "🟢" // Green circle for healthy workers
	unhealthyEmoji = "🔴" // Red circle for unhealthy workers
	staleEmoji     = "⚫" // Black circle for stale/offline workers
)

// Builder creates the visual layout for the worker status menu.
type Builder struct {
	workerStatuses []core.Status
	titleCaser     cases.Caser
}

// NewBuilder creates a new status builder.
func NewBuilder(s *session.Session) *Builder {
	return &Builder{
		workerStatuses: session.StatusWorkers.Get(s),
		titleCaser:     cases.Title(language.English),
	}
}

// Build creates a Discord message showing worker status information.
func (b *Builder) Build() *discord.MessageUpdateBuilder {
	var content strings.Builder

	// Create header and legend
	content.WriteString("## Worker Statuses\n")
	content.WriteString(fmt.Sprintf("%s Online  %s Unhealthy  %s Offline\n\n", healthyEmoji, unhealthyEmoji, staleEmoji))

	// Group workers by type
	workerGroups := make(map[string][]core.Status)
	for _, status := range b.workerStatuses {
		workerGroups[status.WorkerType] = append(workerGroups[status.WorkerType], status)
	}

	// Add sections for each worker type
	for workerType, workers := range workerGroups {
		// Format worker statuses
		var statusLines []string
		for _, w := range workers {
			shortID := w.WorkerID[:8]
			emoji := b.getStatusEmoji(w)
			statusLines = append(statusLines, fmt.Sprintf("%s `%s` %s (%d%%)",
				emoji, shortID, w.CurrentTask, w.Progress))
		}

		// Add section for this worker type
		content.WriteString(fmt.Sprintf("### %s\n", b.titleCaser.String(workerType)))
		if len(statusLines) > 0 {
			content.WriteString(strings.Join(statusLines, "\n"))
		} else {
			content.WriteString("No workers online")
		}
		content.WriteString("\n\n")
	}

	// Create container with text display and navigation buttons
	container := discord.NewContainer(
		discord.NewTextDisplay(content.String()),
		discord.NewLargeSeparator(),
		discord.NewActionRow(
			discord.NewSecondaryButton("◀️ Back", constants.BackButtonCustomID),
			discord.NewSecondaryButton("🔄 Refresh", constants.RefreshButtonCustomID),
		),
	).WithAccentColor(constants.DefaultContainerColor)

	return discord.NewMessageUpdateBuilder().
		AddComponents(container)
}

// getStatusEmoji returns the appropriate emoji for a worker's status.
func (b *Builder) getStatusEmoji(status core.Status) string {
	// Check if worker is stale first (last seen > StaleThreshold)
	if time.Since(status.LastSeen) > core.StaleThreshold {
		return staleEmoji
	}
	// If worker is not stale, show health status
	if !status.IsHealthy {
		return unhealthyEmoji
	}
	return healthyEmoji
}
