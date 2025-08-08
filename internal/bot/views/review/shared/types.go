package shared

import (
	"fmt"
	"strings"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/utils"
	"github.com/robalyx/rotector/internal/database/types"
	"github.com/robalyx/rotector/internal/database/types/enum"
)

// TargetType represents the type of target being reviewed.
type TargetType string

const (
	TargetTypeUser  TargetType = "user"
	TargetTypeGroup TargetType = "group"
)

// BaseReviewBuilder contains common fields used by both user and group review builders.
type BaseReviewBuilder struct {
	BotSettings    *types.BotSetting
	Logs           []*types.ActivityLog
	Comments       []*types.Comment
	ReviewMode     enum.ReviewMode
	ReviewHistory  []uint64
	UserID         uint64
	HistoryIndex   int
	LogsHasMore    bool
	ReasonsChanged bool
	IsReviewer     bool
	IsAdmin        bool
	TrainingMode   bool
	PrivacyMode    bool
}

// BuildCommentsText creates the comments text content.
func (b *BaseReviewBuilder) BuildCommentsText() string {
	if b.IsReviewer && !b.TrainingMode {
		return b.BuildReviewerCommentsText()
	}

	return b.BuildNonReviewerCommentsText()
}

// BuildReviewerCommentsText creates the comments text content for reviewers.
func (b *BaseReviewBuilder) BuildReviewerCommentsText() string {
	if len(b.Comments) == 0 {
		return ""
	}

	var content strings.Builder
	content.WriteString("## 📝 Recent Community Note\n\n")

	// Take the most recent comment
	comment := b.Comments[0]
	timestamp := fmt.Sprintf("<t:%d:R>", comment.CreatedAt.Unix())

	// Determine user role
	var roleTitle string

	switch {
	case b.BotSettings.IsAdmin(comment.CommenterID):
		roleTitle = "Administrator Note"
	case b.BotSettings.IsReviewer(comment.CommenterID):
		roleTitle = "Reviewer Note"
	default:
		roleTitle = "Community Note"
	}

	content.WriteString(fmt.Sprintf("**%s**\nFrom <@%d> - %s\n%s",
		roleTitle,
		comment.CommenterID,
		timestamp,
		utils.FormatString(utils.TruncateString(comment.Message, 256))))

	// Add remaining notes count if there are more
	if len(b.Comments) > 1 {
		content.WriteString(fmt.Sprintf("\n-# and %d more community notes", len(b.Comments)-1))
	}

	return content.String()
}

// BuildNonReviewerCommentsText creates the comments text content for non-reviewers.
func (b *BaseReviewBuilder) BuildNonReviewerCommentsText() string {
	if len(b.Comments) == 0 {
		return ""
	}

	var content strings.Builder
	content.WriteString("## 📝 Your Community Note\n\n")

	// Find user's comment
	var userComment *types.Comment

	for _, comment := range b.Comments {
		if comment.CommenterID == b.UserID {
			userComment = comment
			break
		}
	}

	if userComment == nil {
		return ""
	}

	timestamp := fmt.Sprintf("<t:%d:R>", userComment.CreatedAt.Unix())
	if !userComment.UpdatedAt.Equal(userComment.CreatedAt) {
		timestamp += fmt.Sprintf(" (edited <t:%d:R>)", userComment.UpdatedAt.Unix())
	}

	content.WriteString(fmt.Sprintf("%s\n%s",
		timestamp,
		utils.FormatString(userComment.Message)))

	return content.String()
}

// BuildSortingOptions creates the sorting options.
func (b *BaseReviewBuilder) BuildSortingOptions(defaultSort enum.ReviewSortBy) []discord.StringSelectMenuOption {
	return []discord.StringSelectMenuOption{
		discord.NewStringSelectMenuOption("Selected by random", enum.ReviewSortByRandom.String()).
			WithDefault(defaultSort == enum.ReviewSortByRandom).
			WithEmoji(discord.ComponentEmoji{Name: "🔀"}),
		discord.NewStringSelectMenuOption("Selected by confidence", enum.ReviewSortByConfidence.String()).
			WithDefault(defaultSort == enum.ReviewSortByConfidence).
			WithEmoji(discord.ComponentEmoji{Name: "🔮"}),
		discord.NewStringSelectMenuOption("Selected by least recently updated", enum.ReviewSortByLastUpdated.String()).
			WithDefault(defaultSort == enum.ReviewSortByLastUpdated).
			WithEmoji(discord.ComponentEmoji{Name: "📅"}),
		discord.NewStringSelectMenuOption("Selected by most recently updated", enum.ReviewSortByRecentlyUpdated.String()).
			WithDefault(defaultSort == enum.ReviewSortByRecentlyUpdated).
			WithEmoji(discord.ComponentEmoji{Name: "🔄"}),
		discord.NewStringSelectMenuOption("Selected by last viewed", enum.ReviewSortByLastViewed.String()).
			WithDefault(defaultSort == enum.ReviewSortByLastViewed).
			WithEmoji(discord.ComponentEmoji{Name: "👁️"}),
	}
}

