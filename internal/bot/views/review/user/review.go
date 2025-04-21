package user

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/disgoorg/disgo/discord"
	"github.com/robalyx/rotector/assets"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/bot/utils"
	"github.com/robalyx/rotector/internal/bot/views/review/shared"
	"github.com/robalyx/rotector/internal/database"
	"github.com/robalyx/rotector/internal/database/types"
	"github.com/robalyx/rotector/internal/database/types/enum"
	"github.com/robalyx/rotector/internal/roblox/fetcher"
	"github.com/robalyx/rotector/internal/translator"
)

// ReviewBuilder creates the visual layout for reviewing a user.
type ReviewBuilder struct {
	shared.BaseReviewBuilder
	db             database.Client
	user           *types.ReviewUser
	flaggedFriends map[uint64]*types.ReviewUser
	flaggedGroups  map[uint64]*types.ReviewGroup
	translator     *translator.Translator
	defaultSort    enum.ReviewSortBy
	trainingMode   bool
}

// NewReviewBuilder creates a new review builder.
func NewReviewBuilder(s *session.Session, translator *translator.Translator, db database.Client) *ReviewBuilder {
	reviewMode := session.UserReviewMode.Get(s)
	trainingMode := reviewMode == enum.ReviewModeTraining
	userID := session.UserID.Get(s)
	return &ReviewBuilder{
		BaseReviewBuilder: shared.BaseReviewBuilder{
			BotSettings:    s.BotSettings(),
			Logs:           session.ReviewLogs.Get(s),
			Comments:       session.ReviewComments.Get(s),
			ReviewMode:     reviewMode,
			ReviewHistory:  session.UserReviewHistory.Get(s),
			UserID:         userID,
			HistoryIndex:   session.UserReviewHistoryIndex.Get(s),
			LogsHasMore:    session.ReviewLogsHasMore.Get(s),
			ReasonsChanged: session.ReasonsChanged.Get(s),
			IsReviewer:     s.BotSettings().IsReviewer(userID),
			IsAdmin:        s.BotSettings().IsAdmin(userID),
			PrivacyMode:    trainingMode || session.UserStreamerMode.Get(s),
			TrainingMode:   trainingMode,
		},
		db:             db,
		user:           session.UserTarget.Get(s),
		flaggedFriends: session.UserFlaggedFriends.Get(s),
		flaggedGroups:  session.UserFlaggedGroups.Get(s),
		translator:     translator,
		defaultSort:    session.UserUserDefaultSort.Get(s),
		trainingMode:   trainingMode,
	}
}

