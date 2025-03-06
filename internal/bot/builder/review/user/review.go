package user

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/robalyx/rotector/assets"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/bot/utils"
	"github.com/robalyx/rotector/internal/common/client/fetcher"
	"github.com/robalyx/rotector/internal/common/storage/database"
	"github.com/robalyx/rotector/internal/common/storage/database/types"
	"github.com/robalyx/rotector/internal/common/storage/database/types/enum"
	"github.com/robalyx/rotector/internal/common/translator"
)

// ReviewBuilder creates the visual layout for reviewing a user.
type ReviewBuilder struct {
	db             database.Client
	userID         uint64
	user           *types.ReviewUser
	flaggedFriends map[uint64]*types.ReviewUser
	flaggedGroups  map[uint64]*types.ReviewGroup
	translator     *translator.Translator
	reviewMode     enum.ReviewMode
	defaultSort    enum.ReviewSortBy
	isReviewer     bool
	trainingMode   bool
	privacyMode    bool
}

// NewReviewBuilder creates a new review builder.
func NewReviewBuilder(s *session.Session, translator *translator.Translator, db database.Client) *ReviewBuilder {
	trainingMode := session.UserReviewMode.Get(s) == enum.ReviewModeTraining
	userID := session.UserID.Get(s)
	return &ReviewBuilder{
		db:             db,
		userID:         userID,
		user:           session.UserTarget.Get(s),
		flaggedFriends: session.UserFlaggedFriends.Get(s),
		flaggedGroups:  session.UserFlaggedGroups.Get(s),
		translator:     translator,
		reviewMode:     session.UserReviewMode.Get(s),
		defaultSort:    session.UserUserDefaultSort.Get(s),
		isReviewer:     s.BotSettings().IsReviewer(userID),
		trainingMode:   trainingMode,
		privacyMode:    trainingMode || session.UserStreamerMode.Get(s),
	}
}

