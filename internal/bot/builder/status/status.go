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
	embed := b.buildWorkerStatusEmbed()

	return discord.NewMessageUpdateBuilder().
		SetEmbeds(embed).
		AddActionRow(
			discord.NewSecondaryButton("◀️", string(constants.BackButtonCustomID)),
			discord.NewSecondaryButton("🔄 Refresh", string(constants.RefreshButtonCustomID)),
		)
}

// buildWorkerStatusEmbed creates the worker status monitoring embed.
func (b *Builder) buildWorkerStatusEmbed() discord.Embed {
	embed := discord.NewEmbedBuilder().
		SetTitle("Worker Statuses").
		SetDescription(fmt.Sprintf("%s Online  %s Unhealthy  %s Offline", healthyEmoji, unhealthyEmoji, staleEmoji)).
		SetColor(constants.DefaultEmbedColor)

	// Group workers by type and subtype
	workerGroups := b.groupWorkers()

	// Add fields for each worker type
	for workerType, subtypes := range workerGroups {
		for subType, workers := range subtypes {
			// Format worker statuses
			var statusLines []string
			for _, w := range workers {
				shortID := w.WorkerID[:8]
				emoji := b.getStatusEmoji(w)
				statusLines = append(statusLines, fmt.Sprintf("%s `%s` %s (%d%%)",
					emoji, shortID, w.CurrentTask, w.Progress))
			}

			// Add field for this worker type
			fieldName := fmt.Sprintf("%s %s",
				b.titleCaser.String(workerType),
				b.titleCaser.String(subType),
			)
			fieldValue := "No workers online"
			if len(statusLines) > 0 {
				fieldValue = strings.Join(statusLines, "\n")
			}
			embed.AddField(fieldName, fieldValue, false)
		}
	}

	return embed.Build()
}

// groupWorkers organizes workers by type and subtype.
func (b *Builder) groupWorkers() map[string]map[string][]core.Status {
	groups := make(map[string]map[string][]core.Status)

	for _, status := range b.workerStatuses {
		// Initialize maps
		if _, ok := groups[status.WorkerType]; !ok {
			groups[status.WorkerType] = make(map[string][]core.Status)
		}

		// Add worker to appropriate group
		groups[status.WorkerType][status.SubType] = append(
			groups[status.WorkerType][status.SubType],
			status,
		)
	}

	return groups
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
