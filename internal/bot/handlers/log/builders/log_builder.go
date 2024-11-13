package builders

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/rotector/rotector/internal/bot/constants"
	"github.com/rotector/rotector/internal/bot/session"
	"github.com/rotector/rotector/internal/bot/utils"
	"github.com/rotector/rotector/internal/common/database"
)

// LogEmbed creates the visual layout for viewing activity logs.
type LogEmbed struct {
	settings           *database.UserSetting
	logs               []*database.UserActivityLog
	userID             uint64
	reviewerID         uint64
	activityTypeFilter database.ActivityType
	startDate          time.Time
	endDate            time.Time
	start              int
	page               int
	total              int
	logsPerPage        int
}

// NewLogEmbed loads log data and filter settings from the session state
// to create a new embed builder.
func NewLogEmbed(s *session.Session) *LogEmbed {
	var settings *database.UserSetting
	s.GetInterface(constants.SessionKeyUserSettings, &settings)
	var logs []*database.UserActivityLog
	s.GetInterface(constants.SessionKeyLogs, &logs)
	var activityTypeFilter database.ActivityType
	s.GetInterface(constants.SessionKeyActivityTypeFilter, &activityTypeFilter)
	var startDate time.Time
	s.GetInterface(constants.SessionKeyDateRangeStart, &startDate)
	var endDate time.Time
	s.GetInterface(constants.SessionKeyDateRangeEnd, &endDate)

	return &LogEmbed{
		settings:           settings,
		logs:               logs,
		userID:             s.GetUint64(constants.SessionKeyUserID),
		reviewerID:         s.GetUint64(constants.SessionKeyReviewerID),
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
func (b *LogEmbed) Build() *discord.MessageUpdateBuilder {
	embed := discord.NewEmbedBuilder().
		SetTitle("Log Query Results").
		SetColor(utils.GetMessageEmbedColor(b.settings.StreamerMode))

	totalPages := (b.total + b.logsPerPage - 1) / b.logsPerPage

	// Add fields for each active query condition
	if b.userID != 0 {
		embed.AddField("User ID", fmt.Sprintf("`%s`", utils.CensorString(strconv.FormatUint(b.userID, 10), b.settings.StreamerMode)), true)
	}
	if b.reviewerID != 0 {
		embed.AddField("Reviewer ID", fmt.Sprintf("`%d`", b.reviewerID), true)
	}
	if b.activityTypeFilter != database.ActivityTypeAll {
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

			embed.AddField(
				fmt.Sprintf("%d. <t:%d:F>", b.start+i+1, log.ActivityTimestamp.Unix()),
				fmt.Sprintf("Activity: `%s`\nUser: [%s](https://www.roblox.com/users/%d/profile)\nReviewer: <@%d>%s",
					log.ActivityType.String(),
					utils.CensorString(strconv.FormatUint(log.UserID, 10), b.settings.StreamerMode),
					log.UserID,
					log.ReviewerID,
					details,
				),
				false,
			)
		}
		embed.SetFooterText(fmt.Sprintf("Page %d/%d | Total Logs: %d", b.page+1, totalPages, b.total))
	} else {
		embed.AddField("No Results", "No log entries found for the given query", false)
	}

	// Add filter menus and navigation buttons
	components := []discord.ContainerComponent{
		// Query condition selection menu
		discord.NewActionRow(
			discord.NewStringSelectMenu(constants.ActionSelectMenuCustomID, "Set Query Condition",
				discord.NewStringSelectMenuOption("Query User ID", constants.LogsQueryUserIDOption),
				discord.NewStringSelectMenuOption("Query Reviewer ID", constants.LogsQueryReviewerIDOption),
				discord.NewStringSelectMenuOption("Query Date Range", constants.LogsQueryDateRangeOption),
			),
		),
		// Activity type filter menu
		discord.NewActionRow(
			discord.NewStringSelectMenu(constants.LogsQueryActivityTypeFilterCustomID, "Filter Activity Type",
				discord.NewStringSelectMenuOption("All", strconv.Itoa(int(database.ActivityTypeAll))).WithDefault(b.activityTypeFilter == database.ActivityTypeAll),
				discord.NewStringSelectMenuOption("Viewed", strconv.Itoa(int(database.ActivityTypeViewed))).WithDefault(b.activityTypeFilter == database.ActivityTypeViewed),
				discord.NewStringSelectMenuOption("Confirmed", strconv.Itoa(int(database.ActivityTypeConfirmed))).WithDefault(b.activityTypeFilter == database.ActivityTypeConfirmed),
				discord.NewStringSelectMenuOption("Confirmed (Custom)", strconv.Itoa(int(database.ActivityTypeConfirmedCustom))).WithDefault(b.activityTypeFilter == database.ActivityTypeConfirmedCustom),
				discord.NewStringSelectMenuOption("Cleared", strconv.Itoa(int(database.ActivityTypeCleared))).WithDefault(b.activityTypeFilter == database.ActivityTypeCleared),
				discord.NewStringSelectMenuOption("Skipped", strconv.Itoa(int(database.ActivityTypeSkipped))).WithDefault(b.activityTypeFilter == database.ActivityTypeSkipped),
				discord.NewStringSelectMenuOption("Rechecked", strconv.Itoa(int(database.ActivityTypeRechecked))).WithDefault(b.activityTypeFilter == database.ActivityTypeRechecked),
			),
		),
		// Navigation buttons
		discord.NewActionRow(
			discord.NewSecondaryButton("◀️", string(constants.BackButtonCustomID)),
			discord.NewSecondaryButton("⏮️", string(utils.ViewerFirstPage)).WithDisabled(b.page == 0 || b.total == 0),
			discord.NewSecondaryButton("◀️", string(utils.ViewerPrevPage)).WithDisabled(b.page == 0 || b.total == 0),
			discord.NewSecondaryButton("▶️", string(utils.ViewerNextPage)).WithDisabled(b.page == totalPages-1 || b.total == 0),
			discord.NewSecondaryButton("⏭️", string(utils.ViewerLastPage)).WithDisabled(b.page == totalPages-1 || b.total == 0),
		),
	}

	return discord.NewMessageUpdateBuilder().
		SetEmbeds(embed.Build()).
		AddContainerComponents(components...)
}
