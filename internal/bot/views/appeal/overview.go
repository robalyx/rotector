package appeal

import (
	"fmt"
	"strconv"

	"github.com/disgoorg/disgo/discord"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/bot/utils"
	"github.com/robalyx/rotector/internal/database/types"
	"github.com/robalyx/rotector/internal/database/types/enum"
)

// OverviewBuilder creates the visual layout for the appeal overview interface.
type OverviewBuilder struct {
	appeals      []*types.FullAppeal
	sortBy       enum.AppealSortBy
	statusFilter enum.AppealStatus
	hasNextPage  bool
	hasPrevPage  bool
	isReviewer   bool
	streamerMode bool
}

// NewOverviewBuilder creates a new overview builder.
func NewOverviewBuilder(s *session.Session) *OverviewBuilder {
	return &OverviewBuilder{
		appeals:      session.AppealList.Get(s),
		sortBy:       session.UserAppealDefaultSort.Get(s),
		statusFilter: session.UserAppealStatusFilter.Get(s),
		hasNextPage:  session.PaginationHasNextPage.Get(s),
		hasPrevPage:  session.PaginationHasPrevPage.Get(s),
		isReviewer:   s.BotSettings().IsReviewer(session.UserID.Get(s)),
		streamerMode: session.UserStreamerMode.Get(s),
	}
}

// Build creates a Discord message showing the appeals list and controls.
func (b *OverviewBuilder) Build() *discord.MessageUpdateBuilder {
	return discord.NewMessageUpdateBuilder().
		SetEmbeds(
			b.buildInfoEmbed().Build(),
			b.buildListEmbed().Build(),
		).
		AddContainerComponents(b.buildComponents()...)
}

// buildInfoEmbed creates the informational embed on the appeal system.
func (b *OverviewBuilder) buildInfoEmbed() *discord.EmbedBuilder {
	return discord.NewEmbedBuilder().
		SetTitle("Appeal System").
		SetDescription(
			"Welcome to the appeal system. Here you can:\n\n" +
				"- Submit appeals for flagged or confirmed users\n" +
				"- Track the status of your appeals\n" +
				"- Request data deletion under privacy laws\n" +
				"- Communicate with moderators about your case").
		SetColor(utils.GetMessageEmbedColor(b.streamerMode))
}

// buildListEmbed creates the embed showing the list of appeals.
func (b *OverviewBuilder) buildListEmbed() *discord.EmbedBuilder {
	embed := discord.NewEmbedBuilder().
		SetTitle("Active Appeals").
		SetColor(utils.GetMessageEmbedColor(b.streamerMode))

	if len(b.appeals) == 0 {
		embed.SetDescription("No appeals found.")
		return embed
	}

	// Add appeal entries
	for _, appeal := range b.appeals {
		fieldName, fieldValue := b.formatAppealField(appeal)
		embed.AddField(fieldName, fieldValue, false)
	}

	// Add sequence count to footer
	if len(b.appeals) > 0 {
		firstAppeal := b.appeals[0]
		lastAppeal := b.appeals[len(b.appeals)-1]
		embed.SetFooter(fmt.Sprintf("Sequence %d-%d | %d appeals shown",
			firstAppeal.ID,
			lastAppeal.ID,
			len(b.appeals)),
			"")
	}

	return embed
}

// formatAppealField formats a single appeal entry for the embed.
func (b *OverviewBuilder) formatAppealField(appeal *types.FullAppeal) (string, string) {
	// Get status and type emojis
	statusEmoji := appeal.Status.Emoji()
	typeEmoji := appeal.Type.Emoji()

	// Format claimed status
	claimedInfo := ""
	if appeal.ClaimedBy != 0 {
		claimedInfo = fmt.Sprintf("\nClaimed by: <@%d>", appeal.ClaimedBy)
	}

	// Format timestamps
	submitted := "N/A"
	if !appeal.Timestamp.IsZero() {
		submitted = fmt.Sprintf("<t:%d:R>", appeal.Timestamp.Unix())
	}

	lastViewed := "N/A"
	if !appeal.LastViewed.IsZero() {
		lastViewed = fmt.Sprintf("<t:%d:R>", appeal.LastViewed.Unix())
	}

	lastActivity := "N/A"
	if !appeal.LastActivity.IsZero() {
		lastActivity = fmt.Sprintf("<t:%d:R>", appeal.LastActivity.Unix())
	}

	// Format user ID based on appeal type
	var userInfo string
	if appeal.Type == enum.AppealTypeRoblox {
		userInfo = fmt.Sprintf("[%s](https://www.roblox.com/users/%d/profile)",
			utils.CensorString(strconv.FormatUint(appeal.UserID, 10), b.streamerMode),
			appeal.UserID)
	} else {
		userInfo = fmt.Sprintf("<@%d>", appeal.UserID)
	}

	fieldName := fmt.Sprintf("%s %s Appeal `#%d`", statusEmoji, typeEmoji, appeal.ID)
	fieldValue := fmt.Sprintf(
		"User: %s\n"+
			"Requester: <@%d>%s\n"+
			"Submitted: %s\n"+
			"Last Viewed: %s\n"+
			"Last Activity: %s",
		userInfo,
		appeal.RequesterID,
		claimedInfo,
		submitted,
		lastViewed,
		lastActivity,
	)

	return fieldName, fieldValue
}