// Build creates a Discord message with user information in an embed and adds
// interactive components for reviewing the user.
func (b *ReviewBuilder) Build() *discord.MessageUpdateBuilder {
	builder := discord.NewMessageUpdateBuilder()

	// Create embeds
	modeEmbed := b.buildModeEmbed()
	reviewEmbed := b.buildReviewBuilder()

	// Handle thumbnail
	if b.user.ThumbnailURL != "" && b.user.ThumbnailURL != fetcher.ThumbnailPlaceholder {
		reviewEmbed.SetThumbnail(b.user.ThumbnailURL)
	} else {
		// Load and attach placeholder image
		placeholderImage, err := assets.Images.Open("images/content_deleted.png")
		if err == nil {
			builder.SetFiles(discord.NewFile("content_deleted.png", "", placeholderImage))
			reviewEmbed.SetThumbnail("attachment://content_deleted.png")
			_ = placeholderImage.Close()
		}
	}

	// Add deletion notice if user is deleted
	if b.user.IsDeleted {
		deletionEmbed := b.buildDeletionEmbed()
		builder.AddEmbeds(modeEmbed.Build(), reviewEmbed.Build(), deletionEmbed.Build())
	} else {
		builder.AddEmbeds(modeEmbed.Build(), reviewEmbed.Build())
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
	switch b.reviewMode {
	case enum.ReviewModeTraining:
		mode = "🎓 Training Mode"
		description += "**You are not an official reviewer.**\n" +
			"You may help moderators by downvoting to indicate inappropriate activity.\n" +
			"Information is censored and external links are disabled."
	case enum.ReviewModeStandard:
		mode = "⚠️ Standard Mode"
		description += "Your actions are recorded and affect the database. Please review carefully before taking action."
	default:
		mode = "❌ Unknown Mode"
		description += "Error encountered. Please check your settings."
	}

	return discord.NewEmbedBuilder().
		SetTitle(mode).
		SetDescription(description).
		SetColor(utils.GetMessageEmbedColor(b.privacyMode))
}

// buildReviewBuilder creates the main review information embed.
func (b *ReviewBuilder) buildReviewBuilder() *discord.EmbedBuilder {
	embed := discord.NewEmbedBuilder().
		SetColor(utils.GetMessageEmbedColor(b.privacyMode)).
		SetTitle(fmt.Sprintf("⚠️ %d Reports • 🛡️ %d Safe",
			b.user.Reputation.Downvotes,
			b.user.Reputation.Upvotes,
		))

	// Add status indicator based on user status
	var status string
	switch b.user.Status {
	case enum.UserTypeConfirmed:
		status = "⚠️ Confirmed"
	case enum.UserTypeFlagged:
		status = "⏳ Pending Review"
	case enum.UserTypeCleared:
		status = "✅ Cleared"
	}

	// Add banned status if applicable
	if b.user.IsBanned {
		status += " 🔨 Banned"
	}

	createdAt := fmt.Sprintf("<t:%d:R>", b.user.CreatedAt.Unix())
	lastUpdated := fmt.Sprintf("<t:%d:R>", b.user.LastUpdated.Unix())
	confidence := fmt.Sprintf("%.2f%%", b.user.Confidence*100)

	// Censor reason if needed
	reason := b.getReasonField()

	if b.reviewMode == enum.ReviewModeTraining {
		// Training mode - show limited information without links
		embed.AddField("ID", utils.CensorString(strconv.FormatUint(b.user.ID, 10), true), true).
			AddField("Name", utils.CensorString(b.user.Name, true), true).
			AddField("Display Name", utils.CensorString(b.user.DisplayName, true), true).
			AddField("Game Visits", b.getTotalVisits(), true).
			AddField("Confidence", confidence, true).
			AddField("Created At", createdAt, true).
			AddField("Last Updated", lastUpdated, true).
			AddField("Reason", reason, false).
			AddField("Description", b.getDescription(), false).
			AddField(b.getFriendsField(), b.getFriends(), false).
			AddField(b.getGroupsField(), b.getGroups(), false).
			AddField("Outfits", b.getOutfits(), false).
			AddField("Games", b.getGames(), false)
		b.addEvidenceFields(embed)
	} else {
		// Standard mode - show all information with links
		embed.AddField("ID", fmt.Sprintf(
			"[%s](https://www.roblox.com/users/%d/profile)",
			utils.CensorString(strconv.FormatUint(b.user.ID, 10), b.privacyMode),
			b.user.ID,
		), true).
			AddField("Name", utils.CensorString(b.user.Name, b.privacyMode), true).
			AddField("Display Name", utils.CensorString(b.user.DisplayName, b.privacyMode), true).
			AddField("Game Visits", b.getTotalVisits(), true).
			AddField("Confidence", confidence, true).
			AddField("Created At", createdAt, true).
			AddField("Last Updated", lastUpdated, true).
			AddField("Reason", reason, false).
			AddField("Description", b.getDescription(), false).
			AddField(b.getFriendsField(), b.getFriends(), false).
			AddField(b.getGroupsField(), b.getGroups(), false).
			AddField("Outfits", b.getOutfits(), false).
			AddField("Games", b.getGames(), false)
		b.addEvidenceFields(embed)
		embed.AddField("Review History", b.getReviewHistory(), false)
	}

	// Add status-specific timestamps
	if !b.user.VerifiedAt.IsZero() {
		embed.AddField("Verified At", fmt.Sprintf("<t:%d:R>", b.user.VerifiedAt.Unix()), true)
	}
	if !b.user.ClearedAt.IsZero() {
		embed.AddField("Cleared At", fmt.Sprintf("<t:%d:R>", b.user.ClearedAt.Unix()), true)
	}

	// Add UUID and status to footer
	embed.SetFooter(fmt.Sprintf("%s • UUID: %s", status, b.user.UUID.String()), "")

	return embed
}

// buildDeletionEmbed creates an embed notifying that the user has requested data deletion.
func (b *ReviewBuilder) buildDeletionEmbed() *discord.EmbedBuilder {
	return discord.NewEmbedBuilder().
		SetTitle("🗑️ Data Deletion Notice").
		SetDescription("This user has requested deletion of their data. Some information may be missing or incomplete.").
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
	options := []discord.StringSelectMenuOption{
		discord.NewStringSelectMenuOption("Open friends viewer", constants.OpenFriendsMenuButtonCustomID).
			WithEmoji(discord.ComponentEmoji{Name: "👫"}).
			WithDescription("View all user friends"),
		discord.NewStringSelectMenuOption("Open group viewer", constants.OpenGroupsMenuButtonCustomID).
			WithEmoji(discord.ComponentEmoji{Name: "🌐"}).
			WithDescription("View all user groups"),
		discord.NewStringSelectMenuOption("Open outfit viewer", constants.OpenOutfitsMenuButtonCustomID).
			WithEmoji(discord.ComponentEmoji{Name: "👕"}).
			WithDescription("View all user outfits"),
	}

	// Add reviewer-only options
	if b.isReviewer {
		// Options available in both normal and lookup mode
		reviewerOptions := []discord.StringSelectMenuOption{
			discord.NewStringSelectMenuOption("Ask AI about user", constants.OpenAIChatButtonCustomID).
				WithEmoji(discord.ComponentEmoji{Name: "🤖"}).
				WithDescription("Ask the AI questions about this user"),
			discord.NewStringSelectMenuOption("View user logs", constants.ViewUserLogsButtonCustomID).
				WithEmoji(discord.ComponentEmoji{Name: "📋"}).
				WithDescription("View activity logs for this user"),
			discord.NewStringSelectMenuOption("Recheck user", constants.RecheckButtonCustomID).
				WithEmoji(discord.ComponentEmoji{Name: "🔄"}).
				WithDescription("Add user to high priority queue for recheck"),
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
			WithDescription("Change what type of users to review"),
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

	// Add reason management dropdown for reviewers
	if b.isReviewer && b.reviewMode != enum.ReviewModeTraining {
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

	// Add navigation/action buttons
	components = append(components, discord.NewActionRow(
		discord.NewSecondaryButton("◀️", constants.BackButtonCustomID),
		discord.NewDangerButton(b.getConfirmButtonLabel(), constants.ConfirmButtonCustomID),
		discord.NewSuccessButton(b.getClearButtonLabel(), constants.ClearButtonCustomID),
		discord.NewSecondaryButton("Skip", constants.SkipButtonCustomID),
	))

	return components
}

// buildReasonOptions creates the reason management options.
func (b *ReviewBuilder) buildReasonOptions() []discord.StringSelectMenuOption {
	options := make([]discord.StringSelectMenuOption, 0)

	// Define available reason types for users
	reasonTypes := []enum.UserReasonType{
		enum.UserReasonTypeDescription,
		enum.UserReasonTypeFriend,
		enum.UserReasonTypeOutfit,
		enum.UserReasonTypeGroup,
	}

	for _, reasonType := range reasonTypes {
		// Check if this reason type exists
		_, exists := b.user.Reasons[reasonType]

		var action string
		optionValue := reasonType.String()
		if exists {
			action = "Remove"
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

// getTotalVisits returns the total visits across all games.
func (b *ReviewBuilder) getTotalVisits() string {
	if len(b.user.Games) == 0 {
		return constants.NotApplicable
	}

	var totalVisits uint64
	for _, game := range b.user.Games {
		totalVisits += game.PlaceVisits
	}

	return utils.FormatNumber(totalVisits)
}

// getReasonField returns the formatted reason field for the embed.
func (b *ReviewBuilder) getReasonField() string {
	if len(b.user.Reasons) == 0 {
		return constants.NotApplicable
	}

	// Build formatted output
	var formattedReasons []string

	// Order of reason types to display
	reasonTypes := []enum.UserReasonType{
		enum.UserReasonTypeDescription,
		enum.UserReasonTypeFriend,
		enum.UserReasonTypeOutfit,
		enum.UserReasonTypeGroup,
	}

	for _, reasonType := range reasonTypes {
		if reason, ok := b.user.Reasons[reasonType]; ok {
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
			strconv.FormatUint(b.user.ID, 10),
			b.user.Name,
			b.user.DisplayName,
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
	for reasonType, reason := range b.user.Reasons {
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
						strconv.FormatUint(b.user.ID, 10),
						b.user.Name,
						b.user.DisplayName,
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

// getDescription returns the description field for the embed.
func (b *ReviewBuilder) getDescription() string {
	description := b.user.Description

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
		strconv.FormatUint(b.user.ID, 10),
		b.user.Name,
		b.user.DisplayName,
	)

	// Translate the description
	translatedDescription, err := b.translator.Translate(context.Background(), description, "auto", "en")
	if err == nil && translatedDescription != description {
		return "(translated)\n" + translatedDescription
	}

	return description
}

// getReviewHistory returns the review history field for the embed.
func (b *ReviewBuilder) getReviewHistory() string {
	logs, nextCursor, err := b.db.Models().Activities().GetLogs(
		context.Background(),
		types.ActivityFilter{
			UserID:       b.user.ID,
			GroupID:      0,
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

// getFriends returns the friends field for the embed.
func (b *ReviewBuilder) getFriends() string {
	friends := make([]string, 0, constants.ReviewFriendsLimit)

	for i, friend := range b.user.Friends {
		if i >= constants.ReviewFriendsLimit {
			break
		}

		name := utils.CensorString(friend.Name, b.privacyMode)
		if b.trainingMode {
			friends = append(friends, name)
		} else {
			friends = append(friends, fmt.Sprintf(
				"[%s](https://www.roblox.com/users/%d/profile)",
				name,
				friend.ID,
			))
		}
	}

	if len(friends) == 0 {
		return constants.NotApplicable
	}

	result := strings.Join(friends, ", ")
	if len(b.user.Friends) > constants.ReviewFriendsLimit {
		result += fmt.Sprintf(" ... and %d more", len(b.user.Friends)-constants.ReviewFriendsLimit)
	}

	return result
}

// getGroups returns the groups field for the embed.
func (b *ReviewBuilder) getGroups() string {
	groups := make([]string, 0, constants.ReviewGroupsLimit)

	for i, group := range b.user.Groups {
		if i >= constants.ReviewGroupsLimit {
			break
		}

		name := utils.CensorString(group.Group.Name, b.privacyMode)
		if b.trainingMode {
			groups = append(groups, name)
		} else {
			groups = append(groups, fmt.Sprintf(
				"[%s](https://www.roblox.com/groups/%d)",
				name,
				group.Group.ID,
			))
		}
	}

	if len(groups) == 0 {
		return constants.NotApplicable
	}

	result := strings.Join(groups, ", ")
	if len(b.user.Groups) > constants.ReviewGroupsLimit {
		result += fmt.Sprintf(" ... and %d more", len(b.user.Groups)-constants.ReviewGroupsLimit)
	}

	return result
}

// getGames returns the games field for the embed.
func (b *ReviewBuilder) getGames() string {
	if len(b.user.Games) == 0 {
		return constants.NotApplicable
	}

	// Format games list with visit counts
	games := make([]string, 0, constants.ReviewGamesLimit)

	for i, game := range b.user.Games {
		if i >= constants.ReviewGamesLimit {
			break
		}

		name := utils.CensorString(game.Name, b.privacyMode)
		visits := utils.FormatNumber(game.PlaceVisits)

		if b.trainingMode {
			games = append(games, fmt.Sprintf("%s (%s visits)", name, visits))
		} else {
			games = append(games, fmt.Sprintf("[%s](https://www.roblox.com/games/%d) (%s visits)",
				name, game.ID, visits))
		}
	}

	if len(games) == 0 {
		return constants.NotApplicable
	}

	result := strings.Join(games, ", ")
	if len(b.user.Games) > constants.ReviewGamesLimit {
		result += fmt.Sprintf(" ... and %d more", len(b.user.Games)-constants.ReviewGamesLimit)
	}

	return result
}

// getOutfits returns the outfits field for the embed.
func (b *ReviewBuilder) getOutfits() string {
	// Get the first 10 outfits
	outfits := make([]string, 0, constants.ReviewOutfitsLimit)
	for i, outfit := range b.user.Outfits {
		if i >= constants.ReviewOutfitsLimit {
			break
		}
		outfits = append(outfits, outfit.Name)
	}

	if len(outfits) == 0 {
		return constants.NotApplicable
	}

	result := strings.Join(outfits, ", ")
	if len(b.user.Outfits) > constants.ReviewOutfitsLimit {
		result += fmt.Sprintf(" ... and %d more", len(b.user.Outfits)-constants.ReviewOutfitsLimit)
	}

	return result
}

// getFriendsField returns the friends field name for the embed.
func (b *ReviewBuilder) getFriendsField() string {
	if len(b.flaggedFriends) == 0 {
		return "Friends"
	}

	// Count different friend types
	counts := make(map[enum.UserType]int)
	for _, friend := range b.flaggedFriends {
		counts[friend.Status]++
	}

	// Build status parts
	var parts []string
	if c := counts[enum.UserTypeConfirmed]; c > 0 {
		parts = append(parts, fmt.Sprintf("%d ⚠️", c))
	}
	if c := counts[enum.UserTypeFlagged]; c > 0 {
		parts = append(parts, fmt.Sprintf("%d ⏳", c))
	}
	if c := counts[enum.UserTypeCleared]; c > 0 {
		parts = append(parts, fmt.Sprintf("%d ✅", c))
	}

	if len(parts) > 0 {
		return "Friends (" + strings.Join(parts, ", ") + ")"
	}
	return "Friends"
}

// getGroupsField returns the groups field name for the embed.
func (b *ReviewBuilder) getGroupsField() string {
	if len(b.flaggedGroups) == 0 {
		return "Groups"
	}

	// Count different group types
	counts := make(map[enum.GroupType]int)
	for _, group := range b.flaggedGroups {
		counts[group.Status]++
	}

	// Build status parts
	var parts []string
	if c := counts[enum.GroupTypeConfirmed]; c > 0 {
		parts = append(parts, fmt.Sprintf("%d ⚠️", c))
	}
	if c := counts[enum.GroupTypeFlagged]; c > 0 {
		parts = append(parts, fmt.Sprintf("%d ⏳", c))
	}
	if c := counts[enum.GroupTypeCleared]; c > 0 {
		parts = append(parts, fmt.Sprintf("%d ✅", c))
	}

	if len(parts) > 0 {
		return "Groups (" + strings.Join(parts, ", ") + ")"
	}
	return "Groups"
}

// getReasonEmoji returns the appropriate emoji for a reason type.
func getReasonEmoji(reasonType enum.UserReasonType) string {
	switch reasonType {
	case enum.UserReasonTypeDescription:
		return "🔞"
	case enum.UserReasonTypeFriend:
		return "👥"
	case enum.UserReasonTypeOutfit:
		return "👕"
	case enum.UserReasonTypeGroup:
		return "🌐"
	default:
		return "❓"
	}
}