// BuildNavigationButtons creates navigation buttons for review menus.
func (b *BaseReviewBuilder) BuildNavigationButtons() []discord.InteractiveComponent {
	prevButton := discord.NewSecondaryButton("⬅️ Prev", constants.PrevReviewButtonCustomID)
	if b.HistoryIndex <= 0 || len(b.ReviewHistory) == 0 {
		prevButton = prevButton.WithDisabled(true)
	}

	var nextButtonLabel string
	if b.HistoryIndex >= len(b.ReviewHistory)-1 {
		nextButtonLabel = "Skip ➡️"
	} else {
		nextButtonLabel = "Next ➡️"
	}

	nextButton := discord.NewSecondaryButton(nextButtonLabel, constants.NextReviewButtonCustomID)

	return []discord.InteractiveComponent{
		discord.NewSecondaryButton("◀️", constants.BackButtonCustomID),
		prevButton,
		nextButton,
	}
}

// BuildCommentOptions creates the comment options based on user permissions and state.
func (b *BaseReviewBuilder) BuildCommentOptions() []discord.StringSelectMenuOption {
	var options []discord.StringSelectMenuOption

	// Add comment options based on reviewer status
	if b.IsReviewer && b.ReviewMode != enum.ReviewModeTraining {
		options = append(options,
			discord.NewStringSelectMenuOption("View community notes", constants.ViewCommentsButtonCustomID).
				WithEmoji(discord.ComponentEmoji{Name: "📝"}).
				WithDescription("View and manage community notes"),
		)
	} else {
		// Check if user has an existing comment
		var hasComment bool

		for _, comment := range b.Comments {
			if comment.CommenterID == b.UserID {
				hasComment = true
				break
			}
		}

		if hasComment {
			options = append(options,
				discord.NewStringSelectMenuOption("Delete your community note", constants.DeleteCommentButtonCustomID).
					WithEmoji(discord.ComponentEmoji{Name: "📝"}).
					WithDescription("Delete your note"),
			)
		} else if len(b.Comments) < constants.CommentLimit {
			options = append(options,
				discord.NewStringSelectMenuOption("Add community note", constants.AddCommentButtonCustomID).
					WithEmoji(discord.ComponentEmoji{Name: "📝"}).
					WithDescription("Add a note to help reviewers understand the user"),
			)
		}
	}

	return options
}

// BuildBaseComponents creates the common base components for review menus.
func (b *BaseReviewBuilder) BuildBaseComponents(
	sortOptions []discord.StringSelectMenuOption,
	reasonOptions []discord.StringSelectMenuOption,
	actionOptions []discord.StringSelectMenuOption,
) []discord.LayoutComponent {
	var components []discord.LayoutComponent

	// Add sorting options
	components = append(components,
		discord.NewActionRow(
			discord.NewStringSelectMenu(constants.SortOrderSelectMenuCustomID, "Sorting", sortOptions...),
		),
	)

	// Add reason management dropdown for reviewers
	if b.IsReviewer && b.ReviewMode != enum.ReviewModeTraining {
		components = append(components,
			discord.NewActionRow(
				discord.NewStringSelectMenu(constants.ReasonSelectMenuCustomID, "Manage Reasons", reasonOptions...),
			),
		)
	}

	// Add action options menu
	components = append(components,
		discord.NewActionRow(
			discord.NewStringSelectMenu(constants.ActionSelectMenuCustomID, "Actions", actionOptions...),
		),
	)

	return components
}

// BuildDeletionDisplay creates the deletion notice display.
func (b *BaseReviewBuilder) BuildDeletionDisplay(targetType string) discord.ContainerSubComponent {
	return discord.NewTextDisplay(fmt.Sprintf(
		"## 🗑️ Data Deletion Notice\nThis %s has requested deletion of their data. Some information may be missing or incomplete.",
		targetType))
}

