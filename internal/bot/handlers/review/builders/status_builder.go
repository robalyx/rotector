package builders

import (
	"context"
	"fmt"

	"github.com/rotector/rotector/internal/bot/constants"
	"github.com/rotector/rotector/internal/bot/session"
	"github.com/rotector/rotector/internal/common/queue"

	"github.com/disgoorg/disgo/discord"
)

// StatusEmbed builds the embed for the status menu.
type StatusEmbed struct {
	queueManager        *queue.Manager
	userID              uint64
	highPriorityCount   int
	normalPriorityCount int
	lowPriorityCount    int
}

// NewStatusEmbed creates a new StatusEmbed.
func NewStatusEmbed(queueManager *queue.Manager, s *session.Session) *StatusEmbed {
	return &StatusEmbed{
		queueManager:        queueManager,
		userID:              s.GetUint64(constants.SessionKeyQueueUser),
		highPriorityCount:   s.GetInt(constants.SessionKeyQueueHighCount),
		normalPriorityCount: s.GetInt(constants.SessionKeyQueueNormalCount),
		lowPriorityCount:    s.GetInt(constants.SessionKeyQueueLowCount),
	}
}

// Build constructs and returns the discord.Embed.
func (b *StatusEmbed) Build() *discord.MessageUpdateBuilder {
	// Get queue status info
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

	embed := discord.NewEmbedBuilder().
		SetTitle("Recheck Status").
		AddField("Current User", fmt.Sprintf("[%d](https://roblox.com/users/%d/profile)", b.userID, b.userID), true).
		AddField("Status", queueInfo, false).
		AddField("High Priority Queue", fmt.Sprintf("%d items", b.highPriorityCount), true).
		AddField("Normal Priority Queue", fmt.Sprintf("%d items", b.normalPriorityCount), true).
		AddField("Low Priority Queue", fmt.Sprintf("%d items", b.lowPriorityCount), true).
		SetColor(constants.DefaultEmbedColor)

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
