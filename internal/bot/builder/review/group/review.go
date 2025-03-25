package group

import (
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
	db             database.Client
	botSettings    *types.BotSetting
	group          *types.ReviewGroup
	groupInfo      *apiTypes.GroupResponse
	logs           []*types.ActivityLog
	comments       []*types.Comment
	flaggedCount   int
	reviewMode     enum.ReviewMode
	defaultSort    enum.ReviewSortBy
	reviewHistory  []uint64
	userID         uint64
	historyIndex   int
	logsHasMore    bool
	reasonsChanged bool
	isAdmin        bool
	privacyMode    bool
}

// NewReviewBuilder creates a new review builder.
func NewReviewBuilder(s *session.Session, db database.Client) *ReviewBuilder {
	userID := session.UserID.Get(s)
	return &ReviewBuilder{
		db:             db,
		botSettings:    s.BotSettings(),
		group:          session.GroupTarget.Get(s),
		groupInfo:      session.GroupInfo.Get(s),
		logs:           session.ReviewLogs.Get(s),
		comments:       session.ReviewComments.Get(s),
		flaggedCount:   session.GroupFlaggedMembersCount.Get(s),
		reviewMode:     session.UserReviewMode.Get(s),
		defaultSort:    session.UserGroupDefaultSort.Get(s),
		reviewHistory:  session.GroupReviewHistory.Get(s),
		userID:         userID,
		historyIndex:   session.GroupReviewHistoryIndex.Get(s),
		logsHasMore:    session.ReviewLogsHasMore.Get(s),
		reasonsChanged: session.ReasonsChanged.Get(s),
		isAdmin:        s.BotSettings().IsAdmin(userID),
		privacyMode:    session.UserStreamerMode.Get(s),
	}
}

// Build creates a Discord message with group information in an embed and adds
// interactive components for reviewing the group.
func (b *ReviewBuilder) Build() *discord.MessageUpdateBuilder {
	builder := discord.NewMessageUpdateBuilder()

	// Add recent comments embed if there are any
	if commentsEmbed := b.buildCommentsEmbed(); commentsEmbed != nil {
		builder.AddEmbeds(commentsEmbed.Build())
	}

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

	// Add deletion notice if group is deleted
	if b.group.IsDeleted {
		deletionEmbed := b.buildDeletionEmbed()
		builder.AddEmbeds(modeEmbed.Build(), reviewEmbed.Build(), deletionEmbed.Build())
	} else {
		builder.AddEmbeds(modeEmbed.Build(), reviewEmbed.Build())
	}

	// Add recent comments embed if there are any
	if commentsEmbed := b.buildCommentsEmbed(); commentsEmbed != nil {
		builder.AddEmbeds(commentsEmbed.Build())
	}

	// Add warning embed if there are recent reviewers
	if warningEmbed := b.buildReviewWarningEmbed(); warningEmbed != nil {
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
	case b.reviewMode == enum.ReviewModeTraining || !b.isAdmin:
		mode = "🗳️ Voting Mode"
		description += "**You are an official reviewer.**\n" +
			"While you can review and vote on groups, only administrators can make final decisions. " +
			"Your votes are recorded but will not affect the group's status. Community members who are " +
			"not official reviewers will not be able to see this group review menu."
	case b.reviewMode == enum.ReviewModeStandard:
		mode = "⚠️ Standard Mode"
		description += "Your actions are recorded and affect the database. Please review carefully before taking action."
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
		utils.CensorString(groupID, b.privacyMode),
		b.group.ID,
	), true).
		AddField("Name", utils.CensorString(b.group.Name, b.privacyMode), true).
		AddField("Owner", fmt.Sprintf(
			"[%s](https://www.roblox.com/users/%d/profile)",
			utils.CensorString(ownerID, b.privacyMode),
			b.group.Owner.UserID,
		), true).
		AddField("Members", memberCount, true).
		AddField("Flagged Members", flaggedCount, true).
		AddField("Confidence", confidence, true).
		AddField("Last Updated", lastUpdated, true).
		AddField("Reason", b.getReason(), false).
		AddField("Shout", b.getShout(), false).
		AddField("Description", b.getDescription(), false)
	b.addEvidenceFields(embed)
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
	if len(b.reviewHistory) > 0 {
		footerText = fmt.Sprintf("%s • UUID: %s • History: %d/%d",
			status,
			b.group.UUID.String(),
			b.historyIndex+1,
			len(b.reviewHistory))
	} else {
		footerText = fmt.Sprintf("%s • UUID: %s", status, b.group.UUID.String())
	}
	embed.SetFooter(footerText, "")

	return embed
}

