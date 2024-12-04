package group

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/rotector/rotector/assets"
	"github.com/rotector/rotector/internal/bot/constants"
	"github.com/rotector/rotector/internal/bot/core/session"
	"github.com/rotector/rotector/internal/bot/utils"
	"github.com/rotector/rotector/internal/common/client/fetcher"
	"github.com/rotector/rotector/internal/common/storage/database"
	"github.com/rotector/rotector/internal/common/storage/database/types"
)

// ReviewBuilder creates the visual layout for reviewing a group.
type ReviewBuilder struct {
	db          *database.Client
	settings    *types.UserSetting
	botSettings *types.BotSetting
	userID      uint64
	group       *types.ConfirmedGroup
}

// NewReviewBuilder creates a new review builder.
func NewReviewBuilder(s *session.Session, db *database.Client) *ReviewBuilder {
	var settings *types.UserSetting
	s.GetInterface(constants.SessionKeyUserSettings, &settings)
	var botSettings *types.BotSetting
	s.GetInterface(constants.SessionKeyBotSettings, &botSettings)
	var group *types.ConfirmedGroup
	s.GetInterface(constants.SessionKeyGroupTarget, &group)

	return &ReviewBuilder{
		db:          db,
		settings:    settings,
		botSettings: botSettings,
		userID:      s.GetUint64(constants.SessionKeyUserID),
		group:       group,
	}
}

// Build creates a Discord message with group information in an embed and adds
// interactive components for reviewing the group.
func (b *ReviewBuilder) Build() *discord.MessageUpdateBuilder {
	// Create embeds
	modeEmbed := b.buildModeEmbed()
	reviewEmbed := b.buildReviewEmbed()

	// Create components
	components := b.buildComponents()

	// Create builder and handle thumbnail
	builder := discord.NewMessageUpdateBuilder()
	if b.group.ThumbnailURL != "" && b.group.ThumbnailURL != fetcher.ThumbnailPlaceholder {
		reviewEmbed.SetThumbnail(b.group.ThumbnailURL)
	} else {
		// Load and attach placeholder image
		placeholderImage, err := assets.Images.Open("images/content_deleted.png")
		if err == nil {
			builder.SetFiles(discord.NewFile("content_deleted.png", "", placeholderImage))
			_ = placeholderImage.Close()
		}
		reviewEmbed.SetThumbnail("attachment://content_deleted.png")
	}

	return builder.
		SetEmbeds(modeEmbed.Build(), reviewEmbed.Build()).
		AddContainerComponents(components...)
}

// buildModeEmbed creates the review mode info embed.
func (b *ReviewBuilder) buildModeEmbed() *discord.EmbedBuilder {
	var mode string
	var description string

	// Format review mode
	switch b.settings.ReviewMode {
	case types.TrainingReviewMode:
		mode = "🎓 Training Mode"
		description += `
		**You are not an official reviewer.**
		You may help moderators by using upvotes/downvotes to indicate suspicious activity. Information is censored and external links are disabled.
		`
	case types.StandardReviewMode:
		mode = "⚠️ Standard Mode"
		description += `
		Your actions are recorded and affect the database. Please review carefully before taking action.
		`
	default:
		mode = "❌ Unknown Mode"
		description = "Error encountered. Please check your settings."
	}

	return discord.NewEmbedBuilder().
		SetTitle(mode).
		SetDescription(description).
		SetColor(utils.GetMessageEmbedColor(b.settings.StreamerMode))
}

