package user

import (
	"bytes"
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
)

// ReviewBuilder creates the visual layout for reviewing a user.
type ReviewBuilder struct {
	shared.BaseReviewBuilder

	db             database.Client
	user           *types.ReviewUser
	flaggedFriends map[uint64]*types.ReviewUser
	flaggedGroups  map[uint64]*types.ReviewGroup
	unsavedReasons map[enum.UserReasonType]struct{}
	defaultSort    enum.ReviewSortBy
	trainingMode   bool
}

// NewReviewBuilder creates a new review builder.
func NewReviewBuilder(s *session.Session, db database.Client) *ReviewBuilder {
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
		unsavedReasons: session.UnsavedUserReasons.Get(s),
		defaultSort:    session.UserUserDefaultSort.Get(s),
		trainingMode:   trainingMode,
	}
}

// Build creates a Discord message with user information.
func (b *ReviewBuilder) Build() *discord.MessageUpdateBuilder {
	builder := discord.NewMessageUpdateBuilder()

	// Create main info container
	mainInfoDisplays := []discord.ContainerSubComponent{
		b.buildUserInfoSection(),
		discord.NewLargeSeparator(),
		discord.NewSection(
			discord.NewTextDisplay(fmt.Sprintf("-# UUID: %s\n-# Created: %s • Updated: %s\n-# Engine Version: %s",
				b.user.UUID.String(),
				fmt.Sprintf("<t:%d:R>", b.user.CreatedAt.Unix()),
				fmt.Sprintf("<t:%d:R>", b.user.LastUpdated.Unix()),
				b.user.EngineVersion,
			)),
		).WithAccessory(
			discord.NewLinkButton("View Profile", fmt.Sprintf("https://www.roblox.com/users/%d/profile", b.user.ID)).
				WithEmoji(discord.ComponentEmoji{Name: "🔗"}),
		),
	}

	// Add status-specific timestamps if they exist
	if !b.user.VerifiedAt.IsZero() || !b.user.ClearedAt.IsZero() {
		var timestamps []string
		if !b.user.VerifiedAt.IsZero() {
			timestamps = append(timestamps, "Verified: "+fmt.Sprintf("<t:%d:R>", b.user.VerifiedAt.Unix()))
		}

		if !b.user.ClearedAt.IsZero() {
			timestamps = append(timestamps, "Cleared: "+fmt.Sprintf("<t:%d:R>", b.user.ClearedAt.Unix()))
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
	if b.IsReviewer && b.ReviewMode != enum.ReviewModeTraining {
		reviewInfoDisplays = append(reviewInfoDisplays,
			discord.NewActionRow(
				discord.NewStringSelectMenu(constants.ReasonSelectMenuCustomID, "Manage Reasons", b.buildReasonOptions()...),
			),
			discord.NewActionRow(
				discord.NewStringSelectMenu(constants.AIReasonSelectMenuCustomID, "Generate AI Reasons", b.buildAIReasonOptions()...),
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
	if b.user.ThumbnailURL == "" || b.user.ThumbnailURL == fetcher.ThumbnailPlaceholder {
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

	// Add deletion notice if user is deleted
	if b.user.IsDeleted {
		content.WriteString(
			"\n\n## 🗑️ Data Deletion Notice\nThis user has requested deletion of their data. Some information may be missing or incomplete.")
	}

	// Add warning if there are recent reviewers
	if warningDisplay := b.BuildReviewWarningText("user", enum.ActivityTypeUserViewed); warningDisplay != "" {
		content.WriteString("\n\n## ⚠️ Active Review Warning\n" + warningDisplay)
	}

	// Add condo warning if applicable
	if condoWarningDisplay := b.buildCondoWarningDisplay(); condoWarningDisplay != "" {
		content.WriteString("\n\n" + condoWarningDisplay)
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

	switch b.user.Status {
	case enum.UserTypeConfirmed:
		statusIcon = "⚠️"
		statusName = "Confirmed"
		statusDesc = "This user has been confirmed to have inappropriate content or behavior"
	case enum.UserTypeFlagged:
		statusIcon = "⏳"
		statusName = "Flagged"
		statusDesc = "This user has been flagged for review but no final decision has been made"
	case enum.UserTypeCleared:
		statusIcon = "✅"
		statusName = "Cleared"
		statusDesc = "This user has been reviewed and cleared of any violations"
	}

	if b.user.IsBanned {
		statusIcon += "🔨"
		statusDesc += " and is currently banned from Roblox"
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
	case enum.ReviewModeTraining:
		mode = "🎓 Training Mode"
		description = "**You are not an official reviewer.**\n" +
			"You may help moderators by downvoting to indicate inappropriate activity.\n" +
			"Information is censored and external links are disabled."
	case enum.ReviewModeStandard:
		mode = "⚠️ Standard Mode"
		description = "Your actions are recorded and affect the database. Please review carefully before taking action."
	default:
		mode = "❌ Unknown Mode"
		description = "Error encountered. Please check your settings."
	}

	return fmt.Sprintf("## %s\n%s", mode, description)
}

// buildUserInfoSection creates the main user information section with thumbnail.
func (b *ReviewBuilder) buildUserInfoSection() discord.ContainerSubComponent {
	var content strings.Builder

	// Add name header
	content.WriteString(fmt.Sprintf("## %s (%s)\n",
		utils.CensorString(b.user.Name, b.PrivacyMode),
		utils.CensorString(b.user.DisplayName, b.PrivacyMode)))

	// Add description
	content.WriteString(b.getDescription())

	// Add friends section
	content.WriteString(fmt.Sprintf(
		"\n### 👥 %s\n-# Total Friends: %d\n%s",
		b.getFriendsField(), len(b.user.Friends), b.getFriends()))

	// Add groups section
	content.WriteString(fmt.Sprintf(
		"\n### 🌐 %s\n-# Total Groups: %d\n%s",
		b.getGroupsField(), len(b.user.Groups), b.getGroups()))

	// Add outfits section
	content.WriteString(fmt.Sprintf(
		"\n### 👕 Outfits\n-# Total Outfits: %d\n%s", len(b.user.Outfits),
		b.getOutfits()))

	// Add games section
	content.WriteString(fmt.Sprintf(
		"\n### 🎮 Games\n-# Total Games: %d\n-# Total Visits: %s\n%s",
		len(b.user.Games), b.getTotalVisits(), b.getGames()))

	// Create main section
	section := discord.NewSection(discord.NewTextDisplay(content.String()))
	if b.user.ThumbnailURL != "" && b.user.ThumbnailURL != fetcher.ThumbnailPlaceholder {
		section = section.WithAccessory(discord.NewThumbnail(b.user.ThumbnailURL))
	} else {
		section = section.WithAccessory(discord.NewThumbnail("attachment://content_deleted.png"))
	}

	return section
}

// buildInteractiveComponents creates the navigation and action buttons.
func (b *ReviewBuilder) buildInteractiveComponents() []discord.ContainerSubComponent {
	// Add navigation buttons
	navButtons := b.BuildNavigationButtons()
	confirmButton := discord.NewDangerButton(b.getConfirmButtonLabel(), constants.ConfirmButtonCustomID)
	clearButton := discord.NewSuccessButton(b.getClearButtonLabel(), constants.ClearButtonCustomID)

	// Disable buttons if only condo reason
	if len(b.user.Reasons) == 1 && b.user.Reasons[enum.UserReasonTypeCondo] != nil {
		confirmButton = confirmButton.WithDisabled(true)
		clearButton = clearButton.WithDisabled(true)
	}

	// Add all buttons to a single row
	allButtons := make([]discord.InteractiveComponent, 0, len(navButtons)+2)
	allButtons = append(allButtons, navButtons...)
	allButtons = append(allButtons, confirmButton, clearButton)

	return []discord.ContainerSubComponent{
		discord.NewActionRow(allButtons...),
	}
}

// buildReasonDisplay creates the reason section with evidence.
func (b *ReviewBuilder) buildReasonDisplay() discord.ContainerSubComponent {
	var content strings.Builder
	content.WriteString("## Reasons and Evidence\n")

	if len(b.user.Reasons) == 0 {
		content.WriteString("No reasons have been added yet.")
		return discord.NewTextDisplay(content.String())
	}

	content.WriteString(fmt.Sprintf("-# Total Confidence: %.2f%%\n\n", b.user.Confidence*100))

	// Calculate dynamic truncation length based on number of reasons
	maxLength := utils.CalculateDynamicTruncationLength(len(b.user.Reasons))

	for _, reasonType := range []enum.UserReasonType{
		enum.UserReasonTypeProfile,
		enum.UserReasonTypeFriend,
		enum.UserReasonTypeOutfit,
		enum.UserReasonTypeGroup,
		enum.UserReasonTypeCondo,
		enum.UserReasonTypeChat,
		enum.UserReasonTypeFavorites,
		enum.UserReasonTypeBadges,
	} {
		if reason, ok := b.user.Reasons[reasonType]; ok {
			// Add reason header and message
			message := utils.CensorStringsInText(reason.Message, b.PrivacyMode,
				strconv.FormatUint(b.user.ID, 10),
				b.user.Name,
				b.user.DisplayName)
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
							strconv.FormatUint(b.user.ID, 10),
							b.user.Name,
							b.user.DisplayName)
					}

					content.WriteString(fmt.Sprintf("- `%s`\n", evidence))
				}
			}

			content.WriteString("\n")
		}
	}

	return discord.NewTextDisplay(content.String())
}

// buildCondoWarningDisplay creates the condo warning display.
func (b *ReviewBuilder) buildCondoWarningDisplay() string {
	// Check if the user has only one reason
	if len(b.user.Reasons) != 1 {
		return ""
	}

	// Check if the only reason is a condo reason
	if _, hasCondoReason := b.user.Reasons[enum.UserReasonTypeCondo]; !hasCondoReason {
		return ""
	}

	var content strings.Builder
	content.WriteString("## ⚠️ Condo Visit Notice\n")
	content.WriteString("This user has been flagged **only** for joining known condo games. ")
	content.WriteString("Our detection method for condo visits is not always reliable and may incorrectly flag users, ")
	content.WriteString("especially those with default avatars.\n\n")
	content.WriteString("**Review Guidelines:**\n")
	content.WriteString("- You cannot accept or reject users based solely on condo visits\n")
	content.WriteString("- If this is a default avatar user, please **skip** this review - our system will handle these false positives\n")
	content.WriteString("- For established accounts, please check their profiles thoroughly as AI could have missed something\n")
	content.WriteString("- Additional evidence types (description, outfits, groups, etc.) are required to take action")

	return content.String()
}

// buildActionOptions creates the action menu options.
func (b *ReviewBuilder) buildActionOptions() []discord.StringSelectMenuOption {
	options := []discord.StringSelectMenuOption{
		discord.NewStringSelectMenuOption("View Friends", constants.OpenFriendsMenuButtonCustomID).
			WithEmoji(discord.ComponentEmoji{Name: "👥"}).
			WithDescription("View user's friends"),
		discord.NewStringSelectMenuOption("View Groups", constants.OpenGroupsMenuButtonCustomID).
			WithEmoji(discord.ComponentEmoji{Name: "🌐"}).
			WithDescription("View user's groups"),
		discord.NewStringSelectMenuOption("View Outfits", constants.OpenOutfitsMenuButtonCustomID).
			WithEmoji(discord.ComponentEmoji{Name: "👕"}).
			WithDescription("View user's outfits"),
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

	// Add last default option
	options = append(options,
		discord.NewStringSelectMenuOption("Change Review Target", constants.ReviewTargetModeOption).
			WithEmoji(discord.ComponentEmoji{Name: "🎯"}).
			WithDescription("Change what type of users to review"),
	)

	return options
}

// buildReasonOptions creates the reason management options.
func (b *ReviewBuilder) buildReasonOptions() []discord.StringSelectMenuOption {
	reasonTypes := []enum.UserReasonType{
		enum.UserReasonTypeProfile,
		enum.UserReasonTypeFriend,
		enum.UserReasonTypeOutfit,
		enum.UserReasonTypeGroup,
		enum.UserReasonTypeCondo,
		enum.UserReasonTypeChat,
		enum.UserReasonTypeFavorites,
		enum.UserReasonTypeBadges,
	}

	return shared.BuildReasonOptions(b.user.Reasons, reasonTypes, getReasonEmoji, b.ReasonsChanged)
}

// buildAIReasonOptions creates the AI reason generation options.
func (b *ReviewBuilder) buildAIReasonOptions() []discord.StringSelectMenuOption {
	options := []discord.StringSelectMenuOption{
		discord.NewStringSelectMenuOption("Generate Profile Reason", constants.GenerateProfileReasonButtonCustomID).
			WithEmoji(discord.ComponentEmoji{Name: "📝"}).
			WithDescription("Use AI to generate a reason based on profile content"),
		discord.NewStringSelectMenuOption("Generate Friend Reason", constants.GenerateFriendReasonButtonCustomID).
			WithEmoji(discord.ComponentEmoji{Name: "👥"}).
			WithDescription("Use AI to generate a reason based on friend network"),
		discord.NewStringSelectMenuOption("Generate Group Reason", constants.GenerateGroupReasonButtonCustomID).
			WithEmoji(discord.ComponentEmoji{Name: "🌐"}).
			WithDescription("Use AI to generate a reason based on group membership"),
	}

	return options
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

// getDescription returns the description field.
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

	return description
}

// getFriends returns the friends field.
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

// getGroups returns the groups field.
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
				"[%s](https://www.roblox.com/communities/%d)",
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

// getGames returns the games field.
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

// getOutfits returns the outfits field.
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

// getFriendsField returns the friends field name.
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

// getGroupsField returns the groups field name.
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

// getConfirmButtonLabel returns the appropriate label for the confirm button based on review mode.
func (b *ReviewBuilder) getConfirmButtonLabel() string {
	if b.ReviewMode == enum.ReviewModeTraining || !b.IsReviewer {
		return "Report"
	}

	return "Confirm"
}

// getClearButtonLabel returns the appropriate label for the clear button based on review mode.
func (b *ReviewBuilder) getClearButtonLabel() string {
	if b.ReviewMode == enum.ReviewModeTraining || !b.IsReviewer {
		return "Safe"
	}

	return "Clear"
}

// getReasonEmoji returns the appropriate emoji for a reason type.
func getReasonEmoji(reasonType enum.UserReasonType) string {
	switch reasonType {
	case enum.UserReasonTypeProfile:
		return "📝"
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
	case enum.UserReasonTypeFavorites:
		return "🌟"
	case enum.UserReasonTypeBadges:
		return "🏆"
	default:
		return "❓"
	}
}
