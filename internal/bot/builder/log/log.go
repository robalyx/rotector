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
	"github.com/rotector/rotector/internal/common/storage/database/types"
)

// Builder creates the visual layout for viewing activity logs.
type Builder struct {
	settings           *types.UserSetting
	logs               []*types.UserActivityLog
	userID             uint64
	groupID            uint64
	reviewerID         uint64
	activityTypeFilter types.ActivityType
	startDate          time.Time
	endDate            time.Time
	hasNextPage        bool
	hasPrevPage        bool
}

// NewBuilder creates a new log builder.
func NewBuilder(s *session.Session) *Builder {
	var settings *types.UserSetting
	s.GetInterface(constants.SessionKeyUserSettings, &settings)
	var logs []*types.UserActivityLog
	s.GetInterface(constants.SessionKeyLogs, &logs)
	var activityTypeFilter types.ActivityType
	s.GetInterface(constants.SessionKeyActivityTypeFilter, &activityTypeFilter)

	return &Builder{
		settings:           settings,
		logs:               logs,
		userID:             s.GetUint64(constants.SessionKeyUserIDFilter),
		groupID:            s.GetUint64(constants.SessionKeyGroupIDFilter),
		reviewerID:         s.GetUint64(constants.SessionKeyReviewerIDFilter),
		activityTypeFilter: activityTypeFilter,
		startDate:          s.GetTime(constants.SessionKeyDateRangeStartFilter),
		endDate:            s.GetTime(constants.SessionKeyDateRangeEndFilter),
		hasNextPage:        s.GetBool(constants.SessionKeyHasNextPage),
		hasPrevPage:        s.GetBool(constants.SessionKeyHasPrevPage),
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
	if b.activityTypeFilter != types.ActivityTypeAll {
		embed.AddField("Activity Type", fmt.Sprintf("`%s`", b.activityTypeFilter), true)
	}
	if !b.startDate.IsZero() && !b.endDate.IsZero() {
		embed.AddField("Date Range", fmt.Sprintf("`%s` to `%s`", b.startDate.Format("2006-01-02"), b.endDate.Format("2006-01-02")), true)
	}

	// Add log entries with details
	if len(b.logs) > 0 {
		for _, log := range b.logs {
			details := ""
			for key, value := range log.Details {
				newKey := strings.ToUpper(key[:1]) + key[1:]
				newValue := utils.NormalizeString(fmt.Sprintf("%v", value))
				details += fmt.Sprintf("\n%s: `%v`", newKey, newValue)
			}

			description := fmt.Sprintf("Activity: `%s`", log.ActivityType.String())

			if log.ActivityTarget.UserID != 0 {
				description += fmt.Sprintf("\nUser: [%s](https://www.roblox.com/users/%d/profile)",
					utils.CensorString(strconv.FormatUint(log.ActivityTarget.UserID, 10), b.settings.StreamerMode),
					log.ActivityTarget.UserID)
			}

			if log.ActivityTarget.GroupID != 0 {
				description += fmt.Sprintf("\nGroup: [%s](https://www.roblox.com/groups/%d)",
					utils.CensorString(strconv.FormatUint(log.ActivityTarget.GroupID, 10), b.settings.StreamerMode),
					log.ActivityTarget.GroupID)
			}

			description += fmt.Sprintf("\nReviewer: <@%d>%s", log.ReviewerID, details)

			embed.AddField(
				fmt.Sprintf("<t:%d:F>", log.ActivityTimestamp.Unix()),
				description,
				false,
			)
		}
		embed.SetFooterText(fmt.Sprintf("Sequence %d | %d logs shown", b.logs[0].Sequence, len(b.logs)))
	} else {
		embed.AddField("No Results", "No log entries found for the given query", false)
	}

	// Create components
	components := b.buildComponents()

	return discord.NewMessageUpdateBuilder().
		SetEmbeds(embed.Build()).
		AddContainerComponents(components...)
}

// buildComponents creates all interactive components for the log viewer.
func (b *Builder) buildComponents() []discord.ContainerComponent {
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
				b.buildActivityTypeOptions()...),
		),
		// Clear filters and refresh buttons
		discord.NewActionRow(
			discord.NewDangerButton("Clear Filters", constants.ClearFiltersButtonCustomID),
			discord.NewSecondaryButton("Refresh Logs", constants.RefreshButtonCustomID),
		),
		// Navigation buttons
		discord.NewActionRow(
			discord.NewSecondaryButton("◀️", constants.BackButtonCustomID),
			discord.NewSecondaryButton("⏮️", string(utils.ViewerFirstPage)).WithDisabled(!b.hasPrevPage),
			discord.NewSecondaryButton("◀️", string(utils.ViewerPrevPage)).WithDisabled(!b.hasPrevPage),
			discord.NewSecondaryButton("▶️", string(utils.ViewerNextPage)).WithDisabled(!b.hasNextPage),
			discord.NewSecondaryButton("⏭️", string(utils.ViewerLastPage)).WithDisabled(true), // This is disabled on purpose
		),
	}
}

