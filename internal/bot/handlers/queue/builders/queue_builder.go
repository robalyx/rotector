package builders

import (
	"fmt"

	"github.com/disgoorg/disgo/discord"
	"github.com/rotector/rotector/internal/bot/constants"
)

// QueueBuilder creates the visual layout for managing queue operations.
// It shows current queue lengths and provides options for adding users
// to different priority queues.
type QueueBuilder struct {
	highPriorityCount   int
	normalPriorityCount int
	lowPriorityCount    int
}

// NewQueueBuilder loads current queue lengths to create a new builder.
// The counts are used to show queue status in the interface.
func NewQueueBuilder(highCount, normalCount, lowCount int) *QueueBuilder {
	return &QueueBuilder{
		highPriorityCount:   highCount,
		normalPriorityCount: normalCount,
		lowPriorityCount:    lowCount,
	}
}

// Build creates a Discord message showing:
// - Current number of items in each priority queue
// - Select menu for adding users to different priority queues
// - Navigation and refresh buttons.
func (b *QueueBuilder) Build() *discord.MessageUpdateBuilder {
	// Create embed showing queue lengths
	embed := discord.NewEmbedBuilder().
		SetTitle("Queue Manager").
		AddField("High Priority Queue", fmt.Sprintf("%d items", b.highPriorityCount), true).
		AddField("Normal Priority Queue", fmt.Sprintf("%d items", b.normalPriorityCount), true).
		AddField("Low Priority Queue", fmt.Sprintf("%d items", b.lowPriorityCount), true).
		SetColor(constants.DefaultEmbedColor).
		Build()

	// Add queue management components
	components := []discord.ContainerComponent{
		// Priority selection menu
		discord.NewActionRow(
			discord.NewStringSelectMenu(constants.ActionSelectMenuCustomID, "Add to queue",
				discord.NewStringSelectMenuOption("Add to High Priority", constants.QueueHighPriorityCustomID).
					WithEmoji(discord.ComponentEmoji{Name: "🔴"}),
				discord.NewStringSelectMenuOption("Add to Normal Priority", constants.QueueNormalPriorityCustomID).
					WithEmoji(discord.ComponentEmoji{Name: "🟡"}),
				discord.NewStringSelectMenuOption("Add to Low Priority", constants.QueueLowPriorityCustomID).
					WithEmoji(discord.ComponentEmoji{Name: "🟢"}),
			),
		),
		// Navigation and refresh buttons
		discord.NewActionRow(
			discord.NewSecondaryButton("◀️", string(constants.BackButtonCustomID)),
			discord.NewSecondaryButton("🔄", string(constants.RefreshButtonCustomID)),
		),
	}

	return discord.NewMessageUpdateBuilder().
		SetEmbeds(embed).
		AddContainerComponents(components...)
}
