package appeal

import (
	"fmt"
	"strconv"

	"github.com/disgoorg/disgo/discord"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/bot/utils"
	"github.com/robalyx/rotector/internal/common/storage/database/types"
	"github.com/robalyx/rotector/internal/common/storage/database/types/enum"
)

// OverviewBuilder creates the visual layout for the appeal overview interface.
type OverviewBuilder struct {
	appeals      []*types.Appeal
	settings     *types.UserSetting
	sortBy       enum.AppealSortBy
	statusFilter enum.AppealStatus
	hasNextPage  bool
	hasPrevPage  bool
	isReviewer   bool
}

// NewOverviewBuilder creates a new overview builder.
func NewOverviewBuilder(s *session.Session) *OverviewBuilder {
	var appeals []*types.Appeal
	s.GetInterface(constants.SessionKeyAppeals, &appeals)
	var settings *types.UserSetting
	s.GetInterface(constants.SessionKeyUserSettings, &settings)
	var botSettings *types.BotSetting
	s.GetInterface(constants.SessionKeyBotSettings, &botSettings)

	return &OverviewBuilder{
		appeals:      appeals,
		settings:     settings,
		sortBy:       settings.AppealDefaultSort,
		statusFilter: settings.AppealStatusFilter,
		hasNextPage:  s.GetBool(constants.SessionKeyHasNextPage),
		hasPrevPage:  s.GetBool(constants.SessionKeyHasPrevPage),
		isReviewer:   botSettings.IsReviewer(s.UserID()),
	}
}

// Build creates a Discord message showing the appeals list and controls.
func (b *OverviewBuilder) Build() *discord.MessageUpdateBuilder {
	embed := b.buildEmbed()
	components := b.buildComponents()

	return discord.NewMessageUpdateBuilder().
		SetEmbeds(embed.Build()).
		AddContainerComponents(components...)
}

// buildEmbed creates the main embed showing appeal information.
func (b *OverviewBuilder) buildEmbed() *discord.EmbedBuilder {
	embed := discord.NewEmbedBuilder().
		SetTitle("Appeal Tickets").
		SetColor(utils.GetMessageEmbedColor(b.settings.StreamerMode))

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
func (b *OverviewBuilder) formatAppealField(appeal *types.Appeal) (string, string) {
	// Format status with emoji
	var statusEmoji string
	switch appeal.Status {
	case enum.AppealStatusPending:
		statusEmoji = "⏳"
	case enum.AppealStatusAccepted:
		statusEmoji = "✅"
	case enum.AppealStatusRejected:
		statusEmoji = "❌"
	}

	// Format claimed status
	claimedInfo := ""
	if appeal.ClaimedBy != 0 {
		claimedInfo = fmt.Sprintf("\nClaimed by: <@%d>", appeal.ClaimedBy)
	}

	// Format timestamps
	submitted := fmt.Sprintf("<t:%d:R>", appeal.Timestamp.Unix())
	lastViewed := fmt.Sprintf("<t:%d:R>", appeal.LastViewed.Unix())
	lastActivity := fmt.Sprintf("<t:%d:R>", appeal.LastActivity.Unix())

	fieldName := fmt.Sprintf("%s Appeal `#%d`", statusEmoji, appeal.ID)
	fieldValue := fmt.Sprintf(
		"User: [%s](https://www.roblox.com/users/%d/profile)\n"+
			"Requester: <@%d>%s\n"+
			"Submitted: %s\n"+
			"Last Viewed: %s\n"+
			"Last Activity: %s",
		utils.CensorString(strconv.FormatUint(appeal.UserID, 10), b.settings.StreamerMode),
		appeal.UserID,
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

	// Add appeal selector
	if len(b.appeals) > 0 {
		options := make([]discord.StringSelectMenuOption, 0, len(b.appeals))
		for _, appeal := range b.appeals {
			// Format status emoji
			var statusEmoji string
			switch appeal.Status {
			case enum.AppealStatusPending:
				statusEmoji = "⏳"
			case enum.AppealStatusAccepted:
				statusEmoji = "✅"
			case enum.AppealStatusRejected:
				statusEmoji = "❌"
			}

			// Create option for each appeal
			option := discord.NewStringSelectMenuOption(
				fmt.Sprintf("%s Appeal #%d", statusEmoji, appeal.ID),
				strconv.FormatInt(appeal.ID, 10),
			).WithDescription(
				"View appeal for User ID: " +
					utils.CensorString(strconv.FormatUint(appeal.UserID, 10), b.settings.StreamerMode),
			)

			options = append(options, option)
		}

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

	// Add new appeal button only for non-reviewers
	if !b.isReviewer {
		actionButtons = append(actionButtons,
			discord.NewPrimaryButton("New Appeal", constants.AppealCreateButtonCustomID))
	}

	components = append(components, discord.NewActionRow(actionButtons...))

	// Add navigation buttons
	components = append(components, discord.NewActionRow(
		discord.NewSecondaryButton("◀️", constants.BackButtonCustomID),
		discord.NewSecondaryButton("⏮️", string(utils.ViewerFirstPage)).
			WithDisabled(!b.hasPrevPage),
		discord.NewSecondaryButton("◀️", string(utils.ViewerPrevPage)).
			WithDisabled(!b.hasPrevPage),
		discord.NewSecondaryButton("▶️", string(utils.ViewerNextPage)).
			WithDisabled(!b.hasNextPage),
		discord.NewSecondaryButton("⏭️", string(utils.ViewerLastPage)).
			WithDisabled(true),
	))

	return components
}
