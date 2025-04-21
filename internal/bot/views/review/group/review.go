package group

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/disgoorg/disgo/discord"
	apiTypes "github.com/jaxron/roapi.go/pkg/api/types"
	"github.com/robalyx/rotector/assets"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/bot/utils"
	"github.com/robalyx/rotector/internal/bot/views/review/shared"
	"github.com/robalyx/rotector/internal/database"
	"github.com/robalyx/rotector/internal/database/types"
	"github.com/robalyx/rotector/internal/database/types/enum"
	"github.com/robalyx/rotector/internal/roblox/fetcher"
)

// ReviewBuilder creates the visual layout for reviewing a group.
type ReviewBuilder struct {
	shared.BaseReviewBuilder
	db           database.Client
	group        *types.ReviewGroup
	groupInfo    *apiTypes.GroupResponse
	flaggedCount int
	defaultSort  enum.ReviewSortBy
}

// NewReviewBuilder creates a new review builder.
func NewReviewBuilder(s *session.Session, db database.Client) *ReviewBuilder {
	reviewMode := session.UserReviewMode.Get(s)
	userID := session.UserID.Get(s)
	return &ReviewBuilder{
		BaseReviewBuilder: shared.BaseReviewBuilder{
			BotSettings:    s.BotSettings(),
			Logs:           session.ReviewLogs.Get(s),
			Comments:       session.ReviewComments.Get(s),
			ReviewMode:     reviewMode,
			ReviewHistory:  session.GroupReviewHistory.Get(s),
			UserID:         userID,
			HistoryIndex:   session.GroupReviewHistoryIndex.Get(s),
			LogsHasMore:    session.ReviewLogsHasMore.Get(s),
			ReasonsChanged: session.ReasonsChanged.Get(s),
			IsReviewer:     s.BotSettings().IsReviewer(userID),
			IsAdmin:        s.BotSettings().IsAdmin(userID),
			PrivacyMode:    session.UserStreamerMode.Get(s),
			TrainingMode:   reviewMode == enum.ReviewModeTraining,
		},
		db:           db,
		group:        session.GroupTarget.Get(s),
		groupInfo:    session.GroupInfo.Get(s),
		flaggedCount: session.GroupFlaggedMembersCount.Get(s),
		defaultSort:  session.UserGroupDefaultSort.Get(s),
	}
}

// Build creates a Discord message with group information in an embed and adds
// interactive components for reviewing the group.
func (b *ReviewBuilder) Build() *discord.MessageUpdateBuilder {
	builder := discord.NewMessageUpdateBuilder()

	// Add mode embed
	modeEmbed := b.buildModeEmbed()
	builder.AddEmbeds(modeEmbed.Build())

	// Handle thumbnail
	reviewEmbed := b.buildReviewEmbed()
	if b.group.ThumbnailURL != "" && b.group.ThumbnailURL != fetcher.ThumbnailPlaceholder {
		reviewEmbed.SetThumbnail(b.group.ThumbnailURL)
	} else {
		// Load and attach placeholder image
		placeholderImage, err := assets.Images.Open("images/content_deleted.png")
		if err == nil {
			builder.SetFiles(discord.NewFile("content_deleted.png", "", placeholderImage))
			reviewEmbed.SetThumbnail("attachment://content_deleted.png")
			_ = placeholderImage.Close()
		}
	}
	builder.AddEmbeds(reviewEmbed.Build())

	// Add recent comments embed if there are any
	if commentsEmbed := b.BuildCommentsEmbed(); commentsEmbed != nil {
		builder.AddEmbeds(commentsEmbed.Build())
	}

	// Add deletion notice if group is deleted
	if b.group.IsDeleted {
		deletionEmbed := b.BuildDeletionEmbed(enum.ActivityTypeGroupViewed)
		builder.AddEmbeds(deletionEmbed.Build())
	}

	// Add warning embed if there are recent reviewers
	if warningEmbed := b.BuildReviewWarningEmbed(enum.ActivityTypeGroupViewed); warningEmbed != nil {
		builder.AddEmbeds(warningEmbed.Build())
	}

	// Create components
	components := b.buildComponents()

	return builder.AddContainerComponents(components...)
}

// buildModeEmbed creates the review mode info embed.
func (b *ReviewBuilder) buildModeEmbed() *discord.EmbedBuilder {
	var mode string
	var description string

	// Format review mode
	switch {
	case b.ReviewMode == enum.ReviewModeTraining || !b.IsAdmin:
		mode = "🗳️ Voting Mode"
		description += "**You are an official reviewer.**\n" +
			"While you can review and vote on groups, only administrators can make final decisions. " +
			"Your votes are recorded but will not affect the group's status. Community members who are " +
			"not official reviewers will not be able to see this group review menu."
	case b.ReviewMode == enum.ReviewModeStandard:
		mode = "⚠️ Standard Mode"
		description += "Your actions are recorded and affect the database. Please review carefully before taking action."
	default:
		mode = "❌ Unknown Mode"
		description = "Error encountered. Please check your settings."
	}

	return discord.NewEmbedBuilder().
		SetTitle(mode).
		SetDescription(description).
		SetColor(utils.GetMessageEmbedColor(b.PrivacyMode))
}