// buildComponents creates all the interactive components.
func (b *OverviewBuilder) buildComponents() []discord.ContainerComponent {
	var components []discord.ContainerComponent
	options := make([]discord.StringSelectMenuOption, 0, len(b.appeals)+1)

	// Add search option for reviewers only
	if b.isReviewer {
		options = append(options, discord.NewStringSelectMenuOption(
			"🔍 Search by ID", constants.AppealSearchCustomID,
		).WithDescription("Look up a specific appeal by ID"))
	}

	// Add appeal selector
	if len(b.appeals) > 0 {
		for _, appeal := range b.appeals {
			// Get status and type emojis
			statusEmoji := appeal.Status.Emoji()
			typeEmoji := appeal.Type.Emoji()

			// Create option for each appeal
			option := discord.NewStringSelectMenuOption(
				fmt.Sprintf("%s %s Appeal #%d", statusEmoji, typeEmoji, appeal.ID),
				strconv.FormatInt(appeal.ID, 10),
			).WithDescription(
				"View appeal for User ID: " +
					utils.CensorString(strconv.FormatUint(appeal.UserID, 10), b.streamerMode),
			)

			options = append(options, option)
		}
	}

	if len(options) > 0 {
		components = append(components, discord.NewActionRow(
			discord.NewStringSelectMenu(constants.AppealSelectID, "Select Appeal", options...),
		))
	}

	// Add status filter dropdown
	components = append(components, discord.NewActionRow(
		discord.NewStringSelectMenu(constants.AppealStatusSelectID, "Filter by Status",
			discord.NewStringSelectMenuOption("Pending Appeals", enum.AppealStatusPending.String()).
				WithDescription("Show only pending appeals").
				WithDefault(b.statusFilter == enum.AppealStatusPending),
			discord.NewStringSelectMenuOption("Accepted Appeals", enum.AppealStatusAccepted.String()).
				WithDescription("Show only accepted appeals").
				WithDefault(b.statusFilter == enum.AppealStatusAccepted),
			discord.NewStringSelectMenuOption("Rejected Appeals", enum.AppealStatusRejected.String()).
				WithDescription("Show only rejected appeals").
				WithDefault(b.statusFilter == enum.AppealStatusRejected)),
	))

	if b.isReviewer {
		// Add sorting options for reviewers
		components = append(components, discord.NewActionRow(
			discord.NewStringSelectMenu(constants.AppealSortSelectID, "Sort by",
				discord.NewStringSelectMenuOption("Oldest First", enum.AppealSortByOldest.String()).
					WithDescription("Show oldest appeals first").
					WithDefault(b.sortBy == enum.AppealSortByOldest),
				discord.NewStringSelectMenuOption("My Claims", enum.AppealSortByClaimed.String()).
					WithDescription("Show appeals claimed by you").
					WithDefault(b.sortBy == enum.AppealSortByClaimed),
				discord.NewStringSelectMenuOption("Newest First", enum.AppealSortByNewest.String()).
					WithDescription("Show newest appeals first").
					WithDefault(b.sortBy == enum.AppealSortByNewest),
			),
		))
	}

	// Add action buttons row
	var actionButtons []discord.InteractiveComponent

	// Add refresh button for everyone
	actionButtons = append(actionButtons,
		discord.NewSecondaryButton("🔄 Refresh", constants.RefreshButtonCustomID))

	// Add new appeal dropdown only for non-reviewers
	if !b.isReviewer {
		components = append(components, discord.NewActionRow(
			discord.NewStringSelectMenu(constants.AppealCreateSelectID, "Create New Appeal",
				discord.NewStringSelectMenuOption("Appeal Roblox User", constants.AppealCreateRobloxButtonCustomID).
					WithDescription("Submit an appeal for a Roblox user").
					WithEmoji(discord.ComponentEmoji{Name: "🎮"}),
				discord.NewStringSelectMenuOption("Appeal Discord User", constants.AppealCreateDiscordButtonCustomID).
					WithDescription("Submit an appeal for a Discord user").
					WithEmoji(discord.ComponentEmoji{Name: "💬"}),
			),
		))
	}

	components = append(components, discord.NewActionRow(actionButtons...))

	// Add navigation buttons
	components = append(components, discord.NewActionRow(
		discord.NewSecondaryButton("◀️", constants.BackButtonCustomID),
		discord.NewSecondaryButton("⏮️", string(session.ViewerFirstPage)).
			WithDisabled(!b.hasPrevPage),
		discord.NewSecondaryButton("◀️", string(session.ViewerPrevPage)).
			WithDisabled(!b.hasPrevPage),
		discord.NewSecondaryButton("▶️", string(session.ViewerNextPage)).
			WithDisabled(!b.hasNextPage),
		discord.NewSecondaryButton("⏭️", string(session.ViewerLastPage)).
			WithDisabled(true),
	))

	return components
}