// buildDeletionEmbed creates an embed notifying that the group has requested data deletion.
func (b *ReviewBuilder) buildDeletionEmbed() *discord.EmbedBuilder {
	return discord.NewEmbedBuilder().
		SetTitle("🗑️ Data Deletion Notice").
		SetDescription("This group has requested deletion of their data. Some information may be missing or incomplete.").
		SetColor(constants.ErrorEmbedColor)
}

// buildCommentsEmbed creates an embed showing recent comments if any exist.
func (b *ReviewBuilder) buildCommentsEmbed() *discord.EmbedBuilder {
	if len(b.comments) == 0 {
		return nil
	}

	embed := discord.NewEmbedBuilder().
		SetTitle("📝 Recent Community Notes").
		SetColor(utils.GetMessageEmbedColor(b.privacyMode))

	// Take up to 3 most recent comments
	numComments := min(3, len(b.comments))
	for i := range numComments {
		comment := b.comments[i]
		timestamp := fmt.Sprintf("<t:%d:R>", comment.CreatedAt.Unix())

		// Determine user role
		var roleTitle string
		switch {
		case b.botSettings.IsAdmin(comment.CommenterID):
			roleTitle = "Administrator Note"
		case b.botSettings.IsReviewer(comment.CommenterID):
			roleTitle = "Reviewer Note"
		default:
			roleTitle = "Community Note"
		}

		// Add field for each comment
		embed.AddField(
			roleTitle,
			fmt.Sprintf("From <@%d> - %s\n```%s```",
				comment.CommenterID,
				timestamp,
				utils.TruncateString(comment.Message, 52),
			),
			false,
		)
	}

	if len(b.comments) > 3 {
		embed.SetFooter(fmt.Sprintf("... and %d more notes", len(b.comments)-3), "")
	}

	return embed
}

// buildReviewWarningEmbed creates a warning embed if another reviewer is reviewing the group.
func (b *ReviewBuilder) buildReviewWarningEmbed() *discord.EmbedBuilder {
	// Check for recent views in the logs
	fiveMinutesAgo := time.Now().Add(-5 * time.Minute)
	var recentReviewers []uint64

	for _, log := range b.logs {
		if log.ActivityType == enum.ActivityTypeGroupViewed &&
			log.ActivityTimestamp.After(fiveMinutesAgo) &&
			log.ReviewerID != b.userID &&
			b.botSettings.IsReviewer(log.ReviewerID) {
			recentReviewers = append(recentReviewers, log.ReviewerID)
		}
	}

	if len(recentReviewers) == 0 {
		return nil
	}

	// Create reviewer mentions
	mentions := make([]string, len(recentReviewers))
	for i, reviewerID := range recentReviewers {
		mentions[i] = fmt.Sprintf("<@%d>", reviewerID)
	}

	return discord.NewEmbedBuilder().
		SetTitle("⚠️ Active Review Warning").
		SetDescription(fmt.Sprintf(
			"This group was recently viewed by official reviewer%s %s. They may be actively reviewing this group. "+
				"Please coordinate with them before taking any actions to avoid conflicts.",
			map[bool]string{true: "s", false: ""}[len(recentReviewers) > 1],
			strings.Join(mentions, ", "),
		)).
		SetColor(constants.ErrorEmbedColor)
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
		discord.NewStringSelectMenuOption("Selected by last viewed", enum.ReviewSortByLastViewed.String()).
			WithDefault(b.defaultSort == enum.ReviewSortByLastViewed).
			WithEmoji(discord.ComponentEmoji{Name: "👁️"}),
	}
}