// buildActivityTypeOptions creates the options for the activity type filter menu.
func (b *Builder) buildActivityTypeOptions() []discord.StringSelectMenuOption {
	return []discord.StringSelectMenuOption{
		discord.NewStringSelectMenuOption("All", strconv.Itoa(int(types.ActivityTypeAll))).
			WithDefault(b.activityTypeFilter == types.ActivityTypeAll),
		discord.NewStringSelectMenuOption("User Viewed", strconv.Itoa(int(types.ActivityTypeUserViewed))).
			WithDefault(b.activityTypeFilter == types.ActivityTypeUserViewed),
		discord.NewStringSelectMenuOption("User Confirmed", strconv.Itoa(int(types.ActivityTypeUserConfirmed))).
			WithDefault(b.activityTypeFilter == types.ActivityTypeUserConfirmed),
		discord.NewStringSelectMenuOption("User Confirmed (Custom)", strconv.Itoa(int(types.ActivityTypeUserConfirmedCustom))).
			WithDefault(b.activityTypeFilter == types.ActivityTypeUserConfirmedCustom),
		discord.NewStringSelectMenuOption("User Cleared", strconv.Itoa(int(types.ActivityTypeUserCleared))).
			WithDefault(b.activityTypeFilter == types.ActivityTypeUserCleared),
		discord.NewStringSelectMenuOption("User Skipped", strconv.Itoa(int(types.ActivityTypeUserSkipped))).
			WithDefault(b.activityTypeFilter == types.ActivityTypeUserSkipped),
		discord.NewStringSelectMenuOption("User Rechecked", strconv.Itoa(int(types.ActivityTypeUserRechecked))).
			WithDefault(b.activityTypeFilter == types.ActivityTypeUserRechecked),
		discord.NewStringSelectMenuOption("User Training Upvote", strconv.Itoa(int(types.ActivityTypeUserTrainingUpvote))).
			WithDefault(b.activityTypeFilter == types.ActivityTypeUserTrainingUpvote),
		discord.NewStringSelectMenuOption("User Training Downvote", strconv.Itoa(int(types.ActivityTypeUserTrainingDownvote))).
			WithDefault(b.activityTypeFilter == types.ActivityTypeUserTrainingDownvote),
		discord.NewStringSelectMenuOption("Group Viewed", strconv.Itoa(int(types.ActivityTypeGroupViewed))).
			WithDefault(b.activityTypeFilter == types.ActivityTypeGroupViewed),
		discord.NewStringSelectMenuOption("Group Confirmed", strconv.Itoa(int(types.ActivityTypeGroupConfirmed))).
			WithDefault(b.activityTypeFilter == types.ActivityTypeGroupConfirmed),
		discord.NewStringSelectMenuOption("Group Confirmed (Custom)", strconv.Itoa(int(types.ActivityTypeGroupConfirmedCustom))).
			WithDefault(b.activityTypeFilter == types.ActivityTypeGroupConfirmedCustom),
		discord.NewStringSelectMenuOption("Group Cleared", strconv.Itoa(int(types.ActivityTypeGroupCleared))).
			WithDefault(b.activityTypeFilter == types.ActivityTypeGroupCleared),
		discord.NewStringSelectMenuOption("Group Skipped", strconv.Itoa(int(types.ActivityTypeGroupSkipped))).
			WithDefault(b.activityTypeFilter == types.ActivityTypeGroupSkipped),
		discord.NewStringSelectMenuOption("Group Training Upvote", strconv.Itoa(int(types.ActivityTypeGroupTrainingUpvote))).
			WithDefault(b.activityTypeFilter == types.ActivityTypeGroupTrainingUpvote),
		discord.NewStringSelectMenuOption("Group Training Downvote", strconv.Itoa(int(types.ActivityTypeGroupTrainingDownvote))).
			WithDefault(b.activityTypeFilter == types.ActivityTypeGroupTrainingDownvote),
	}
}