// Build creates a Discord message with user information in an embed and adds
// interactive components for reviewing the user.
func (b *ReviewBuilder) Build() *discord.MessageUpdateBuilder {
	builder := discord.NewMessageUpdateBuilder()

	// Add mode embed
	modeEmbed := b.buildModeEmbed()
	builder.AddEmbeds(modeEmbed.Build())

	// Add review embed
	reviewEmbed := b.buildReviewEmbed()
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
	builder.AddEmbeds(reviewEmbed.Build())

	// Add recent comments embed if there are any
	if commentsEmbed := b.BuildCommentsEmbed(); commentsEmbed != nil {
		builder.AddEmbeds(commentsEmbed.Build())
	}

	// Add deletion notice if user is deleted
	if b.user.IsDeleted {
		deletionEmbed := b.BuildDeletionEmbed(enum.ActivityTypeUserViewed)
		builder.AddEmbeds(deletionEmbed.Build())
	}

	// Add warning embed if there are recent reviewers
	if warningEmbed := b.BuildReviewWarningEmbed(enum.ActivityTypeUserViewed); warningEmbed != nil {
		builder.AddEmbeds(warningEmbed.Build())
	}

	// Add condo warning embed if applicable
	if condoWarningEmbed := b.buildCondoWarningEmbed(); condoWarningEmbed != nil {
		builder.AddEmbeds(condoWarningEmbed.Build())
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
	switch b.ReviewMode {
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
		SetColor(utils.GetMessageEmbedColor(b.PrivacyMode))
}

// buildReviewEmbed creates the main review information embed.
func (b *ReviewBuilder) buildReviewEmbed() *discord.EmbedBuilder {
	embed := discord.NewEmbedBuilder().
		SetColor(utils.GetMessageEmbedColor(b.PrivacyMode)).
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
		status = "⏳ Pending"
	case enum.UserTypeCleared:
		status = "✅ Cleared"
	}

	// Add banned status if applicable
	if b.user.IsBanned {
		status += " 🔨 Banned"
	}

	userID := strconv.FormatUint(b.user.ID, 10)
	createdAt := fmt.Sprintf("<t:%d:R>", b.user.CreatedAt.Unix())
	lastUpdated := fmt.Sprintf("<t:%d:R>", b.user.LastUpdated.Unix())
	confidence := fmt.Sprintf("%.2f%%", b.user.Confidence*100)

	if b.ReviewMode == enum.ReviewModeTraining {
		// Training mode - show limited information without links
		embed.AddField("ID", utils.CensorString(userID, true), true).
			AddField("Name", utils.CensorString(b.user.Name, true), true).
			AddField("Display Name", utils.CensorString(b.user.DisplayName, true), true).
			AddField("Game Visits", b.getTotalVisits(), true).
			AddField("Confidence", confidence, true).
			AddField("Created At", createdAt, true).
			AddField("Last Updated", lastUpdated, true).
			AddField("Reason", b.getReason(), false).
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
			utils.CensorString(userID, b.PrivacyMode),
			b.user.ID,
		), true).
			AddField("Name", utils.CensorString(b.user.Name, b.PrivacyMode), true).
			AddField("Display Name", utils.CensorString(b.user.DisplayName, b.PrivacyMode), true).
			AddField("Game Visits", b.getTotalVisits(), true).
			AddField("Confidence", confidence, true).
			AddField("Created At", createdAt, true).
			AddField("Last Updated", lastUpdated, true).
			AddField("Reason", b.getReason(), false).
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

	// Build footer with status and history position
	var footerText string
	if len(b.ReviewHistory) > 0 {
		footerText = fmt.Sprintf("%s • UUID: %s • History: %d/%d",
			status,
			b.user.UUID.String(),
			b.HistoryIndex+1,
			len(b.ReviewHistory))
	} else {
		footerText = fmt.Sprintf("%s • UUID: %s", status, b.user.UUID.String())
	}
	embed.SetFooter(footerText, "")

	return embed
}

