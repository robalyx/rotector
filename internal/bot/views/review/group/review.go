package group

import (
	"bytes"
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

	db             database.Client
	group          *types.ReviewGroup
	groupInfo      *apiTypes.GroupResponse
	unsavedReasons map[enum.GroupReasonType]struct{}
	flaggedCount   int
	defaultSort    enum.ReviewSortBy
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
		},
		db:             db,
		group:          session.GroupTarget.Get(s),
		groupInfo:      session.GroupInfo.Get(s),
		unsavedReasons: session.UnsavedGroupReasons.Get(s),
		flaggedCount:   session.GroupFlaggedMembersCount.Get(s),
		defaultSort:    session.UserGroupDefaultSort.Get(s),
	}
}

// Build creates a Discord message with group information.
func (b *ReviewBuilder) Build() *discord.MessageUpdateBuilder {
	builder := discord.NewMessageUpdateBuilder()

	// Create main info container
	mainInfoDisplays := []discord.ContainerSubComponent{
		b.buildGroupInfoSection(),
		discord.NewLargeSeparator(),
		discord.NewSection(
			discord.NewTextDisplay(fmt.Sprintf("-# UUID: %s\n-# Created: %s • Updated: %s",
				b.group.UUID.String(),
				fmt.Sprintf("<t:%d:R>", b.group.LastUpdated.Unix()),
				fmt.Sprintf("<t:%d:R>", b.group.LastUpdated.Unix()),
			)),
		).WithAccessory(
			discord.NewLinkButton("View Group", fmt.Sprintf("https://www.roblox.com/communities/%d", b.group.ID)).
				WithEmoji(discord.ComponentEmoji{Name: "🔗"}),
		),
	}

	// Add status-specific timestamps if they exist
	if !b.group.VerifiedAt.IsZero() || !b.group.ClearedAt.IsZero() {
		var timestamps []string
		if !b.group.VerifiedAt.IsZero() {
			timestamps = append(timestamps, "Verified: "+fmt.Sprintf("<t:%d:R>", b.group.VerifiedAt.Unix()))
		}

		if !b.group.ClearedAt.IsZero() {
			timestamps = append(timestamps, "Cleared: "+fmt.Sprintf("<t:%d:R>", b.group.ClearedAt.Unix()))
		}

		mainInfoDisplays = append(mainInfoDisplays,
			discord.NewTextDisplay("-# "+strings.Join(timestamps, " • ")),
		)
	}

	mainContainer := discord.NewContainer(mainInfoDisplays...).
		WithAccentColor(utils.GetContainerColor(b.PrivacyMode))

	// Create review info container
	var reviewInfoDisplays []discord.ContainerSubComponent

	// Add reason section with evidence
	reviewInfoDisplays = append(reviewInfoDisplays, b.buildReasonDisplay())

	// Add reason management dropdown for reviewers
	if b.IsReviewer {
		reviewInfoDisplays = append(reviewInfoDisplays,
			discord.NewActionRow(
				discord.NewStringSelectMenu(constants.ReasonSelectMenuCustomID, "Manage Reasons", b.buildReasonOptions()...),
			),
		)
	}

	// Create review container if we have any review info
	var reviewContainer discord.ContainerComponent
	if len(reviewInfoDisplays) > 0 {
		reviewContainer = discord.NewContainer(reviewInfoDisplays...).
			WithAccentColor(utils.GetContainerColor(b.PrivacyMode))
	}

	// Add mode display with warnings and action rows
	modeDisplays := b.buildStatusDisplay()
	modeDisplays = append(modeDisplays,
		discord.NewLargeSeparator(),
		discord.NewActionRow(
			discord.NewStringSelectMenu(constants.SortOrderSelectMenuCustomID, "Sorting", b.BuildSortingOptions(b.defaultSort)...),
		),
		discord.NewActionRow(
			discord.NewStringSelectMenu(constants.ActionSelectMenuCustomID, "Other Actions", b.buildActionOptions()...),
		),
	)

	// Add navigation and action buttons
	modeDisplays = append(modeDisplays, b.buildInteractiveComponents()...)

	modeContainer := discord.NewContainer(modeDisplays...).
		WithAccentColor(utils.GetContainerColor(b.PrivacyMode))

	// Add containers to builder
	builder.AddComponents(mainContainer)

	if len(reviewInfoDisplays) > 0 {
		builder.AddComponents(reviewContainer)
	}

	builder.AddComponents(modeContainer)

	// Handle thumbnail
	if b.group.ThumbnailURL == "" || b.group.ThumbnailURL == fetcher.ThumbnailPlaceholder {
		builder.AddFiles(discord.NewFile("content_deleted.png", "", bytes.NewReader(assets.ContentDeleted)))
	}

	return builder
}

