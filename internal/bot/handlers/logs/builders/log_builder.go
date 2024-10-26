package builders

import (
	"fmt"
	"strings"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/rotector/rotector/internal/bot/constants"
	"github.com/rotector/rotector/internal/bot/session"
	"github.com/rotector/rotector/internal/bot/utils"
	"github.com/rotector/rotector/internal/common/database"
)

// LogEmbed builds the embed for the log viewer message.
type LogEmbed struct {
	logs               []*database.UserActivityLog
	userID             uint64
	reviewerID         uint64
	activityTypeFilter string
	startDate          time.Time
	endDate            time.Time
	start              int
	page               int
	total              int
	logsPerPage        int
}

// NewLogEmbed creates a new LogEmbed.
func NewLogEmbed(s *session.Session) *LogEmbed {
	return &LogEmbed{
		logs:               s.Get(constants.SessionKeyLogs).([]*database.UserActivityLog),
		userID:             s.GetUint64(constants.SessionKeyUserID),
		reviewerID:         s.GetUint64(constants.SessionKeyReviewerID),
		activityTypeFilter: s.GetString(constants.SessionKeyActivityTypeFilter),
		startDate:          s.Get(constants.SessionKeyDateRangeStart).(time.Time),
		endDate:            s.Get(constants.SessionKeyDateRangeEnd).(time.Time),
		start:              s.GetInt(constants.SessionKeyStart),
		page:               s.GetInt(constants.SessionKeyPaginationPage),
		total:              s.GetInt(constants.SessionKeyTotalItems),
		logsPerPage:        constants.LogsPerPage,
	}
}

// Build constructs and returns the discord.Embed.
func (b *LogEmbed) Build() *discord.MessageUpdateBuilder {
	embed := discord.NewEmbedBuilder().
		SetTitle("Log Query Results").
		SetColor(constants.DefaultEmbedColor)

	totalPages := (b.total + b.logsPerPage - 1) / b.logsPerPage

	// Add fields for each active query condition
	if b.userID != 0 {
		embed.AddField("User ID", fmt.Sprintf("`%d`", b.userID), true)
	}
	if b.reviewerID != 0 {
		embed.AddField("Reviewer ID", fmt.Sprintf("`%d`", b.reviewerID), true)
	}
	if b.activityTypeFilter != "" && b.activityTypeFilter != string(database.ActivityTypeAll) {
		embed.AddField("Activity Type", fmt.Sprintf("`%s`", b.activityTypeFilter), true)
	}
	if !b.startDate.IsZero() && !b.endDate.IsZero() {
		embed.AddField("Date Range", fmt.Sprintf("`%s` to `%s`", b.startDate.Format("2006-01-02"), b.endDate.Format("2006-01-02")), true)
	}

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
				fmt.Sprintf("Activity: `%s`\nUser: [%d](https://www.roblox.com/users/%d/profile)\nReviewer: <@%d>%s", log.ActivityType, log.UserID, log.UserID, log.ReviewerID, details),
				false,
			)
		}
		embed.SetFooterText(fmt.Sprintf("Page %d/%d | Total Logs: %d", b.page+1, totalPages, b.total))
	} else {
		embed.AddField("No Results", "No log entries found for the given query", false)
	}

	components := []discord.ContainerComponent{
		discord.NewActionRow(
			discord.NewStringSelectMenu(constants.ActionSelectMenuCustomID, "Set Query Condition",
				discord.NewStringSelectMenuOption("Query User ID", constants.LogsQueryUserIDOption),
				discord.NewStringSelectMenuOption("Query Reviewer ID", constants.LogsQueryReviewerIDOption),
				discord.NewStringSelectMenuOption("Query Date Range", constants.LogsQueryDateRangeOption),
			),
		),
		discord.NewActionRow(
			discord.NewStringSelectMenu(constants.LogsQueryActivityTypeFilterCustomID, "Filter Activity Type",
				discord.NewStringSelectMenuOption("All", string(database.ActivityTypeAll)).WithDefault(b.activityTypeFilter == string(database.ActivityTypeAll)),
				discord.NewStringSelectMenuOption("Reviewed", string(database.ActivityTypeReviewed)).WithDefault(b.activityTypeFilter == string(database.ActivityTypeReviewed)),
				discord.NewStringSelectMenuOption("Banned", string(database.ActivityTypeBanned)).WithDefault(b.activityTypeFilter == string(database.ActivityTypeBanned)),
				discord.NewStringSelectMenuOption("Banned (Custom)", string(database.ActivityTypeBannedCustom)).WithDefault(b.activityTypeFilter == string(database.ActivityTypeBannedCustom)),
				discord.NewStringSelectMenuOption("Cleared", string(database.ActivityTypeCleared)).WithDefault(b.activityTypeFilter == string(database.ActivityTypeCleared)),
				discord.NewStringSelectMenuOption("Skipped", string(database.ActivityTypeSkipped)).WithDefault(b.activityTypeFilter == string(database.ActivityTypeSkipped)),
			),
		),
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
