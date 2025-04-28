package appeal

import (
	"fmt"
	"strconv"
	"strings"

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
	builder := discord.NewMessageUpdateBuilder()

	// Create info container
	var infoContent strings.Builder
	infoContent.WriteString("## Appeal System\n\n")
	infoContent.WriteString("Welcome to the appeal system. Here you can:\n\n")
	infoContent.WriteString("- Submit appeals for flagged or confirmed users\n")
	infoContent.WriteString("- Track the status of your appeals\n")
	infoContent.WriteString("- Request data deletion under privacy laws\n")
	infoContent.WriteString("- Communicate with moderators about your case")

	infoContainer := discord.NewContainer(
		discord.NewTextDisplay(infoContent.String()),
	).WithAccentColor(utils.GetContainerColor(b.streamerMode))

	// Create list content
	var listContent strings.Builder
	listContent.WriteString("## Active Appeals\n\n")

	if len(b.appeals) == 0 {
		listContent.WriteString("No appeals found.")
	}

	// Create list container components
	listComponents := []discord.ContainerSubComponent{
		discord.NewTextDisplay(listContent.String()),
		discord.NewLargeSeparator(),
	}

	// Add appeal sections if there are appeals
	if len(b.appeals) > 0 {
		for i, appeal := range b.appeals {
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

			// Create section content
			var sectionContent strings.Builder
			sectionContent.WriteString(fmt.Sprintf("### %s %s Appeal `#%d`\n", statusEmoji, typeEmoji, appeal.ID))
			sectionContent.WriteString(fmt.Sprintf("User: %s\n", userInfo))
			sectionContent.WriteString(fmt.Sprintf("Requester: <@%d>%s\n", appeal.RequesterID, claimedInfo))
			sectionContent.WriteString(fmt.Sprintf("Submitted: %s\n", submitted))
			sectionContent.WriteString(fmt.Sprintf("Last Viewed: %s\n", lastViewed))
			sectionContent.WriteString("Last Activity: " + lastActivity)

			// If this is the last appeal, add sequence count
			if i == len(b.appeals)-1 {
				firstAppeal := b.appeals[0]
				lastAppeal := b.appeals[len(b.appeals)-1]
				sectionContent.WriteString(fmt.Sprintf("\n\n-# Sequence %d-%d | %d appeals shown",
					firstAppeal.ID,
					lastAppeal.ID,
					len(b.appeals)))
			}

			// Create section with view button
			section := discord.NewSection(
				discord.NewTextDisplay(sectionContent.String()),
			).WithAccessory(
				discord.NewPrimaryButton("View Appeal", strconv.FormatInt(appeal.ID, 10)),
			)

			listComponents = append(listComponents, section)
		}
	}

	// Add interactive components
	listComponents = append(listComponents, b.buildInteractiveComponents()...)

	listContainer := discord.NewContainer(listComponents...).WithAccentColor(utils.GetContainerColor(b.streamerMode))

	// Add containers and back button
	builder.AddComponents(
		infoContainer,
		listContainer,
		discord.NewActionRow(
			discord.NewSecondaryButton("◀️ Back", constants.BackButtonCustomID),
		),
	)

	return builder
}

// buildInteractiveComponents creates the interactive components for the appeal overview.
func (b *OverviewBuilder) buildInteractiveComponents() []discord.ContainerSubComponent {
	var components []discord.ContainerSubComponent

	// Add status filter dropdown
	components = append(components,
		discord.NewActionRow(
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
		),
	)

	// Add sorting options for reviewers
	if b.isReviewer {
		components = append(components,
			discord.NewActionRow(
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
			),
		)
	}

	// Add new appeal dropdown for non-reviewers
	if !b.isReviewer {
		components = append(components,
			discord.NewActionRow(
				discord.NewStringSelectMenu(constants.AppealCreateSelectID, "Create New Appeal",
					discord.NewStringSelectMenuOption("Appeal Roblox User", constants.AppealCreateRobloxButtonCustomID).
						WithDescription("Submit an appeal for a Roblox user").
						WithEmoji(discord.ComponentEmoji{Name: "🎮"}),
					discord.NewStringSelectMenuOption("Appeal Discord User", constants.AppealCreateDiscordButtonCustomID).
						WithDescription("Submit an appeal for a Discord user").
						WithEmoji(discord.ComponentEmoji{Name: "💬"}),
				),
			),
		)
	}

	// Add refresh and search buttons
	actionButtons := []discord.InteractiveComponent{
		discord.NewSecondaryButton("🔄 Refresh", constants.RefreshButtonCustomID),
	}

	// Add search button for reviewers
	if b.isReviewer {
		actionButtons = append(actionButtons,
			discord.NewSecondaryButton("🔍 Search by ID", constants.AppealSearchCustomID),
		)
	}

	components = append(components,
		discord.NewActionRow(actionButtons...),
	)

	// Add pagination buttons
	components = append(components,
		discord.NewActionRow(
			discord.NewSecondaryButton("⏮️", string(session.ViewerFirstPage)).
				WithDisabled(!b.hasPrevPage),
			discord.NewSecondaryButton("◀️", string(session.ViewerPrevPage)).
				WithDisabled(!b.hasPrevPage),
			discord.NewSecondaryButton("▶️", string(session.ViewerNextPage)).
				WithDisabled(!b.hasNextPage),
			discord.NewSecondaryButton("⏭️", string(session.ViewerLastPage)).
				WithDisabled(true),
		),
	)

	return components
}