// BuildReviewWarningDisplay creates the review warning display.
func (b *BaseReviewBuilder) BuildReviewWarningDisplay(targetType string, activityType enum.ActivityType) discord.ContainerSubComponent {
	// Check for recent views in the logs
	fiveMinutesAgo := time.Now().Add(-5 * time.Minute)

	var recentReviewers []uint64

	for _, log := range b.Logs {
		if log.ActivityType == activityType &&
			log.ActivityTimestamp.After(fiveMinutesAgo) &&
			log.ReviewerID != b.UserID &&
			b.BotSettings.IsReviewer(log.ReviewerID) {
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

	// Build the warning text
	var content strings.Builder
	content.WriteString(fmt.Sprintf(
		"## ⚠️ Active Review Warning\nThis %s was recently viewed by official reviewer%s %s. ",
		targetType,
		map[bool]string{true: "s", false: ""}[len(recentReviewers) > 1],
		strings.Join(mentions, ", ")))
	content.WriteString(fmt.Sprintf(
		"They may be actively reviewing this %s. Please coordinate with them before taking any actions to avoid conflicts.",
		targetType))

	return discord.NewTextDisplay(content.String())
}

// BuildFooterText builds the footer text with status and history position.
func (b *BaseReviewBuilder) BuildFooterText(status, uuid string) string {
	if len(b.ReviewHistory) > 0 {
		return fmt.Sprintf("%s • UUID: %s • History: %d/%d",
			status,
			uuid,
			b.HistoryIndex+1,
			len(b.ReviewHistory))
	}

	return fmt.Sprintf("%s • UUID: %s", status, uuid)
}

// BuildReviewWarningText creates the review warning text for both users and groups.
func (b *BaseReviewBuilder) BuildReviewWarningText(targetType string, activityType enum.ActivityType) string {
	// Check for recent views in the logs
	fiveMinutesAgo := time.Now().Add(-5 * time.Minute)

	var recentReviewers []uint64

	for _, log := range b.Logs {
		if log.ActivityType == activityType &&
			log.ActivityTimestamp.After(fiveMinutesAgo) &&
			log.ReviewerID != b.UserID &&
			b.BotSettings.IsReviewer(log.ReviewerID) {
			recentReviewers = append(recentReviewers, log.ReviewerID)
		}
	}

	if len(recentReviewers) == 0 {
		return ""
	}

	// Create reviewer mentions
	mentions := make([]string, len(recentReviewers))
	for i, reviewerID := range recentReviewers {
		mentions[i] = fmt.Sprintf("<@%d>", reviewerID)
	}

	return fmt.Sprintf("This %s was recently viewed by official reviewer%s %s. "+
		"They may be actively reviewing this %s. Please coordinate with them before taking any actions to avoid conflicts.",
		targetType,
		map[bool]string{true: "s", false: ""}[len(recentReviewers) > 1],
		strings.Join(mentions, ", "),
		targetType)
}

// BuildSingleReasonDisplay creates a display for a single reason with evidence.
func BuildSingleReasonDisplay[T types.ReasonType](
	privacyMode bool, reasonType T, reason *types.Reason, maxLength int, sensitiveInfo ...string,
) string {
	var content strings.Builder

	// Format the message
	message := utils.CensorStringsInText(reason.Message, privacyMode, sensitiveInfo...)
	message = utils.TruncateString(message, maxLength)
	message = utils.FormatString(message)

	// Build the header with emoji and confidence
	content.WriteString(fmt.Sprintf("### %s Reason [%.0f%%]\n", reasonType.String(), reason.Confidence*100))
	content.WriteString(message)

	// Add evidence if any exists
	if len(reason.Evidence) > 0 {
		content.WriteString("\n**Evidence:**")

		for i, evidence := range reason.Evidence {
			if i >= 5 {
				content.WriteString("\n... and more evidence")
				break
			}

			evidence = utils.TruncateString(evidence, 100)

			evidence = utils.NormalizeString(evidence)
			if privacyMode {
				evidence = utils.CensorStringsInText(evidence, true, sensitiveInfo...)
			}

			content.WriteString(fmt.Sprintf("\n- `%s`", evidence))
		}
	}

	return content.String()
}

// BuildReasonOptions creates the common reason management options.
func BuildReasonOptions[T types.ReasonType](
	reasons types.Reasons[T], reasonTypes []T, getEmoji func(T) string, reasonsChanged bool,
) []discord.StringSelectMenuOption {
	options := make([]discord.StringSelectMenuOption, 0)

	// Add refresh option if reasons have been changed
	if reasonsChanged {
		options = append(options, discord.NewStringSelectMenuOption(
			"Restore original reasons",
			constants.RefreshButtonCustomID,
		).WithEmoji(discord.ComponentEmoji{Name: "🔄"}).
			WithDescription("Reset all reasons back to their original state"))
	}

	for _, reasonType := range reasonTypes {
		// Check if this reason type exists
		_, exists := reasons[reasonType]

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
		).WithEmoji(discord.ComponentEmoji{Name: getEmoji(reasonType)}))
	}

	return options
}
