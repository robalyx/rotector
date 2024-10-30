package builders

import (
	"fmt"

	"github.com/disgoorg/disgo/discord"
	"github.com/rotector/rotector/internal/bot/constants"
)

// QueueBuilder builds the queue management menu.
type QueueBuilder struct {
	highPriorityCount   int
	normalPriorityCount int
	lowPriorityCount    int
}

// NewQueueBuilder creates a new QueueBuilder.
func NewQueueBuilder(highCount, normalCount, lowCount int) *QueueBuilder {
	return &QueueBuilder{
		highPriorityCount:   highCount,
		normalPriorityCount: normalCount,
		lowPriorityCount:    lowCount,
	}
}

// Build constructs the queue management menu.
func (b *QueueBuilder) Build() *discord.MessageUpdateBuilder {
	embed := discord.NewEmbedBuilder().
		SetTitle("Queue Manager").
		AddField("High Priority Queue", fmt.Sprintf("%d items", b.highPriorityCount), true).
		AddField("Normal Priority Queue", fmt.Sprintf("%d items", b.normalPriorityCount), true).
		AddField("Low Priority Queue", fmt.Sprintf("%d items", b.lowPriorityCount), true).
		SetColor(constants.DefaultEmbedColor).
		Build()

	components := []discord.ContainerComponent{
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
		discord.NewActionRow(
			discord.NewSecondaryButton("◀️", string(constants.BackButtonCustomID)),
			discord.NewSecondaryButton("🔄", string(constants.RefreshButtonCustomID)),
		),
	}

	return discord.NewMessageUpdateBuilder().
		SetEmbeds(embed).
		AddContainerComponents(components...)
}