// buildActionOptions creates the action menu options.
func (b *ReviewBuilder) buildActionOptions() []discord.StringSelectMenuOption {
	return []discord.StringSelectMenuOption{
		discord.NewStringSelectMenuOption("View Flagged Members", constants.GroupViewMembersButtonCustomID).
			WithDescription("View all flagged members of this group").
			WithEmoji(discord.ComponentEmoji{Name: "👥"}),
		discord.NewStringSelectMenuOption("View community notes", constants.ViewCommentsButtonCustomID).
			WithEmoji(discord.ComponentEmoji{Name: "📝"}).
			WithDescription("View and manage community notes"),
		discord.NewStringSelectMenuOption("Ask AI about group", constants.OpenAIChatButtonCustomID).
			WithEmoji(discord.ComponentEmoji{Name: "🤖"}).
			WithDescription("Ask the AI questions about this group"),
		discord.NewStringSelectMenuOption("View group logs", constants.GroupViewLogsButtonCustomID).
			WithEmoji(discord.ComponentEmoji{Name: "📋"}).
			WithDescription("View activity logs for this group"),
		discord.NewStringSelectMenuOption("Change Review Mode", constants.ReviewModeOption).
			WithEmoji(discord.ComponentEmoji{Name: "🗳️"}).
			WithDescription("Switch between voting (training) and standard modes"),
		discord.NewStringSelectMenuOption("Change Review Target", constants.ReviewTargetModeOption).
			WithEmoji(discord.ComponentEmoji{Name: "🎯"}).
			WithDescription("Change what type of groups to review"),
	}
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

	// Add reason management dropdown for reviewers
	if b.isAdmin && b.reviewMode != enum.ReviewModeTraining {
		components = append(components,
			discord.NewActionRow(
				discord.NewStringSelectMenu(constants.ReasonSelectMenuCustomID, "Manage Reasons", b.buildReasonOptions()...),
			),
		)
	}

	// Add action options menu
	components = append(components,
		discord.NewActionRow(
			discord.NewStringSelectMenu(constants.ActionSelectMenuCustomID, "Actions", b.buildActionOptions()...),
		),
	)

	// Create navigation buttons
	prevButton := discord.NewSecondaryButton("⬅️ Prev", constants.PrevReviewButtonCustomID)
	if b.historyIndex <= 0 || len(b.reviewHistory) == 0 {
		prevButton = prevButton.WithDisabled(true)
	}

	var nextButtonLabel string
	if b.historyIndex >= len(b.reviewHistory)-1 {
		nextButtonLabel = "Skip ➡️"
	} else {
		nextButtonLabel = "Next ➡️"
	}
	nextButton := discord.NewSecondaryButton(nextButtonLabel, constants.NextReviewButtonCustomID)

	// First action row with navigation and review buttons
	components = append(components, discord.NewActionRow(
		discord.NewSecondaryButton("◀️", constants.BackButtonCustomID),
		prevButton,
		nextButton,
		discord.NewDangerButton(b.getConfirmButtonLabel(), constants.ConfirmButtonCustomID),
		discord.NewSuccessButton(b.getClearButtonLabel(), constants.ClearButtonCustomID),
	))

	return components
}

