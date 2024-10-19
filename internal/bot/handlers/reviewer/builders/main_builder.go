package builders

import (
	"strconv"

	"github.com/disgoorg/disgo/discord"
	"github.com/rotector/rotector/internal/bot/handlers/reviewer/constants"
)

// MainBuilder is the builder for the main menu.
type MainBuilder struct {
	pendingCount int
	flaggedCount int
}

// NewMainBuilder creates a new MainBuilder.
func NewMainBuilder(pendingCount, flaggedCount int) *MainBuilder {
	return &MainBuilder{
		pendingCount: pendingCount,
		flaggedCount: flaggedCount,
	}
}

// Build builds the main menu.
func (b *MainBuilder) Build() *discord.MessageUpdateBuilder {
	embed := discord.NewEmbedBuilder().
		AddField("Pending Users", strconv.Itoa(b.pendingCount), true).
		AddField("Flagged Users", strconv.Itoa(b.flaggedCount), true).
		SetColor(0x312D2B).
		Build()

	components := []discord.ContainerComponent{
		discord.NewActionRow(
			discord.NewStringSelectMenu(constants.ActionSelectMenuCustomID, "Select an action",
				discord.NewStringSelectMenuOption("Start reviewing flagged players", constants.StartReviewCustomID),
			),
		),
	}

	return discord.NewMessageUpdateBuilder().
		SetEmbeds(embed).
		AddContainerComponents(components...)
}
