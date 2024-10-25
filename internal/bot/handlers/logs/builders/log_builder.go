package builders

import (
	"fmt"
	"strings"

	"github.com/disgoorg/disgo/discord"
	"github.com/rotector/rotector/internal/bot/constants"
	"github.com/rotector/rotector/internal/bot/session"
	"github.com/rotector/rotector/internal/bot/utils"
	"github.com/rotector/rotector/internal/common/database"
)

// LogEmbed builds the embed for the log viewer message.
type LogEmbed struct {
	logs               []*database.UserActivityLog
	queryType          string
	queryID            uint64
	activityTypeFilter string
	start              int
	page               int
	total              int
	logsPerPage        int
}

// NewLogEmbed creates a new LogEmbed.
func NewLogEmbed(s *session.Session) *LogEmbed {
	return &LogEmbed{
		logs:               s.Get(constants.SessionKeyLogs).([]*database.UserActivityLog),
		queryType:          s.GetString(constants.SessionKeyQueryType),
		queryID:            s.GetUint64(constants.SessionKeyQueryID),
		activityTypeFilter: s.GetString(constants.SessionKeyActivityTypeFilter),
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

	if b.queryType != "" {
		queryType := strings.Replace(b.queryType, "_modal", "", 1)
		embed.AddField("Query Type", fmt.Sprintf("`%s`", queryType), true)
		embed.AddField("Query ID", fmt.Sprintf("`%d`", b.queryID), true)
	} else {
		embed.SetDescription("Select a query type to view logs")
	}

	if b.activityTypeFilter != "" {
		embed.AddField("Activity Type Filter", fmt.Sprintf("`%s`", b.activityTypeFilter), true)
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
	} else if b.queryType != "" {
		embed.AddField("No Results", "No log entries found for the given query", false)
	}

	components := []discord.ContainerComponent{
		discord.NewActionRow(
			discord.NewStringSelectMenu(constants.ActionSelectMenuCustomID, "Actions",
				discord.NewStringSelectMenuOption("Query User ID", constants.LogsQueryUserIDOption),
				discord.NewStringSelectMenuOption("Query Reviewer ID", constants.LogsQueryReviewerIDOption),
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
