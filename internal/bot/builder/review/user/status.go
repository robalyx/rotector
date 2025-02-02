package user

import (
	"fmt"
	"strconv"

	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/bot/utils"
	"github.com/robalyx/rotector/internal/common/queue"
	"github.com/robalyx/rotector/internal/common/storage/database/types/enum"

	"github.com/disgoorg/disgo/discord"
)

// StatusBuilder creates the visual layout for viewing queue status information.
// It combines queue position, status, and queue lengths into a Discord embed.
type StatusBuilder struct {
	queueManager        *queue.Manager
	userID              uint64
	status              queue.Status
	priority            queue.Priority
	position            int
	highPriorityCount   int
	normalPriorityCount int
	lowPriorityCount    int
	privacyMode         bool
}

// NewStatusBuilder creates a new status builder.
func NewStatusBuilder(queueManager *queue.Manager, s *session.Session) *StatusBuilder {
	return &StatusBuilder{
		queueManager:        queueManager,
		userID:              session.QueueUser.Get(s),
		status:              session.QueueStatus.Get(s),
		priority:            session.QueuePriority.Get(s),
		position:            session.QueuePosition.Get(s),
		highPriorityCount:   session.QueueHighCount.Get(s),
		normalPriorityCount: session.QueueNormalCount.Get(s),
		lowPriorityCount:    session.QueueLowCount.Get(s),
		privacyMode:         session.UserReviewMode.Get(s) == enum.ReviewModeTraining || session.UserStreamerMode.Get(s),
	}
}

// Build creates a Discord message showing the user's queue status.
func (b *StatusBuilder) Build() *discord.MessageUpdateBuilder {
	// Create embed with queue information
	embed := discord.NewEmbedBuilder().
		SetTitle("Recheck Status")

	// Format user field based on review mode
	userID := utils.CensorString(strconv.FormatUint(b.userID, 10), b.privacyMode)
	if b.privacyMode {
		embed.AddField("Current User", userID, true)
	} else {
		embed.AddField("Current User", fmt.Sprintf(
			"[%s](https://roblox.com/users/%d/profile)",
			userID,
			b.userID,
		), true)
	}

	embed.AddField("Status", fmt.Sprintf("%s (Position: %d in %s queue)", b.status, b.position, b.priority), false).
		AddField("High Priority Queue", fmt.Sprintf("%d items", b.highPriorityCount), true).
		AddField("Normal Priority Queue", fmt.Sprintf("%d items", b.normalPriorityCount), true).
		AddField("Low Priority Queue", fmt.Sprintf("%d items", b.lowPriorityCount), true).
		SetColor(utils.GetMessageEmbedColor(b.privacyMode))

	// Add queue management buttons
	components := []discord.ContainerComponent{
		discord.NewActionRow(
			discord.NewSecondaryButton("🔄 Refresh", constants.RefreshButtonCustomID),
			discord.NewDangerButton("Abort", constants.AbortButtonCustomID),
		),
	}

	return discord.NewMessageUpdateBuilder().
		SetEmbeds(embed.Build()).
		AddContainerComponents(components...)
}
