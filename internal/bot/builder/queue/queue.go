package queue

import (
	"fmt"

	"github.com/disgoorg/disgo/discord"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/session"
)

// Builder creates the visual layout for managing queue operations.
// It shows current queue lengths and provides options for adding users
// to different priority queues.
type Builder struct {
	highPriorityCount   int
	normalPriorityCount int
	lowPriorityCount    int
}

// NewBuilder creates a new queue embed.
func NewBuilder(s *session.Session) *Builder {
	return &Builder{
		highPriorityCount:   session.QueueHighCount.Get(s),
		normalPriorityCount: session.QueueNormalCount.Get(s),
		lowPriorityCount:    session.QueueLowCount.Get(s),
	}
}

// Build creates a Discord message showing the priority queues.
func (b *Builder) Build() *discord.MessageUpdateBuilder {
	// Create embed showing queue lengths
	embed := discord.NewEmbedBuilder().
		SetTitle("User Queue Manager").
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
					WithEmoji(discord.ComponentEmoji{Name: "🔴"}).
					WithDescription("Add user to high priority queue"),
				discord.NewStringSelectMenuOption("Add to Normal Priority", constants.QueueNormalPriorityCustomID).
					WithEmoji(discord.ComponentEmoji{Name: "🟡"}).
					WithDescription("Add user to normal priority queue"),
				discord.NewStringSelectMenuOption("Add to Low Priority", constants.QueueLowPriorityCustomID).
					WithEmoji(discord.ComponentEmoji{Name: "🟢"}).
					WithDescription("Add user to low priority queue"),
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
