package log

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/rotector/rotector/internal/bot/constants"
	"github.com/rotector/rotector/internal/bot/core/session"
	"github.com/rotector/rotector/internal/bot/utils"
	"github.com/rotector/rotector/internal/common/storage/database/models"
)

// Builder creates the visual layout for viewing activity logs.
type Builder struct {
	settings           *models.UserSetting
	logs               []*models.UserActivityLog
	userID             uint64
	groupID            uint64
	reviewerID         uint64
	activityTypeFilter models.ActivityType
	startDate          time.Time
	endDate            time.Time
	start              int
	page               int
	total              int
	logsPerPage        int
}

// NewBuilder creates a new log builder.
func NewBuilder(s *session.Session) *Builder {
	var settings *models.UserSetting
	s.GetInterface(constants.SessionKeyUserSettings, &settings)
	var logs []*models.UserActivityLog
	s.GetInterface(constants.SessionKeyLogs, &logs)
	var activityTypeFilter models.ActivityType
	s.GetInterface(constants.SessionKeyActivityTypeFilter, &activityTypeFilter)
	var startDate time.Time
	s.GetInterface(constants.SessionKeyDateRangeStartFilter, &startDate)
	var endDate time.Time
	s.GetInterface(constants.SessionKeyDateRangeEndFilter, &endDate)

	return &Builder{
		settings:           settings,
		logs:               logs,
		userID:             s.GetUint64(constants.SessionKeyUserIDFilter),
		groupID:            s.GetUint64(constants.SessionKeyGroupIDFilter),
		reviewerID:         s.GetUint64(constants.SessionKeyReviewerIDFilter),
		activityTypeFilter: activityTypeFilter,
		startDate:          startDate,
		endDate:            endDate,
		start:              s.GetInt(constants.SessionKeyStart),
		page:               s.GetInt(constants.SessionKeyPaginationPage),
		total:              s.GetInt(constants.SessionKeyTotalItems),
		logsPerPage:        constants.LogsPerPage,
	}
}

// Build creates a Discord message showing:
// - Current filter settings (user ID, reviewer ID, activity type, date range)
// - List of log entries with timestamps and details
// - Filter menus and navigation buttons.
func (b *Builder) Build() *discord.MessageUpdateBuilder {
	// Create embed
	embed := discord.NewEmbedBuilder().
		SetTitle("Log Query Results").
		SetColor(utils.GetMessageEmbedColor(b.settings.StreamerMode))

	// Create components
	totalPages := (b.total + b.logsPerPage - 1) / b.logsPerPage
	components := b.buildComponents(totalPages)

	// Add fields for each active query condition
	if b.userID != 0 {
		embed.AddField("User ID", fmt.Sprintf("`%s`", utils.CensorString(strconv.FormatUint(b.userID, 10), b.settings.StreamerMode)), true)
	}
	if b.groupID != 0 {
		embed.AddField("Group ID", fmt.Sprintf("`%s`", utils.CensorString(strconv.FormatUint(b.groupID, 10), b.settings.StreamerMode)), true)
	}
	if b.reviewerID != 0 {
		embed.AddField("Reviewer ID", fmt.Sprintf("`%d`", b.reviewerID), true)
	}
	if b.activityTypeFilter != models.ActivityTypeAll {
		embed.AddField("Activity Type", fmt.Sprintf("`%s`", b.activityTypeFilter), true)
	}
	if !b.startDate.IsZero() && !b.endDate.IsZero() {
		embed.AddField("Date Range", fmt.Sprintf("`%s` to `%s`", b.startDate.Format("2006-01-02"), b.endDate.Format("2006-01-02")), true)
	}

	// Add log entries with details
	if len(b.logs) > 0 {
		for i, log := range b.logs {
			details := ""
			for key, value := range log.Details {
				newKey := strings.ToUpper(key[:1]) + key[1:]
				newValue := utils.NormalizeString(fmt.Sprintf("%v", value))

				details += fmt.Sprintf("\n%s: `%v`", newKey, newValue)
			}

			description := fmt.Sprintf("Activity: `%s`", log.ActivityType.String())

			if log.UserID != 0 {
				description += fmt.Sprintf("\nUser: [%s](https://www.roblox.com/users/%d/profile)",
					utils.CensorString(strconv.FormatUint(log.UserID, 10), b.settings.StreamerMode),
					log.UserID)
			}

			if log.GroupID != 0 {
				description += fmt.Sprintf("\nGroup: [%s](https://www.roblox.com/groups/%d)",
					utils.CensorString(strconv.FormatUint(log.GroupID, 10), b.settings.StreamerMode),
					log.GroupID)
			}

			description += fmt.Sprintf("\nReviewer: <@%d>%s", log.ReviewerID, details)

			embed.AddField(
				fmt.Sprintf("%d. <t:%d:F>", b.start+i+1, log.ActivityTimestamp.Unix()),
				description,
				false,
			)
		}
		embed.SetFooterText(fmt.Sprintf("Page %d/%d | Total Logs: %d", b.page+1, totalPages, b.total))
	} else {
		embed.AddField("No Results", "No log entries found for the given query", false)
	}

	return discord.NewMessageUpdateBuilder().
		SetEmbeds(embed.Build()).
		AddContainerComponents(components...)
}