// buildReviewEmbed creates the main review information embed.
func (b *ReviewBuilder) buildReviewEmbed() *discord.EmbedBuilder {
	embed := discord.NewEmbedBuilder().
		SetColor(utils.GetMessageEmbedColor(b.settings.StreamerMode))

	// Add status indicator
	var status string
	if b.group.VerifiedAt.IsZero() {
		status = "⏳ Flagged Group"
	} else {
		status = "⚠️ Confirmed Group"
	}

	header := fmt.Sprintf("%s • 👍 %d | 👎 %d", status, b.group.Upvotes, b.group.Downvotes)
	lastUpdated := fmt.Sprintf("<t:%d:R>", b.group.LastUpdated.Unix())
	confidence := fmt.Sprintf("%.2f", b.group.Confidence)
	memberCount := strconv.FormatUint(b.group.MemberCount, 10)
	flaggedMembers := strconv.Itoa(len(b.group.FlaggedUsers))

	if b.settings.ReviewMode == types.TrainingReviewMode {
		// Training mode - show limited information without links
		embed.SetAuthorName(header).
			AddField("ID", utils.CensorString(strconv.FormatUint(b.group.ID, 10), true), true).
			AddField("Name", utils.CensorString(b.group.Name, true), true).
			AddField("Owner", utils.CensorString(strconv.FormatUint(b.group.Owner, 10), true), true).
			AddField("Members", memberCount, true).
			AddField("Flagged Members", flaggedMembers, true).
			AddField("Confidence", confidence, true).
			AddField("Last Updated", lastUpdated, true).
			AddField("Reason", b.group.Reason, false).
			AddField("Shout", b.getShout(), false).
			AddField("Description", b.getDescription(), false)
	} else {
		// Standard mode - show all information with links
		embed.SetAuthorName(header).
			AddField("ID", fmt.Sprintf(
				"[%s](https://www.roblox.com/groups/%d)",
				utils.CensorString(strconv.FormatUint(b.group.ID, 10), b.settings.StreamerMode),
				b.group.ID,
			), true).
			AddField("Name", utils.CensorString(b.group.Name, b.settings.StreamerMode), true).
			AddField("Owner", fmt.Sprintf(
				"[%s](https://www.roblox.com/users/%d/profile)",
				utils.CensorString(strconv.FormatUint(b.group.Owner, 10), b.settings.StreamerMode),
				b.group.Owner,
			), true).
			AddField("Members", memberCount, true).
			AddField("Flagged Members", flaggedMembers, true).
			AddField("Confidence", confidence, true).
			AddField("Last Updated", lastUpdated, true).
			AddField("Reason", b.group.Reason, false).
			AddField("Shout", b.getShout(), false).
			AddField("Description", b.getDescription(), false).
			AddField("Review History", b.getReviewHistory(), false)
	}

	// Add verified at time if this is a confirmed group
	if !b.group.VerifiedAt.IsZero() {
		embed.AddField("Verified At", fmt.Sprintf("<t:%d:R>", b.group.VerifiedAt.Unix()), true)
	}

	return embed
}

// buildActionOptions creates the action menu options.
func (b *ReviewBuilder) buildActionOptions() []discord.StringSelectMenuOption {
	// Get switch text and description
	var switchText string
	var switchDesc string
	if b.settings.ReviewTargetMode == types.FlaggedReviewTarget {
		switchText = "Switch to Confirmed Target"
		switchDesc = "Switch to re-reviewing confirmed groups"
	} else {
		switchText = "Switch to Flagged Target"
		switchDesc = "Switch to reviewing flagged groups"
	}

	// Create base options that everyone can access
	options := []discord.StringSelectMenuOption{
		discord.NewStringSelectMenuOption(switchText, constants.SwitchTargetModeCustomID).
			WithEmoji(discord.ComponentEmoji{Name: "🔄"}).
			WithDescription(switchDesc),
	}

	// Add reviewer-only options
	if b.botSettings.IsReviewer(b.userID) {
		reviewerOptions := []discord.StringSelectMenuOption{
			discord.NewStringSelectMenuOption("Confirm with reason", constants.GroupConfirmWithReasonButtonCustomID).
				WithEmoji(discord.ComponentEmoji{Name: "🚫"}).
				WithDescription("Confirm the group with a custom reason"),
			discord.NewStringSelectMenuOption("View group logs", constants.GroupViewLogsButtonCustomID).
				WithEmoji(discord.ComponentEmoji{Name: "📋"}).
				WithDescription("View activity logs for this group"),
		}
		options = append(reviewerOptions, options...)

		// Add mode switch option
		if b.settings.ReviewMode == types.TrainingReviewMode {
			options = append(options,
				discord.NewStringSelectMenuOption("Switch to Standard Mode", constants.SwitchReviewModeCustomID).
					WithEmoji(discord.ComponentEmoji{Name: "⚠️"}).
					WithDescription("Switch to standard mode for actual moderation"),
			)
		} else {
			options = append(options,
				discord.NewStringSelectMenuOption("Switch to Training Mode", constants.SwitchReviewModeCustomID).
					WithEmoji(discord.ComponentEmoji{Name: "🎓"}).
					WithDescription("Switch to training mode to practice"),
			)
		}
	}

	return options
}