// buildReviewEmbed creates the main review information embed.
func (b *ReviewBuilder) buildReviewEmbed() *discord.EmbedBuilder {
	embed := discord.NewEmbedBuilder().
		SetColor(utils.GetMessageEmbedColor(b.PrivacyMode)).
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
		status = "⏳ Pending"
	case enum.GroupTypeCleared:
		status = "✅ Cleared"
	}

	// Add locked status if applicable
	if b.group.IsLocked {
		status += " 🔒 Locked"
	}

	groupID := strconv.FormatUint(b.group.ID, 10)
	ownerID := strconv.FormatUint(b.group.Owner.UserID, 10)
	memberCount := strconv.FormatUint(b.groupInfo.MemberCount, 10)
	flaggedCount := strconv.Itoa(b.flaggedCount)
	lastUpdated := fmt.Sprintf("<t:%d:R>", b.group.LastUpdated.Unix())
	confidence := fmt.Sprintf("%.2f%%", b.group.Confidence*100)

	// Add all information fields to the embed
	embed.AddField("ID", fmt.Sprintf(
		"[%s](https://www.roblox.com/groups/%d)",
		utils.CensorString(groupID, b.PrivacyMode),
		b.group.ID,
	), true).
		AddField("Name", utils.CensorString(b.group.Name, b.PrivacyMode), true).
		AddField("Owner", fmt.Sprintf(
			"[%s](https://www.roblox.com/users/%d/profile)",
			utils.CensorString(ownerID, b.PrivacyMode),
			b.group.Owner.UserID,
		), true).
		AddField("Members", memberCount, true).
		AddField("Flagged Members", flaggedCount, true).
		AddField("Confidence", confidence, true).
		AddField("Last Updated", lastUpdated, true).
		AddField("Reason", b.getReason(), false).
		AddField("Shout", b.getShout(), false).
		AddField("Description", b.getDescription(), false)
	shared.AddEvidenceFields(
		embed, b.group.Reasons, b.PrivacyMode,
		strconv.FormatUint(b.group.ID, 10),
		b.group.Name,
		strconv.FormatUint(b.group.Owner.UserID, 10),
	)
	embed.AddField("Review History", b.getReviewHistory(), false)

	// Add status-specific timestamps
	if !b.group.VerifiedAt.IsZero() {
		embed.AddField("Verified At", fmt.Sprintf("<t:%d:R>", b.group.VerifiedAt.Unix()), true)
	}
	if !b.group.ClearedAt.IsZero() {
		embed.AddField("Cleared At", fmt.Sprintf("<t:%d:R>", b.group.ClearedAt.Unix()), true)
	}

	// Build footer with status and history position
	var footerText string
	if len(b.ReviewHistory) > 0 {
		footerText = fmt.Sprintf("%s • UUID: %s • History: %d/%d",
			status,
			b.group.UUID.String(),
			b.HistoryIndex+1,
			len(b.ReviewHistory))
	} else {
		footerText = fmt.Sprintf("%s • UUID: %s", status, b.group.UUID.String())
	}
	embed.SetFooter(footerText, "")

	return embed
}

// buildActionOptions creates the action menu options.
func (b *ReviewBuilder) buildActionOptions() []discord.StringSelectMenuOption {
	options := []discord.StringSelectMenuOption{
		discord.NewStringSelectMenuOption("View Flagged Members", constants.GroupViewMembersButtonCustomID).
			WithDescription("View all flagged members of this group").
			WithEmoji(discord.ComponentEmoji{Name: "👥"}),
	}

	// Add comment options
	options = append(options, b.BuildCommentOptions()...)

	// Add reviewer-only options
	if b.IsReviewer {
		reviewerOptions := []discord.StringSelectMenuOption{
			discord.NewStringSelectMenuOption("Ask AI about group", constants.OpenAIChatButtonCustomID).
				WithEmoji(discord.ComponentEmoji{Name: "🤖"}).
				WithDescription("Ask the AI questions about this group"),
			discord.NewStringSelectMenuOption("View group logs", constants.GroupViewLogsButtonCustomID).
				WithEmoji(discord.ComponentEmoji{Name: "📋"}).
				WithDescription("View activity logs for this group"),
			discord.NewStringSelectMenuOption("Change Review Mode", constants.ReviewModeOption).
				WithEmoji(discord.ComponentEmoji{Name: "🗳️"}).
				WithDescription("Switch between voting and standard modes"),
		}
		options = append(options, reviewerOptions...)
	}

	// Add last default option
	options = append(options,
		discord.NewStringSelectMenuOption("Change Review Target", constants.ReviewTargetModeOption).
			WithEmoji(discord.ComponentEmoji{Name: "🎯"}).
			WithDescription("Change what type of groups to review"),
	)

	return options
}

