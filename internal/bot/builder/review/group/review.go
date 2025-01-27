package group

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/disgoorg/disgo/discord"
	apiTypes "github.com/jaxron/roapi.go/pkg/api/types"
	"github.com/robalyx/rotector/assets"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/bot/utils"
	"github.com/robalyx/rotector/internal/common/client/fetcher"
	"github.com/robalyx/rotector/internal/common/storage/database"
	"github.com/robalyx/rotector/internal/common/storage/database/types"
	"github.com/robalyx/rotector/internal/common/storage/database/types/enum"
)

// ReviewBuilder creates the visual layout for reviewing a group.
type ReviewBuilder struct {
	db          database.Client
	userID      uint64
	group       *types.ReviewGroup
	groupInfo   *apiTypes.GroupResponse
	memberIDs   []uint64
	reviewMode  enum.ReviewMode
	defaultSort enum.ReviewSortBy
	isReviewer  bool
	privacyMode bool
}

// NewReviewBuilder creates a new review builder.
func NewReviewBuilder(s *session.Session, db database.Client) *ReviewBuilder {
	return &ReviewBuilder{
		db:          db,
		userID:      s.UserID(),
		group:       session.GroupTarget.Get(s),
		groupInfo:   session.GroupInfo.Get(s),
		memberIDs:   session.GroupMemberIDs.Get(s),
		reviewMode:  session.UserReviewMode.Get(s),
		defaultSort: session.UserGroupDefaultSort.Get(s),
		isReviewer:  s.BotSettings().IsReviewer(s.UserID()),
		privacyMode: session.UserReviewMode.Get(s) == enum.ReviewModeTraining || session.UserStreamerMode.Get(s),
	}
}

// Build creates a Discord message with group information in an embed and adds
// interactive components for reviewing the group.
func (b *ReviewBuilder) Build() *discord.MessageUpdateBuilder {
	builder := discord.NewMessageUpdateBuilder()

	// Create embeds
	modeEmbed := b.buildModeEmbed()
	reviewEmbed := b.buildReviewEmbed()

	// Create components
	components := b.buildComponents()

	// Create builder and handle thumbnail
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
		AddEmbeds(modeEmbed.Build(), reviewEmbed.Build()).
		AddContainerComponents(components...)
}