// buildComponents creates all interactive components for the review menu.
func (b *ReviewBuilder) buildComponents() []discord.ContainerComponent {
	return []discord.ContainerComponent{
		// Sorting options menu
		discord.NewActionRow(
			discord.NewStringSelectMenu(constants.SortOrderSelectMenuCustomID, "Sorting",
				discord.NewStringSelectMenuOption("Selected by random", string(types.SortByRandom)).
					WithDefault(b.settings.GroupDefaultSort == types.SortByRandom).
					WithEmoji(discord.ComponentEmoji{Name: "🔀"}),
				discord.NewStringSelectMenuOption("Selected by confidence", string(types.SortByConfidence)).
					WithDefault(b.settings.GroupDefaultSort == types.SortByConfidence).
					WithEmoji(discord.ComponentEmoji{Name: "🔍"}),
				discord.NewStringSelectMenuOption("Selected by flagged users", string(types.SortByFlaggedUsers)).
					WithDefault(b.settings.GroupDefaultSort == types.SortByFlaggedUsers).
					WithEmoji(discord.ComponentEmoji{Name: "👥"}),
				discord.NewStringSelectMenuOption("Selected by bad reputation", string(types.SortByReputation)).
					WithDefault(b.settings.GroupDefaultSort == types.SortByReputation).
					WithEmoji(discord.ComponentEmoji{Name: "👎"}),
			),
		),
		// Action options menu
		discord.NewActionRow(
			discord.NewStringSelectMenu(constants.ActionSelectMenuCustomID, "Actions", b.buildActionOptions()...),
		),
		// Quick action buttons
		discord.NewActionRow(
			discord.NewSecondaryButton("◀️", constants.BackButtonCustomID),
			discord.NewDangerButton(b.getConfirmButtonLabel(), constants.GroupConfirmButtonCustomID),
			discord.NewSuccessButton(b.getClearButtonLabel(), constants.GroupClearButtonCustomID),
			discord.NewSecondaryButton("Skip", constants.GroupSkipButtonCustomID),
		),
	}
}

// getDescription returns the description field for the embed.
func (b *ReviewBuilder) getDescription() string {
	description := b.group.Description

	// Check if description is empty
	if description == "" {
		return constants.NotApplicable
	}

	// Format the description
	description = utils.FormatString(description)

	return description
}

// getShout returns the shout field for the embed.
func (b *ReviewBuilder) getShout() string {
	// Skip if shout is not available
	if b.group.Shout == nil {
		return constants.NotApplicable
	}

	return utils.FormatString(b.group.Shout.Body)
}

// getReviewHistory returns the review history field for the embed.
func (b *ReviewBuilder) getReviewHistory() string {
	logs, nextCursor, err := b.db.UserActivity().GetLogs(
		context.Background(),
		types.ActivityFilter{
			GroupID:      b.group.ID,
			ReviewerID:   0,
			ActivityType: types.ActivityTypeAll,
			StartDate:    time.Time{},
			EndDate:      time.Time{},
		},
		nil,
		constants.ReviewHistoryLimit,
	)
	if err != nil {
		return "Failed to fetch review history"
	}

	if len(logs) == 0 {
		return constants.NotApplicable
	}

	history := make([]string, 0, len(logs))
	for _, log := range logs {
		history = append(history, fmt.Sprintf("- <@%d> (%s) - <t:%d:R>",
			log.ReviewerID, log.ActivityType.String(), log.ActivityTimestamp.Unix()))
	}

	if nextCursor != nil {
		history = append(history, "... and more")
	}

	return strings.Join(history, "\n")
}

// getConfirmButtonLabel returns the appropriate label for the confirm button based on review mode.
func (b *ReviewBuilder) getConfirmButtonLabel() string {
	if b.settings.ReviewMode == types.TrainingReviewMode {
		return "Downvote"
	}
	return "Confirm"
}

// getClearButtonLabel returns the appropriate label for the clear button based on review mode.
func (b *ReviewBuilder) getClearButtonLabel() string {
	if b.settings.ReviewMode == types.TrainingReviewMode {
		return "Upvote"
	}
	return "Clear"
}
