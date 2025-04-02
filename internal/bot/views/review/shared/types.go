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
	PrivacyMode    bool
}

// BuildCommentsEmbed creates an embed showing recent comments if any exist.
func (b *BaseReviewBuilder) BuildCommentsEmbed() *discord.EmbedBuilder {
	if len(b.Comments) == 0 {
		return nil
	}

	embed := discord.NewEmbedBuilder().
		SetTitle("📝 Recent Community Notes").
		SetColor(utils.GetMessageEmbedColor(b.PrivacyMode))

	// Take up to 3 most recent comments
	numComments := min(3, len(b.Comments))
	for i := range numComments {
		comment := b.Comments[i]
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

	if len(b.Comments) > 3 {
		embed.SetFooter(fmt.Sprintf("... and %d more notes", len(b.Comments)-3), "")
	}

	return embed
}

// BuildReviewWarningEmbed creates a warning embed if another reviewer is reviewing the target.
func (b *BaseReviewBuilder) BuildReviewWarningEmbed(activityType enum.ActivityType) *discord.EmbedBuilder {
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

	targetType := map[enum.ActivityType]string{
		enum.ActivityTypeUserViewed:  "user",
		enum.ActivityTypeGroupViewed: "group",
	}[activityType]

	return discord.NewEmbedBuilder().
		SetTitle("⚠️ Active Review Warning").
		SetDescription(fmt.Sprintf(
			"This %s was recently viewed by official reviewer%s %s. They may be actively reviewing this %s. "+
				"Please coordinate with them before taking any actions to avoid conflicts.",
			targetType,
			map[bool]string{true: "s", false: ""}[len(recentReviewers) > 1],
			strings.Join(mentions, ", "),
			targetType,
		)).
		SetColor(constants.ErrorEmbedColor)
}

// BuildDeletionEmbed creates an embed notifying that the target has requested data deletion.
func (b *BaseReviewBuilder) BuildDeletionEmbed(activityType enum.ActivityType) *discord.EmbedBuilder {
	targetType := map[enum.ActivityType]string{
		enum.ActivityTypeUserViewed:  "user",
		enum.ActivityTypeGroupViewed: "group",
	}[activityType]

	return discord.NewEmbedBuilder().
		SetTitle("🗑️ Data Deletion Notice").
		SetDescription(fmt.Sprintf("This %s has requested deletion of their data. "+
			"Some information may be missing or incomplete.", targetType)).
		SetColor(constants.ErrorEmbedColor)
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
		discord.NewStringSelectMenuOption("Selected by last updated time", enum.ReviewSortByLastUpdated.String()).
			WithDefault(defaultSort == enum.ReviewSortByLastUpdated).
			WithEmoji(discord.ComponentEmoji{Name: "📅"}),
		discord.NewStringSelectMenuOption("Selected by bad reputation", enum.ReviewSortByReputation.String()).
			WithDefault(defaultSort == enum.ReviewSortByReputation).
			WithEmoji(discord.ComponentEmoji{Name: "👎"}),
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

// BuildBaseComponents creates the common base components for review menus.
func (b *BaseReviewBuilder) BuildBaseComponents(
	sortOptions []discord.StringSelectMenuOption,
	reasonOptions []discord.StringSelectMenuOption,
	actionOptions []discord.StringSelectMenuOption,
) []discord.ContainerComponent {
	components := []discord.ContainerComponent{}

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

// AddEvidenceFields adds separate evidence fields for the embed if any reasons have evidence.
func AddEvidenceFields[T types.ReasonType](
	embed *discord.EmbedBuilder, reasons types.Reasons[T], privacyMode bool, censorStrings ...string,
) {
	var hasEvidence bool
	var fields []struct {
		name  string
		value string
	}

	// Collect evidence from all reasons
	for reasonType, reason := range reasons {
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
				if privacyMode {
					evidence = utils.CensorStringsInText(evidence, true, censorStrings...)
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

// GetConfirmButtonLabel returns the appropriate label for the confirm button based on review mode.
func (b *BaseReviewBuilder) GetConfirmButtonLabel() string {
	if b.ReviewMode == enum.ReviewModeTraining || !b.IsReviewer {
		return "Report"
	}
	return "Confirm"
}

// GetClearButtonLabel returns the appropriate label for the clear button based on review mode.
func (b *BaseReviewBuilder) GetClearButtonLabel() string {
	if b.ReviewMode == enum.ReviewModeTraining || !b.IsReviewer {
		return "Safe"
	}
	return "Clear"
}