// buildCondoWarningEmbed creates a warning embed if the user only has condo-related reasons.
func (b *ReviewBuilder) buildCondoWarningEmbed() *discord.EmbedBuilder {
	// Check if the user has only one reason
	if len(b.user.Reasons) != 1 {
		return nil
	}

	// Check if the only reason is a condo reason
	if _, hasCondoReason := b.user.Reasons[enum.UserReasonTypeCondo]; !hasCondoReason {
		return nil
	}

	description := "This user has been flagged **only** for joining known condo games. " +
		"Our detection method for condo visits is not always reliable and may incorrectly flag users, " +
		"especially those with default avatars.\n\n" +
		"**Review Guidelines:**\n" +
		"- You cannot accept or reject users based solely on condo visits\n" +
		"- If this is a default avatar user, please **skip** this review - our system will handle these false positives\n" +
		"- For established accounts, please check their profiles thoroughly as AI could have missed something\n" +
		"- Additional evidence types (description, outfits, groups, etc.) are required to take action"

	return discord.NewEmbedBuilder().
		SetTitle("⚠️ Condo Visit Notice").
		SetDescription(description).
		SetColor(constants.ErrorEmbedColor)
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
		discord.NewStringSelectMenuOption("Translate caesar cipher", constants.CaesarCipherButtonCustomID).
			WithEmoji(discord.ComponentEmoji{Name: "🔍"}).
			WithDescription("View Caesar cipher analysis of description"),
	}

	// Add comment options
	options = append(options, b.BuildCommentOptions()...)

	// Add reviewer-only options
	if b.IsReviewer {
		reviewerOptions := []discord.StringSelectMenuOption{
			discord.NewStringSelectMenuOption("Ask AI about user", constants.OpenAIChatButtonCustomID).
				WithEmoji(discord.ComponentEmoji{Name: "🤖"}).
				WithDescription("Ask the AI questions about this user"),
			discord.NewStringSelectMenuOption("View user logs", constants.ViewUserLogsButtonCustomID).
				WithEmoji(discord.ComponentEmoji{Name: "📋"}).
				WithDescription("View activity logs for this user"),
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

	// Disable buttons if only condo reason
	if len(b.user.Reasons) == 1 && b.user.Reasons[enum.UserReasonTypeCondo] != nil {
		confirmButton = confirmButton.WithDisabled(true)
		clearButton = clearButton.WithDisabled(true)
	}

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
	reasonTypes := []enum.UserReasonType{
		enum.UserReasonTypeDescription,
		enum.UserReasonTypeFriend,
		enum.UserReasonTypeOutfit,
		enum.UserReasonTypeGroup,
		enum.UserReasonTypeCondo,
		enum.UserReasonTypeChat,
	}
	return shared.BuildReasonOptions(b.user.Reasons, reasonTypes, getReasonEmoji, b.ReasonsChanged)
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

// getReason returns the formatted reason for the embed.
func (b *ReviewBuilder) getReason() string {
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
		enum.UserReasonTypeCondo,
		enum.UserReasonTypeChat,
	}

	// Calculate dynamic truncation length based on number of reasons
	maxLength := utils.CalculateDynamicTruncationLength(len(b.user.Reasons))

	for _, reasonType := range reasonTypes {
		if reason, ok := b.user.Reasons[reasonType]; ok {
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
			strconv.FormatUint(b.user.ID, 10),
			b.user.Name,
			b.user.DisplayName,
		)
	}

	return reasonText
}

// addEvidenceFields adds separate evidence fields for the embed if any reasons have evidence.
func (b *ReviewBuilder) addEvidenceFields(embed *discord.EmbedBuilder) {
	shared.AddEvidenceFields(
		embed, b.user.Reasons, b.PrivacyMode,
		strconv.FormatUint(b.user.ID, 10),
		b.user.Name,
		b.user.DisplayName,
	)
}

// getDescription returns the description field for the embed.
func (b *ReviewBuilder) getDescription() string {
	description := b.user.Description

	// Check if description is empty
	if description == "" {
		return constants.NotApplicable
	}

	// Prepare description
	description = utils.CensorStringsInText(
		description,
		b.PrivacyMode,
		strconv.FormatUint(b.user.ID, 10),
		b.user.Name,
		b.user.DisplayName,
	)
	description = utils.TruncateString(description, 400)
	description = utils.FormatString(description)

	// Translate the description
	translatedDescription, err := b.translator.Translate(context.Background(), description, "auto", "en")
	if err == nil && translatedDescription != description {
		return "(translated)\n" + translatedDescription
	}

	return description
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

// getFriends returns the friends field for the embed.
func (b *ReviewBuilder) getFriends() string {
	friends := make([]string, 0, constants.ReviewFriendsLimit)

	for i, friend := range b.user.Friends {
		if i >= constants.ReviewFriendsLimit {
			break
		}

		name := utils.CensorString(friend.Name, b.PrivacyMode)
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

		name := utils.CensorString(group.Group.Name, b.PrivacyMode)
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

		name := utils.CensorString(game.Name, b.PrivacyMode)
		visits := utils.FormatNumber(game.PlaceVisits)

		if b.trainingMode {
			games = append(games, fmt.Sprintf("%s (%s visits)", name, visits))
		} else {
			games = append(games, fmt.Sprintf("[%s](https://www.roblox.com/games/%d) (%s visits)",
				name, game.RootPlace.ID, visits))
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

// GetConfirmButtonLabel returns the appropriate label for the confirm button based on review mode.
func (b *ReviewBuilder) GetConfirmButtonLabel() string {
	if b.ReviewMode == enum.ReviewModeTraining || !b.IsReviewer {
		return "Report"
	}
	return "Confirm"
}

// GetClearButtonLabel returns the appropriate label for the clear button based on review mode.
func (b *ReviewBuilder) GetClearButtonLabel() string {
	if b.ReviewMode == enum.ReviewModeTraining || !b.IsReviewer {
		return "Safe"
	}
	return "Clear"
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
	case enum.UserReasonTypeCondo:
		return "🏠"
	case enum.UserReasonTypeChat:
		return "💬"
	default:
		return "❓"
	}
}