// buildStatusDisplay creates the review mode info display with warnings and notices.
func (b *ReviewBuilder) buildStatusDisplay() []discord.ContainerSubComponent {
	displays := []discord.ContainerSubComponent{
		discord.NewTextDisplay(b.buildReviewModeText()),
		discord.NewLargeSeparator(),
		discord.NewTextDisplay(b.buildStatusText()),
	}

	var content strings.Builder

	// Add deletion notice if group is deleted
	if b.group.IsDeleted {
		content.WriteString(
			"\n\n## 🗑️ Data Deletion Notice\nThis group has requested deletion of their data. Some information may be missing or incomplete.")
	}

	// Add warning if there are recent reviewers
	if warningDisplay := b.BuildReviewWarningText("group", enum.ActivityTypeGroupViewed); warningDisplay != "" {
		content.WriteString("\n\n## ⚠️ Active Review Warning\n" + warningDisplay)
	}

	// Add comments if any exist
	if len(b.Comments) > 0 {
		content.WriteString("\n\n" + b.BuildCommentsText())
	}

	if content.Len() > 0 {
		displays = append(displays,
			discord.NewLargeSeparator(),
			discord.NewTextDisplay(content.String()))
	}

	return displays
}

// buildStatusText formats the status section with description.
func (b *ReviewBuilder) buildStatusText() string {
	var (
		statusIcon string
		statusName string
		statusDesc string
	)

	switch b.group.Status {
	case enum.GroupTypeConfirmed:
		statusIcon = "⚠️"
		statusName = "Confirmed"
		statusDesc = "This group has been confirmed to have inappropriate content or behavior"
	case enum.GroupTypeFlagged:
		statusIcon = "⏳"
		statusName = "Flagged"
		statusDesc = "This group has been flagged for review but no final decision has been made"
	case enum.GroupTypeCleared:
		statusIcon = "✅"
		statusName = "Cleared"
		statusDesc = "This group has been reviewed and cleared of any violations"
	}

	if b.group.IsLocked {
		statusIcon += "🔒"
		statusDesc += " and is currently locked from accepting new members"
	}

	return fmt.Sprintf("## %s %s\n%s", statusIcon, statusName, statusDesc)
}

// buildReviewModeText formats the review mode section with description.
func (b *ReviewBuilder) buildReviewModeText() string {
	// Format review mode
	var (
		mode        string
		description string
	)

	switch b.ReviewMode {
	case enum.ReviewModeStandard:
		mode = "⚠️ Standard Mode"
		description = "Your actions are recorded and affect the database. Please review carefully before taking action."
	default:
		mode = "❌ Unknown Mode"
		description = "Error encountered. Please check your settings."
	}

	return fmt.Sprintf("## %s\n%s", mode, description)
}

// buildGroupInfoSection creates the main group information section with thumbnail.
func (b *ReviewBuilder) buildGroupInfoSection() discord.ContainerSubComponent {
	// Add basic info with status
	var content strings.Builder

	// Add name header
	content.WriteString(fmt.Sprintf("## %s\n",
		utils.CensorString(b.group.Name, b.PrivacyMode)))

	// Add owner info
	ownerID := strconv.FormatInt(b.group.Owner.UserID, 10)
	content.WriteString(fmt.Sprintf("-# Owner: [%s](https://www.roblox.com/users/%d/profile)\n",
		utils.CensorString(ownerID, b.PrivacyMode), b.group.Owner.UserID))
	content.WriteString(fmt.Sprintf("-# Members: %s\n", strconv.FormatInt(b.groupInfo.MemberCount, 10)))
	content.WriteString("-# Flagged Members: " + strconv.Itoa(b.flaggedCount))

	// Add description
	content.WriteString("\n### 📝 Description\n")
	content.WriteString(b.getDescription())

	// Add shout
	content.WriteString("\n### 📢 Group Shout\n")
	content.WriteString(b.getShout())

	// Create main section with thumbnail
	section := discord.NewSection(discord.NewTextDisplay(content.String()))
	if b.group.ThumbnailURL != "" && b.group.ThumbnailURL != fetcher.ThumbnailPlaceholder {
		section = section.WithAccessory(discord.NewThumbnail(b.group.ThumbnailURL))
	} else {
		section = section.WithAccessory(discord.NewThumbnail("attachment://content_deleted.png"))
	}

	return section
}

