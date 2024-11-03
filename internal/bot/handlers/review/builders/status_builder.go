package builders

import (
	"context"
	"fmt"

	"github.com/rotector/rotector/internal/bot/constants"
	"github.com/rotector/rotector/internal/bot/session"
	"github.com/rotector/rotector/internal/common/queue"

	"github.com/disgoorg/disgo/discord"
)

// StatusEmbed creates the visual layout for viewing queue status information.
// It combines queue position, status, and queue lengths into a Discord embed.
type StatusEmbed struct {
	queueManager        *queue.Manager
	userID              uint64
	highPriorityCount   int
	normalPriorityCount int
	lowPriorityCount    int
}

// NewStatusEmbed loads queue information from the session state to create
// a new embed builder.
func NewStatusEmbed(queueManager *queue.Manager, s *session.Session) *StatusEmbed {
	return &StatusEmbed{
		queueManager:        queueManager,
		userID:              s.GetUint64(constants.SessionKeyQueueUser),
		highPriorityCount:   s.GetInt(constants.SessionKeyQueueHighCount),
		normalPriorityCount: s.GetInt(constants.SessionKeyQueueNormalCount),
		lowPriorityCount:    s.GetInt(constants.SessionKeyQueueLowCount),
	}
}

// Build creates a Discord message showing:
// - Current user's queue status and position
// - Number of items in each priority queue
// - Refresh and abort buttons for queue management.
func (b *StatusEmbed) Build() *discord.MessageUpdateBuilder {
	// Get current queue status and position
	queueInfo := "Not in queue"
	status, priority, position, err := b.queueManager.GetQueueInfo(context.Background(), b.userID)
	if err == nil && status != "" {
		if position > 0 {
			queueInfo = fmt.Sprintf("%s (Position: %d in %s queue)",
				status, position, priority)
		} else {
			queueInfo = status
		}
	}

	// Create embed with queue information
	embed := discord.NewEmbedBuilder().
		SetTitle("Recheck Status").
		AddField("Current User", fmt.Sprintf("[%d](https://roblox.com/users/%d/profile)", b.userID, b.userID), true).
		AddField("Status", queueInfo, false).
		AddField("High Priority Queue", fmt.Sprintf("%d items", b.highPriorityCount), true).
		AddField("Normal Priority Queue", fmt.Sprintf("%d items", b.normalPriorityCount), true).
		AddField("Low Priority Queue", fmt.Sprintf("%d items", b.lowPriorityCount), true).
		SetColor(constants.DefaultEmbedColor)

	// Add queue management buttons
	components := []discord.ContainerComponent{
		discord.NewActionRow(
			discord.NewSecondaryButton("🔄", constants.RefreshButtonCustomID),
			discord.NewDangerButton("Abort", constants.AbortButtonCustomID),
		),
	}

	return discord.NewMessageUpdateBuilder().
		SetEmbeds(embed.Build()).
		AddContainerComponents(components...)
}