// buildComponents creates all interactive components for the log viewer.
func (b *Builder) buildComponents(totalPages int) []discord.ContainerComponent {
	return []discord.ContainerComponent{
		// Query condition selection menu
		discord.NewActionRow(
			discord.NewStringSelectMenu(constants.ActionSelectMenuCustomID, "Set Filter Condition",
				discord.NewStringSelectMenuOption("Filter by User ID", constants.LogsQueryUserIDOption).
					WithDescription(utils.CensorString(strconv.FormatUint(b.userID, 10), b.settings.StreamerMode)),
				discord.NewStringSelectMenuOption("Filter by Group ID", constants.LogsQueryGroupIDOption).
					WithDescription(utils.CensorString(strconv.FormatUint(b.groupID, 10), b.settings.StreamerMode)),
				discord.NewStringSelectMenuOption("Filter by Reviewer ID", constants.LogsQueryReviewerIDOption).
					WithDescription(strconv.FormatUint(b.reviewerID, 10)),
				discord.NewStringSelectMenuOption("Filter by Date Range", constants.LogsQueryDateRangeOption).
					WithDescription(fmt.Sprintf("%s to %s", b.startDate.Format("2006-01-02"), b.endDate.Format("2006-01-02"))),
			),
		),
		// Activity type filter menu
		discord.NewActionRow(
			discord.NewStringSelectMenu(constants.LogsQueryActivityTypeFilterCustomID, "Filter Activity Type",
				discord.NewStringSelectMenuOption("All", strconv.Itoa(int(models.ActivityTypeAll))).
					WithDefault(b.activityTypeFilter == models.ActivityTypeAll),
				discord.NewStringSelectMenuOption("User Viewed", strconv.Itoa(int(models.ActivityTypeUserViewed))).
					WithDefault(b.activityTypeFilter == models.ActivityTypeUserViewed),
				discord.NewStringSelectMenuOption("User Confirmed", strconv.Itoa(int(models.ActivityTypeUserConfirmed))).
					WithDefault(b.activityTypeFilter == models.ActivityTypeUserConfirmed),
				discord.NewStringSelectMenuOption("User Confirmed (Custom)", strconv.Itoa(int(models.ActivityTypeUserConfirmedCustom))).
					WithDefault(b.activityTypeFilter == models.ActivityTypeUserConfirmedCustom),
				discord.NewStringSelectMenuOption("User Cleared", strconv.Itoa(int(models.ActivityTypeUserCleared))).
					WithDefault(b.activityTypeFilter == models.ActivityTypeUserCleared),
				discord.NewStringSelectMenuOption("User Skipped", strconv.Itoa(int(models.ActivityTypeUserSkipped))).
					WithDefault(b.activityTypeFilter == models.ActivityTypeUserSkipped),
				discord.NewStringSelectMenuOption("User Rechecked", strconv.Itoa(int(models.ActivityTypeUserRechecked))).
					WithDefault(b.activityTypeFilter == models.ActivityTypeUserRechecked),
				discord.NewStringSelectMenuOption("User Training Upvote", strconv.Itoa(int(models.ActivityTypeUserTrainingUpvote))).
					WithDefault(b.activityTypeFilter == models.ActivityTypeUserTrainingUpvote),
				discord.NewStringSelectMenuOption("User Training Downvote", strconv.Itoa(int(models.ActivityTypeUserTrainingDownvote))).
					WithDefault(b.activityTypeFilter == models.ActivityTypeUserTrainingDownvote),
				discord.NewStringSelectMenuOption("Group Viewed", strconv.Itoa(int(models.ActivityTypeGroupViewed))).
					WithDefault(b.activityTypeFilter == models.ActivityTypeGroupViewed),
				discord.NewStringSelectMenuOption("Group Confirmed", strconv.Itoa(int(models.ActivityTypeGroupConfirmed))).
					WithDefault(b.activityTypeFilter == models.ActivityTypeGroupConfirmed),
				discord.NewStringSelectMenuOption("Group Confirmed (Custom)", strconv.Itoa(int(models.ActivityTypeGroupConfirmedCustom))).
					WithDefault(b.activityTypeFilter == models.ActivityTypeGroupConfirmedCustom),
				discord.NewStringSelectMenuOption("Group Cleared", strconv.Itoa(int(models.ActivityTypeGroupCleared))).
					WithDefault(b.activityTypeFilter == models.ActivityTypeGroupCleared),
				discord.NewStringSelectMenuOption("Group Skipped", strconv.Itoa(int(models.ActivityTypeGroupSkipped))).
					WithDefault(b.activityTypeFilter == models.ActivityTypeGroupSkipped),
				discord.NewStringSelectMenuOption("Group Training Upvote", strconv.Itoa(int(models.ActivityTypeGroupTrainingUpvote))).
					WithDefault(b.activityTypeFilter == models.ActivityTypeGroupTrainingUpvote),
				discord.NewStringSelectMenuOption("Group Training Downvote", strconv.Itoa(int(models.ActivityTypeGroupTrainingDownvote))).
					WithDefault(b.activityTypeFilter == models.ActivityTypeGroupTrainingDownvote),
			),
		),
		// Clear filters and refresh buttons
		discord.NewActionRow(
			discord.NewDangerButton("Clear Filters", constants.ClearFiltersButtonCustomID),
			discord.NewSecondaryButton("Refresh Logs", constants.RefreshButtonCustomID),
		),
		// Navigation buttons
		discord.NewActionRow(
			discord.NewSecondaryButton("◀️", constants.BackButtonCustomID),
			discord.NewSecondaryButton("⏮️", string(utils.ViewerFirstPage)).WithDisabled(b.page == 0 || b.total == 0),
			discord.NewSecondaryButton("◀️", string(utils.ViewerPrevPage)).WithDisabled(b.page == 0 || b.total == 0),
			discord.NewSecondaryButton("▶️", string(utils.ViewerNextPage)).WithDisabled(b.page == totalPages-1 || b.total == 0),
			discord.NewSecondaryButton("⏭️", string(utils.ViewerLastPage)).WithDisabled(b.page == totalPages-1 || b.total == 0),
		),
	}
}