// buildReasonDisplay creates the reason section with evidence.
func (b *ReviewBuilder) buildReasonDisplay() discord.ContainerSubComponent {
	var content strings.Builder
	content.WriteString("## Reasons and Evidence\n")

	if len(b.group.Reasons) == 0 {
		content.WriteString("No reasons have been added yet.")
		return discord.NewTextDisplay(content.String())
	}

	content.WriteString(fmt.Sprintf("-# Total Confidence: %.2f%%\n\n", b.group.Confidence*100))

	// Calculate dynamic truncation length based on number of reasons
	maxLength := utils.CalculateDynamicTruncationLength(len(b.group.Reasons))

	for _, reasonType := range []enum.GroupReasonType{
		enum.GroupReasonTypeMember,
		enum.GroupReasonTypePurpose,
		enum.GroupReasonTypeDescription,
		enum.GroupReasonTypeShout,
	} {
		if reason, ok := b.group.Reasons[reasonType]; ok {
			// Add reason header and message
			message := utils.CensorStringsInText(reason.Message, b.PrivacyMode,
				strconv.FormatInt(b.group.ID, 10),
				b.group.Name)
			message = utils.TruncateString(message, maxLength)
			message = utils.FormatString(message)

			// Check if this reason is unsaved and add indicator
			reasonTitle := reasonType.String()
			if _, isUnsaved := b.unsavedReasons[reasonType]; isUnsaved {
				reasonTitle += "*"
			}

			content.WriteString(fmt.Sprintf("%s **%s** [%.0f%%]\n%s",
				getReasonEmoji(reasonType),
				reasonTitle,
				reason.Confidence*100,
				message))

			// Add evidence if any
			if len(reason.Evidence) > 0 {
				content.WriteString("\n")

				for i, evidence := range reason.Evidence {
					if i >= 3 {
						content.WriteString("... and more\n")
						break
					}

					evidence = utils.TruncateString(evidence, 100)

					evidence = utils.NormalizeString(evidence)
					if b.PrivacyMode {
						evidence = utils.CensorStringsInText(evidence, true,
							strconv.FormatInt(b.group.ID, 10),
							b.group.Name)
					}

					content.WriteString(fmt.Sprintf("- `%s`\n", evidence))
				}
			}

			content.WriteString("\n")
		}
	}

	return discord.NewTextDisplay(content.String())
}

// buildInteractiveComponents creates the navigation and action buttons.
func (b *ReviewBuilder) buildInteractiveComponents() []discord.ContainerSubComponent {
	// Add navigation buttons
	navButtons := b.BuildNavigationButtons()
	confirmButton := discord.NewDangerButton("Confirm", constants.ConfirmButtonCustomID)
	clearButton := discord.NewSuccessButton("Clear", constants.ClearButtonCustomID)

	// Add all buttons to a single row
	allButtons := make([]discord.InteractiveComponent, 0, len(navButtons)+2)
	allButtons = append(allButtons, navButtons...)
	allButtons = append(allButtons, confirmButton, clearButton)

	return []discord.ContainerSubComponent{
		discord.NewActionRow(allButtons...),
	}
}

// buildActionOptions creates the action menu options.
func (b *ReviewBuilder) buildActionOptions() []discord.StringSelectMenuOption {
	options := []discord.StringSelectMenuOption{
		discord.NewStringSelectMenuOption("View Members", constants.GroupViewMembersButtonCustomID).
			WithEmoji(discord.ComponentEmoji{Name: "👥"}).
			WithDescription("View group members"),
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

// buildReasonOptions creates the reason management options.
func (b *ReviewBuilder) buildReasonOptions() []discord.StringSelectMenuOption {
	reasonTypes := []enum.GroupReasonType{
		enum.GroupReasonTypeMember,
		enum.GroupReasonTypePurpose,
		enum.GroupReasonTypeDescription,
		enum.GroupReasonTypeShout,
	}

	return shared.BuildReasonOptions(b.group.Reasons, reasonTypes, getReasonEmoji, b.ReasonsChanged)
}

// getDescription returns the description field.
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
		strconv.FormatInt(b.group.ID, 10),
		b.group.Name,
		strconv.FormatInt(b.group.Owner.UserID, 10),
	)
	description = utils.TruncateString(description, 400)
	description = utils.FormatString(description)

	return description
}

// getShout returns the shout field.
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

// getReasonEmoji returns the appropriate emoji for a reason type.
func getReasonEmoji(reasonType enum.GroupReasonType) string {
	switch reasonType {
	case enum.GroupReasonTypeMember:
		return "👥"
	case enum.GroupReasonTypePurpose:
		return "🎯"
	case enum.GroupReasonTypeDescription:
		return "📝"
	case enum.GroupReasonTypeShout:
		return "📢"
	default:
		return "❓"
	}
}