// buildReasonOptions creates the reason management options.
func (b *ReviewBuilder) buildReasonOptions() []discord.StringSelectMenuOption {
	options := make([]discord.StringSelectMenuOption, 0)

	// Add refresh option if reasons have been changed
	if b.reasonsChanged {
		options = append(options, discord.NewStringSelectMenuOption(
			"Restore original reasons",
			constants.RefreshButtonCustomID,
		).WithEmoji(discord.ComponentEmoji{Name: "🔄"}).
			WithDescription("Reset all reasons back to their original state"))
	}

	// Define available reason types for groups
	reasonTypes := []enum.GroupReasonType{
		enum.GroupReasonTypeMember,
	}

	for _, reasonType := range reasonTypes {
		// Check if this reason type exists
		_, exists := b.group.Reasons[reasonType]

		var action string
		optionValue := reasonType.String()
		if exists {
			action = "Edit"
			optionValue += constants.ModalOpenSuffix
		} else {
			action = "Add"
			optionValue += constants.ModalOpenSuffix
		}

		options = append(options, discord.NewStringSelectMenuOption(
			fmt.Sprintf("%s %s reason", action, reasonType.String()),
			optionValue,
		).WithEmoji(discord.ComponentEmoji{Name: getReasonEmoji(reasonType)}))
	}

	return options
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

	for _, reasonType := range reasonTypes {
		if reason, ok := b.group.Reasons[reasonType]; ok {
			// Join all reasons of this type
			section := fmt.Sprintf("%s **%s**\n%s",
				getReasonEmoji(reasonType),
				reasonType.String(),
				reason.Message,
			)
			formattedReasons = append(formattedReasons, section)
		}
	}

	// Join all sections with double newlines for spacing
	reasonText := strings.Join(formattedReasons, "\n\n")

	// Censor if needed
	if b.privacyMode {
		reasonText = utils.CensorStringsInText(
			reasonText,
			true,
			strconv.FormatUint(b.group.ID, 10),
			b.group.Name,
		)
	}

	return reasonText
}

// addEvidenceFields adds separate evidence fields for the embed if any reasons have evidence.
func (b *ReviewBuilder) addEvidenceFields(embed *discord.EmbedBuilder) {
	var hasEvidence bool
	var fields []struct {
		name  string
		value string
	}

	// Collect evidence from all reasons
	for reasonType, reason := range b.group.Reasons {
		if len(reason.Evidence) > 0 {
			hasEvidence = true
			var evidenceItems []string

			// Add header for this reason type
			fieldName := fmt.Sprintf("%s Evidence", reasonType)

			// Add up to 5 evidence items
			for i, evidence := range reason.Evidence {
				if i >= 5 {
					evidenceItems = append(evidenceItems, "... and more")
					break
				}

				// Format and normalize the evidence
				evidence = utils.TruncateString(evidence, 100)
				evidence = utils.NormalizeString(evidence)

				// Censor if needed
				if b.privacyMode {
					evidence = utils.CensorStringsInText(
						evidence,
						true,
						strconv.FormatUint(b.group.ID, 10),
						b.group.Name,
					)
				}

				evidenceItems = append(evidenceItems, fmt.Sprintf("- `%s`", evidence))
			}

			fields = append(fields, struct {
				name  string
				value string
			}{
				name:  fieldName,
				value: strings.Join(evidenceItems, "\n"),
			})
		}
	}

	if !hasEvidence {
		return
	}

	// Add each evidence type as a separate field
	for _, field := range fields {
		embed.AddField(field.name, field.value, false)
	}
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
	if len(b.logs) == 0 {
		return constants.NotApplicable
	}

	history := make([]string, 0, len(b.logs))
	for _, log := range b.logs {
		history = append(history, fmt.Sprintf("- <@%d> (%s) - <t:%d:R>",
			log.ReviewerID, log.ActivityType.String(), log.ActivityTimestamp.Unix()))
	}

	if b.logsHasMore {
		history = append(history, "... and more")
	}

	return strings.Join(history, "\n")
}

// getConfirmButtonLabel returns the appropriate label for the confirm button based on review mode.
func (b *ReviewBuilder) getConfirmButtonLabel() string {
	if b.reviewMode == enum.ReviewModeTraining || !b.isAdmin {
		return "Report"
	}
	return "Confirm"
}

// getClearButtonLabel returns the appropriate label for the clear button based on review mode.
func (b *ReviewBuilder) getClearButtonLabel() string {
	if b.reviewMode == enum.ReviewModeTraining || !b.isAdmin {
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