// buildComponents creates all interactive components for the review menu.
func (b *ReviewBuilder) buildComponents() []discord.ContainerComponent {
	// Get base components
	components := b.BuildBaseComponents(
		b.BuildSortingOptions(b.defaultSort),
		b.buildReasonOptions(),
		b.buildActionOptions(),
	)

	// Create all buttons
	navButtons := b.BuildNavigationButtons()
	confirmButton := discord.NewDangerButton(b.GetConfirmButtonLabel(), constants.ConfirmButtonCustomID)
	clearButton := discord.NewSuccessButton(b.GetClearButtonLabel(), constants.ClearButtonCustomID)

	// Combine all buttons into a single slice
	allButtons := make([]discord.InteractiveComponent, 0, len(navButtons)+2)
	allButtons = append(allButtons, navButtons...)
	allButtons = append(allButtons, confirmButton, clearButton)

	// Create action row with all buttons
	actionRow := discord.NewActionRow(allButtons...)
	components = append(components, actionRow)

	return components
}

// buildReasonOptions creates the reason management options.
func (b *ReviewBuilder) buildReasonOptions() []discord.StringSelectMenuOption {
	reasonTypes := []enum.GroupReasonType{
		enum.GroupReasonTypeMember,
	}
	return shared.BuildReasonOptions(b.group.Reasons, reasonTypes, getReasonEmoji, b.ReasonsChanged)
}

// getDescription returns the description field for the embed.
func (b *ReviewBuilder) getDescription() string {
	description := b.group.Description

	// Check if description is empty
	if description == "" {
		return constants.NotApplicable
	}

	// Prepare description
	description = utils.CensorStringsInText(
		description,
		b.PrivacyMode,
		strconv.FormatUint(b.group.ID, 10),
		b.group.Name,
		strconv.FormatUint(b.group.Owner.UserID, 10),
	)
	description = utils.TruncateString(description, 400)
	description = utils.FormatString(description)

	return description
}

// getReason returns the formatted reason for the embed.
func (b *ReviewBuilder) getReason() string {
	if len(b.group.Reasons) == 0 {
		return constants.NotApplicable
	}

	// Build formatted output
	var formattedReasons []string

	// Order of reason types to display
	reasonTypes := []enum.GroupReasonType{
		enum.GroupReasonTypeMember,
	}

	// Calculate dynamic truncation length based on number of reasons
	maxLength := utils.CalculateDynamicTruncationLength(len(b.group.Reasons))

	for _, reasonType := range reasonTypes {
		if reason, ok := b.group.Reasons[reasonType]; ok {
			// Join all reasons of this type
			section := fmt.Sprintf("%s **%s**\n%s",
				getReasonEmoji(reasonType),
				reasonType.String(),
				utils.TruncateString(reason.Message, maxLength))
			formattedReasons = append(formattedReasons, section)
		}
	}

	// Join all sections with double newlines for spacing
	reasonText := strings.Join(formattedReasons, "\n\n")

	// Censor if needed
	if b.PrivacyMode {
		reasonText = utils.CensorStringsInText(
			reasonText,
			true,
			strconv.FormatUint(b.group.ID, 10),
			b.group.Name,
		)
	}

	return reasonText
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
	if len(b.Logs) == 0 {
		return constants.NotApplicable
	}

	history := make([]string, 0, len(b.Logs))
	for _, log := range b.Logs {
		history = append(history, fmt.Sprintf("- <@%d> (%s) - <t:%d:R>",
			log.ReviewerID, log.ActivityType.String(), log.ActivityTimestamp.Unix()))
	}

	if b.LogsHasMore {
		history = append(history, "... and more")
	}

	return strings.Join(history, "\n")
}

// GetConfirmButtonLabel returns the appropriate label for the confirm button based on review mode.
func (b *ReviewBuilder) GetConfirmButtonLabel() string {
	if b.ReviewMode == enum.ReviewModeTraining || !b.IsAdmin {
		return "Report"
	}
	return "Confirm"
}

// GetClearButtonLabel returns the appropriate label for the clear button based on review mode.
func (b *ReviewBuilder) GetClearButtonLabel() string {
	if b.ReviewMode == enum.ReviewModeTraining || !b.IsAdmin {
		return "Safe"
	}
	return "Clear"
}

// getReasonEmoji returns the appropriate emoji for a reason type.
func getReasonEmoji(reasonType enum.GroupReasonType) string {
	switch reasonType {
	case enum.GroupReasonTypeMember:
		return "👥"
	default:
		return "❓"
	}
}