// buildModeEmbed creates the review mode info embed.
func (b *ReviewBuilder) buildModeEmbed() *discord.EmbedBuilder {
	var mode string
	var description string

	// Format review mode
	switch b.reviewMode {
	case enum.ReviewModeTraining:
		mode = "🎓 Training Mode"
		description += `
		**You are not an official reviewer.**
		You may help moderators by downvoting to indicate inappropriate activity. Information is censored and external links are disabled.
		`
	case enum.ReviewModeStandard:
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
		SetColor(utils.GetMessageEmbedColor(b.privacyMode))
}

// buildReviewEmbed creates the main review information embed.
func (b *ReviewBuilder) buildReviewEmbed() *discord.EmbedBuilder {
	embed := discord.NewEmbedBuilder().
		SetColor(utils.GetMessageEmbedColor(b.privacyMode)).
		SetTitle(fmt.Sprintf("⚠️ %d Reports • 🛡️ %d Safe ",
			b.group.Reputation.Downvotes,
			b.group.Reputation.Upvotes,
		))

	// Add status indicator based on group status
	var status string
	switch b.group.Status {
	case enum.GroupTypeConfirmed:
		status = "⚠️ Confirmed"
	case enum.GroupTypeFlagged:
		status = "⏳ Pending Review"
	case enum.GroupTypeCleared:
		status = "✅ Cleared"
	case enum.GroupTypeUnflagged:
		status = "🔄 Unflagged"
	}

	// Add locked status if applicable
	if b.group.IsLocked {
		status += " 🔒 Locked"
	}

	lastUpdated := fmt.Sprintf("<t:%d:R>", b.group.LastUpdated.Unix())
	confidence := fmt.Sprintf("%.2f", b.group.Confidence)
	memberCount := strconv.FormatUint(b.groupInfo.MemberCount, 10)
	flaggedMembers := strconv.Itoa(len(b.memberIDs))

	// Censor reason if needed
	reason := utils.CensorStringsInText(
		b.group.Reason,
		b.privacyMode,
		strconv.FormatUint(b.group.ID, 10),
		b.group.Name,
		strconv.FormatUint(b.group.Owner.UserID, 10),
	)

	if b.reviewMode == enum.ReviewModeTraining {
		// Training mode - show limited information without links
		embed.AddField("ID", utils.CensorString(strconv.FormatUint(b.group.ID, 10), true), true).
			AddField("Name", utils.CensorString(b.group.Name, true), true).
			AddField("Owner", utils.CensorString(strconv.FormatUint(b.group.Owner.UserID, 10), true), true).
			AddField("Members", memberCount, true).
			AddField("Flagged Members", flaggedMembers, true).
			AddField("Confidence", confidence, true).
			AddField("Last Updated", lastUpdated, true).
			AddField("Reason", reason, false).
			AddField("Shout", b.getShout(), false).
			AddField("Description", b.getDescription(), false)
	} else {
		// Standard mode - show all information with links
		embed.AddField("ID", fmt.Sprintf(
			"[%s](https://www.roblox.com/groups/%d)",
			utils.CensorString(strconv.FormatUint(b.group.ID, 10), b.privacyMode),
			b.group.ID,
		), true).
			AddField("Name", utils.CensorString(b.group.Name, b.privacyMode), true).
			AddField("Owner", fmt.Sprintf(
				"[%s](https://www.roblox.com/users/%d/profile)",
				utils.CensorString(strconv.FormatUint(b.group.Owner.UserID, 10), b.privacyMode),
				b.group.Owner.UserID,
			), true).
			AddField("Members", memberCount, true).
			AddField("Flagged Members", flaggedMembers, true).
			AddField("Confidence", confidence, true).
			AddField("Last Updated", lastUpdated, true).
			AddField("Reason", reason, false).
			AddField("Shout", b.getShout(), false).
			AddField("Description", b.getDescription(), false).
			AddField("Review History", b.getReviewHistory(), false)
	}

	// Add status-specific timestamps
	if !b.group.VerifiedAt.IsZero() {
		embed.AddField("Verified At", fmt.Sprintf("<t:%d:R>", b.group.VerifiedAt.Unix()), true)
	}
	if !b.group.ClearedAt.IsZero() {
		embed.AddField("Cleared At", fmt.Sprintf("<t:%d:R>", b.group.ClearedAt.Unix()), true)
	}

	// Add UUID and status to footer
	embed.SetFooter(fmt.Sprintf("%s • UUID: %s", status, b.group.UUID.String()), "")

	return embed
}

// buildSortingOptions creates the sorting options.
func (b *ReviewBuilder) buildSortingOptions() []discord.StringSelectMenuOption {
	return []discord.StringSelectMenuOption{
		discord.NewStringSelectMenuOption("Selected by random", enum.ReviewSortByRandom.String()).
			WithDefault(b.defaultSort == enum.ReviewSortByRandom).
			WithEmoji(discord.ComponentEmoji{Name: "🔀"}),
		discord.NewStringSelectMenuOption("Selected by confidence", enum.ReviewSortByConfidence.String()).
			WithDefault(b.defaultSort == enum.ReviewSortByConfidence).
			WithEmoji(discord.ComponentEmoji{Name: "🔮"}),
		discord.NewStringSelectMenuOption("Selected by last updated time", enum.ReviewSortByLastUpdated.String()).
			WithDefault(b.defaultSort == enum.ReviewSortByLastUpdated).
			WithEmoji(discord.ComponentEmoji{Name: "📅"}),
		discord.NewStringSelectMenuOption("Selected by bad reputation", enum.ReviewSortByReputation.String()).
			WithDefault(b.defaultSort == enum.ReviewSortByReputation).
			WithEmoji(discord.ComponentEmoji{Name: "👎"}),
	}
}

// buildActionOptions creates the action menu options.
func (b *ReviewBuilder) buildActionOptions() []discord.StringSelectMenuOption {
	options := []discord.StringSelectMenuOption{
		discord.NewStringSelectMenuOption("View Flagged Members", constants.GroupViewMembersButtonCustomID).
			WithDescription("View all flagged members of this group").
			WithEmoji(discord.ComponentEmoji{Name: "👥"}),
	}

	// Add reviewer-only options
	if b.isReviewer {
		reviewerOptions := []discord.StringSelectMenuOption{
			discord.NewStringSelectMenuOption("Ask AI about group", constants.OpenAIChatButtonCustomID).
				WithEmoji(discord.ComponentEmoji{Name: "🤖"}).
				WithDescription("Ask the AI questions about this group"),
			discord.NewStringSelectMenuOption("View group logs", constants.GroupViewLogsButtonCustomID).
				WithEmoji(discord.ComponentEmoji{Name: "📋"}).
				WithDescription("View activity logs for this group"),
			discord.NewStringSelectMenuOption("Confirm with reason", constants.GroupConfirmWithReasonButtonCustomID).
				WithEmoji(discord.ComponentEmoji{Name: "🚫"}).
				WithDescription("Confirm the group with a custom reason"),
			discord.NewStringSelectMenuOption("Change Review Mode", constants.ReviewModeOption).
				WithEmoji(discord.ComponentEmoji{Name: "🎓"}).
				WithDescription("Switch between training and standard modes"),
		}
		options = append(options, reviewerOptions...)
	}

	// Add last default options
	options = append(options,
		discord.NewStringSelectMenuOption("Change Review Target", constants.ReviewTargetModeOption).
			WithEmoji(discord.ComponentEmoji{Name: "🎯"}).
			WithDescription("Change what type of groups to review"),
	)

	return options
}

// buildComponents creates all interactive components for the review menu.
func (b *ReviewBuilder) buildComponents() []discord.ContainerComponent {
	components := []discord.ContainerComponent{}

	// Add sorting options
	components = append(components,
		discord.NewActionRow(
			discord.NewStringSelectMenu(constants.SortOrderSelectMenuCustomID, "Sorting", b.buildSortingOptions()...),
		),
	)

	// Add action options menu
	components = append(components,
		discord.NewActionRow(
			discord.NewStringSelectMenu(constants.ActionSelectMenuCustomID, "Actions", b.buildActionOptions()...),
		),
	)

	// Add navigation/action buttons
	components = append(components, discord.NewActionRow(
		discord.NewSecondaryButton("◀️", constants.BackButtonCustomID),
		discord.NewDangerButton(b.getConfirmButtonLabel(), constants.ConfirmButtonCustomID),
		discord.NewSuccessButton(b.getClearButtonLabel(), constants.ClearButtonCustomID),
		discord.NewSecondaryButton("Skip", constants.SkipButtonCustomID),
	))

	return components
}

// getDescription returns the description field for the embed.
func (b *ReviewBuilder) getDescription() string {
	description := b.group.Description

	// Check if description is empty
	if description == "" {
		return constants.NotApplicable
	}

	// Prepare description
	description = utils.TruncateString(description, 400)
	description = utils.FormatString(description)
	description = utils.CensorStringsInText(
		description,
		b.privacyMode,
		strconv.FormatUint(b.group.ID, 10),
		b.group.Name,
		strconv.FormatUint(b.group.Owner.UserID, 10),
	)

	return description
}

// getShout returns the shout field for the embed.
func (b *ReviewBuilder) getShout() string {
	// Skip if shout is not available
	if b.group.Shout == nil || b.group.Shout.Body == "" {
		return constants.NotApplicable
	}

	// Prepare shout
	shout := utils.TruncateString(b.group.Shout.Body, 400)
	shout = utils.FormatString(shout)

	return shout
}

// getReviewHistory returns the review history field for the embed.
func (b *ReviewBuilder) getReviewHistory() string {
	logs, nextCursor, err := b.db.Models().Activities().GetLogs(
		context.Background(),
		types.ActivityFilter{
			GroupID:      b.group.ID,
			ReviewerID:   0,
			ActivityType: enum.ActivityTypeAll,
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
	if b.reviewMode == enum.ReviewModeTraining {
		return "Report"
	}
	return "Confirm"
}

// getClearButtonLabel returns the appropriate label for the clear button based on review mode.
func (b *ReviewBuilder) getClearButtonLabel() string {
	if b.reviewMode == enum.ReviewModeTraining {
		return "Safe"
	}
	return "Clear"
}
