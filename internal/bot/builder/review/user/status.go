package user

import (
	"fmt"
	"strconv"

	"github.com/rotector/rotector/internal/bot/constants"
	"github.com/rotector/rotector/internal/bot/core/session"
	"github.com/rotector/rotector/internal/bot/utils"
	"github.com/rotector/rotector/internal/common/queue"
	"github.com/rotector/rotector/internal/common/storage/database/types"

	"github.com/disgoorg/disgo/discord"
)

// StatusBuilder creates the visual layout for viewing queue status information.
// It combines queue position, status, and queue lengths into a Discord embed.
type StatusBuilder struct {
	settings            *types.UserSetting
	queueManager        *queue.Manager
	userID              uint64
	status              string
	priority            string
	position            int
	highPriorityCount   int
	normalPriorityCount int
	lowPriorityCount    int
}

// NewStatusBuilder creates a new status builder.
func NewStatusBuilder(queueManager *queue.Manager, s *session.Session) *StatusBuilder {
	var settings *types.UserSetting
	s.GetInterface(constants.SessionKeyUserSettings, &settings)

	return &StatusBuilder{
		settings:            settings,
		queueManager:        queueManager,
		userID:              s.GetUint64(constants.SessionKeyQueueUser),
		status:              s.GetString(constants.SessionKeyQueueStatus),
		priority:            s.GetString(constants.SessionKeyQueuePriority),
		position:            s.GetInt(constants.SessionKeyQueuePosition),
		highPriorityCount:   s.GetInt(constants.SessionKeyQueueHighCount),
		normalPriorityCount: s.GetInt(constants.SessionKeyQueueNormalCount),
		lowPriorityCount:    s.GetInt(constants.SessionKeyQueueLowCount),
	}
}

// Build creates a Discord message showing:
// - Current user's queue status and position
// - Number of items in each priority queue
// - Refresh and abort buttons for queue management.
func (b *StatusBuilder) Build() *discord.MessageUpdateBuilder {
	// Create embed with queue information
	embed := discord.NewEmbedBuilder().
		SetTitle("Recheck Status")

	// Format user field based on review mode
	if b.settings.ReviewMode == types.TrainingReviewMode {
		embed.AddField("Current User", utils.CensorString(strconv.FormatUint(b.userID, 10), true), true)
	} else {
		embed.AddField("Current User", fmt.Sprintf(
			"[%s](https://roblox.com/users/%d/profile)",
			utils.CensorString(strconv.FormatUint(b.userID, 10), b.settings.StreamerMode),
			b.userID,
		), true)
	}

	embed.AddField("Status", fmt.Sprintf("%s (Position: %d in %s queue)", b.status, b.position, b.priority), false).
		AddField("High Priority Queue", fmt.Sprintf("%d items", b.highPriorityCount), true).
		AddField("Normal Priority Queue", fmt.Sprintf("%d items", b.normalPriorityCount), true).
		AddField("Low Priority Queue", fmt.Sprintf("%d items", b.lowPriorityCount), true).
		SetColor(utils.GetMessageEmbedColor(b.settings.StreamerMode))

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
