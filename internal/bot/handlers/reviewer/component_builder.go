package reviewer

import (
	"github.com/disgoorg/disgo/discord"
	"github.com/rotector/rotector/internal/common/database"
)

// ComponentBuilder builds the components for messages.
type ComponentBuilder struct {
	components []discord.ContainerComponent
}

// NewComponentBuilder creates a new ComponentBuilder.
func NewComponentBuilder() *ComponentBuilder {
	return &ComponentBuilder{}
}

// AddSortSelectMenu adds the sort select menu to the components.
func (b *ComponentBuilder) AddSortSelectMenu(sortBy string) *ComponentBuilder {
	b.components = append(b.components, discord.NewActionRow(
		discord.NewStringSelectMenu(ReviewProcessPrefix+SortSelectMenuCustomID, "Sorting",
			discord.NewStringSelectMenuOption("Selected by random", database.SortByRandom).
				WithDefault(sortBy == database.SortByRandom).
				WithEmoji(discord.ComponentEmoji{Name: "🔀"}),
			discord.NewStringSelectMenuOption("Selected by confidence", database.SortByConfidence).
				WithDefault(sortBy == database.SortByConfidence).
				WithEmoji(discord.ComponentEmoji{Name: "🔮"}),
			discord.NewStringSelectMenuOption("Selected by last updated time", database.SortByLastUpdated).
				WithDefault(sortBy == database.SortByLastUpdated).
				WithEmoji(discord.ComponentEmoji{Name: "📅"}),
		),
	))
	return b
}

// AddActionSelectMenu adds the action select menu to the components.
func (b *ComponentBuilder) AddActionSelectMenu() *ComponentBuilder {
	b.components = append(b.components, discord.NewActionRow(
		discord.NewStringSelectMenu(ReviewProcessPrefix+ActionSelectMenuCustomID, "Actions",
			discord.NewStringSelectMenuOption("Ban with reason", BanWithReasonButtonCustomID),
			discord.NewStringSelectMenuOption("Open outfit viewer", OpenOutfitsMenuButtonCustomID),
			discord.NewStringSelectMenuOption("Open friends viewer", OpenFriendsMenuButtonCustomID),
			discord.NewStringSelectMenuOption("Open group viewer", OpenGroupViewerButtonCustomID),
		),
	))
	return b
}

// AddReviewButtons adds the review buttons to the components.
func (b *ComponentBuilder) AddReviewButtons() *ComponentBuilder {
	b.components = append(b.components, discord.NewActionRow(
		discord.NewSecondaryButton("◀️", ReviewProcessPrefix+BackButtonCustomID),
		discord.NewDangerButton("Ban", ReviewProcessPrefix+BanButtonCustomID),
		discord.NewSuccessButton("Clear", ReviewProcessPrefix+ClearButtonCustomID),
		discord.NewSecondaryButton("Skip", ReviewProcessPrefix+SkipButtonCustomID),
	))
	return b
}

// AddOutfitsMenuButtons adds the outfit viewer buttons to the components.
func (b *ComponentBuilder) AddOutfitsMenuButtons(page, totalPages int) *ComponentBuilder {
	b.components = append(b.components, discord.NewActionRow(
		discord.NewSecondaryButton("◀️", OutfitsMenuPrefix+string(ViewerBackToReview)),
		discord.NewSecondaryButton("⏮️", OutfitsMenuPrefix+string(ViewerFirstPage)).WithDisabled(page == 0),
		discord.NewSecondaryButton("◀️", OutfitsMenuPrefix+string(ViewerPrevPage)).WithDisabled(page == 0),
		discord.NewSecondaryButton("▶️", OutfitsMenuPrefix+string(ViewerNextPage)).WithDisabled(page == totalPages-1),
		discord.NewSecondaryButton("⏭️", OutfitsMenuPrefix+string(ViewerLastPage)).WithDisabled(page == totalPages-1),
	))
	return b
}

// AddFriendsMenuButtons adds the friends viewer buttons to the components.
func (b *ComponentBuilder) AddFriendsMenuButtons(page, totalPages int) *ComponentBuilder {
	b.components = append(b.components, discord.NewActionRow(
		discord.NewSecondaryButton("◀️", FriendsMenuPrefix+string(ViewerBackToReview)),
		discord.NewSecondaryButton("⏮️", FriendsMenuPrefix+string(ViewerFirstPage)).WithDisabled(page == 0),
		discord.NewSecondaryButton("◀️", FriendsMenuPrefix+string(ViewerPrevPage)).WithDisabled(page == 0),
		discord.NewSecondaryButton("▶️", FriendsMenuPrefix+string(ViewerNextPage)).WithDisabled(page == totalPages-1),
		discord.NewSecondaryButton("⏭️", FriendsMenuPrefix+string(ViewerLastPage)).WithDisabled(page == totalPages-1),
	))
	return b
}

// Build returns the built components.
func (b *ComponentBuilder) Build() []discord.ContainerComponent {
	return b.components
}
