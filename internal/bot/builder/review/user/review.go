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
	"github.com/robalyx/rotector/internal/common/translator"
)

// ReviewBuilder creates the visual layout for reviewing a user.
type ReviewBuilder struct {
	db             *database.Client
	settings       *types.UserSetting
	botSettings    *types.BotSetting
	userID         uint64
	user           *types.ReviewUser
	translator     *translator.Translator
	flaggedFriends map[uint64]*types.ReviewUser
	flaggedGroups  map[uint64]*types.ReviewGroup
	isTraining     bool
}

// NewReviewBuilder creates a new review builder.
func NewReviewBuilder(s *session.Session, translator *translator.Translator, db *database.Client) *ReviewBuilder {
	var settings *types.UserSetting
	s.GetInterface(constants.SessionKeyUserSettings, &settings)
	var botSettings *types.BotSetting
	s.GetInterface(constants.SessionKeyBotSettings, &botSettings)
	var user *types.ReviewUser
	s.GetInterface(constants.SessionKeyTarget, &user)
	var flaggedFriends map[uint64]*types.ReviewUser
	s.GetInterface(constants.SessionKeyFlaggedFriends, &flaggedFriends)
	var flaggedGroups map[uint64]*types.ReviewGroup
	s.GetInterface(constants.SessionKeyFlaggedGroups, &flaggedGroups)

	return &ReviewBuilder{
		db:             db,
		settings:       settings,
		botSettings:    botSettings,
		userID:         s.UserID(),
		user:           user,
		translator:     translator,
		flaggedFriends: flaggedFriends,
		flaggedGroups:  flaggedGroups,
		isTraining:     settings.ReviewMode == types.TrainingReviewMode,
	}
}

// Build creates a Discord message with user information in an embed and adds
// interactive components for reviewing the user.
func (b *ReviewBuilder) Build() *discord.MessageUpdateBuilder {
	builder := discord.NewMessageUpdateBuilder()

	// Create embeds
	modeEmbed := b.buildModeEmbed()
	reviewEmbed := b.buildReviewBuilder()

	// Create components
	components := b.buildComponents()

	// Create builder and handle thumbnail
	if b.user.ThumbnailURL != "" && b.user.ThumbnailURL != fetcher.ThumbnailPlaceholder {
		reviewEmbed.SetThumbnail(b.user.ThumbnailURL)
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
	switch b.settings.ReviewMode {
	case types.TrainingReviewMode:
		mode = "🎓 Training Mode"
		description += `
		**You are not an official reviewer.**
		You may help moderators by downvoting to indicate inappropriate activity. Information is censored and external links are disabled.
		`
	case types.StandardReviewMode:
		mode = "⚠️ Standard Mode"
		description += `
		Your actions are recorded and affect the database. Please review carefully before taking action.
		`
	default:
		mode = "❌ Unknown Mode"
		description += "Error encountered. Please check your settings."
	}

	return discord.NewEmbedBuilder().
		SetTitle(mode).
		SetDescription(description).
		SetColor(utils.GetMessageEmbedColor(b.isTraining || b.settings.StreamerMode))
}

// buildReviewBuilder creates the main review information embed.
func (b *ReviewBuilder) buildReviewBuilder() *discord.EmbedBuilder {
	embed := discord.NewEmbedBuilder().
		SetColor(utils.GetMessageEmbedColor(b.isTraining || b.settings.StreamerMode)).
		SetTitle(fmt.Sprintf("🛡️ %d Safe • ⚠️ %d Reports",
			b.user.Reputation.Upvotes,
			b.user.Reputation.Downvotes,
		))

	// Add status indicator based on user status
	var status string
	switch b.user.Status {
	case types.UserTypeFlagged:
		status = "⏳ Flagged User"
	case types.UserTypeConfirmed:
		status = "⚠️ Confirmed User"
	case types.UserTypeCleared:
		status = "✅ Cleared User"
	case types.UserTypeBanned:
		status = "🔨 Banned User"
	case types.UserTypeUnflagged:
		status = "🔄 Unflagged User"
	}

	createdAt := fmt.Sprintf("<t:%d:R>", b.user.CreatedAt.Unix())
	lastUpdated := fmt.Sprintf("<t:%d:R>", b.user.LastUpdated.Unix())
	confidence := fmt.Sprintf("%.2f", b.user.Confidence)
	followerCount := utils.FormatNumber(b.user.FollowerCount)
	followingCount := utils.FormatNumber(b.user.FollowingCount)

	// Censor reason if needed
	reason := utils.CensorStringsInText(
		b.user.Reason,
		b.isTraining || b.settings.StreamerMode,
		strconv.FormatUint(b.user.ID, 10),
		b.user.Name,
		b.user.DisplayName,
	)

	if b.settings.ReviewMode == types.TrainingReviewMode {
		// Training mode - show limited information without links
		embed.AddField("ID", utils.CensorString(strconv.FormatUint(b.user.ID, 10), true), true).
			AddField("Name", utils.CensorString(b.user.Name, true), true).
			AddField("Display Name", utils.CensorString(b.user.DisplayName, true), true).
			AddField("Followers", followerCount, true).
			AddField("Following", followingCount, true).
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

		if len(b.user.FlaggedContent) != 0 {
			embed.AddField("Flagged Content", b.getFlaggedContent(), false)
		}
	} else {
		// Standard mode - show all information with links
		embed.AddField("ID", fmt.Sprintf(
			"[%s](https://www.roblox.com/users/%d/profile)",
			utils.CensorString(strconv.FormatUint(b.user.ID, 10), b.settings.StreamerMode),
			b.user.ID,
		), true).
			AddField("Name", utils.CensorString(b.user.Name, b.settings.StreamerMode), true).
			AddField("Display Name", utils.CensorString(b.user.DisplayName, b.settings.StreamerMode), true).
			AddField("Followers", followerCount, true).
			AddField("Following", followingCount, true).
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

		if len(b.user.FlaggedContent) != 0 {
			embed.AddField("Flagged Content", b.getFlaggedContent(), false)
		}
		embed.AddField("Review History", b.getReviewHistory(), false)
	}

	// Add status-specific timestamps
	if !b.user.VerifiedAt.IsZero() {
		embed.AddField("Verified At", fmt.Sprintf("<t:%d:R>", b.user.VerifiedAt.Unix()), true)
	}
	if !b.user.ClearedAt.IsZero() {
		embed.AddField("Cleared At", fmt.Sprintf("<t:%d:R>", b.user.ClearedAt.Unix()), true)
	}
	if !b.user.PurgedAt.IsZero() {
		embed.AddField("Purged At", fmt.Sprintf("<t:%d:R>", b.user.PurgedAt.Unix()), true)
	}

	// Add UUID and status to footer
	embed.SetFooter(fmt.Sprintf("%s • UUID: %s", status, b.user.UUID.String()), "")

	return embed
}

// buildSortingOptions creates the sorting options.
func (b *ReviewBuilder) buildSortingOptions() []discord.StringSelectMenuOption {
	return []discord.StringSelectMenuOption{
		discord.NewStringSelectMenuOption("Selected by random", string(types.ReviewSortByRandom)).
			WithDefault(b.settings.UserDefaultSort == types.ReviewSortByRandom).
			WithEmoji(discord.ComponentEmoji{Name: "🔀"}),
		discord.NewStringSelectMenuOption("Selected by confidence", string(types.ReviewSortByConfidence)).
			WithDefault(b.settings.UserDefaultSort == types.ReviewSortByConfidence).
			WithEmoji(discord.ComponentEmoji{Name: "🔮"}),
		discord.NewStringSelectMenuOption("Selected by last updated time", string(types.ReviewSortByLastUpdated)).
			WithDefault(b.settings.UserDefaultSort == types.ReviewSortByLastUpdated).
			WithEmoji(discord.ComponentEmoji{Name: "📅"}),
		discord.NewStringSelectMenuOption("Selected by bad reputation", string(types.ReviewSortByReputation)).
			WithDefault(b.settings.UserDefaultSort == types.ReviewSortByReputation).
			WithEmoji(discord.ComponentEmoji{Name: "👎"}),
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
	if b.botSettings.IsReviewer(b.userID) {
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
			discord.NewStringSelectMenuOption("Confirm with reason", constants.ConfirmWithReasonButtonCustomID).
				WithEmoji(discord.ComponentEmoji{Name: "🚫"}).
				WithDescription("Confirm the user with a custom reason"),
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

// getConfirmButtonLabel returns the appropriate label for the confirm button based on review mode.
func (b *ReviewBuilder) getConfirmButtonLabel() string {
	if b.settings.ReviewMode == types.TrainingReviewMode {
		return "Report"
	}
	return "Confirm"
}

// getClearButtonLabel returns the appropriate label for the clear button based on review mode.
func (b *ReviewBuilder) getClearButtonLabel() string {
	if b.settings.ReviewMode == types.TrainingReviewMode {
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
		b.isTraining || b.settings.StreamerMode,
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

// getFlaggedContent returns the flagged content field for the embed.
func (b *ReviewBuilder) getFlaggedContent() string {
	content := make([]string, 0, 5)
	for i, item := range b.user.FlaggedContent {
		if i >= 5 {
			content = append(content, "... and more")
			break
		}
		newItem := utils.TruncateString(item, 100)
		newItem = utils.NormalizeString(newItem)
		content = append(content, fmt.Sprintf("- `%s`", newItem))
	}

	return strings.Join(content, "\n")
}

// getReviewHistory returns the review history field for the embed.
func (b *ReviewBuilder) getReviewHistory() string {
	logs, nextCursor, err := b.db.Activity().GetLogs(
		context.Background(),
		types.ActivityFilter{
			UserID:       b.user.ID,
			GroupID:      0,
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

// getFriends returns the friends field for the embed.
func (b *ReviewBuilder) getFriends() string {
	friends := make([]string, 0, constants.ReviewFriendsLimit)

	for i, friend := range b.user.Friends {
		if i >= constants.ReviewFriendsLimit {
			break
		}

		name := utils.CensorString(friend.Name, b.isTraining || b.settings.StreamerMode)
		if b.isTraining {
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

		name := utils.CensorString(group.Group.Name, b.isTraining || b.settings.StreamerMode)
		if b.isTraining {
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

		name := utils.CensorString(game.Name, b.isTraining || b.settings.StreamerMode)
		visits := utils.FormatNumber(game.PlaceVisits)

		if b.isTraining {
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
	counts := make(map[types.UserType]int)
	for _, friend := range b.flaggedFriends {
		counts[friend.Status]++
	}

	// Build status parts
	var parts []string
	if c := counts[types.UserTypeConfirmed]; c > 0 {
		parts = append(parts, fmt.Sprintf("%d ⚠️", c))
	}
	if c := counts[types.UserTypeFlagged]; c > 0 {
		parts = append(parts, fmt.Sprintf("%d ⏳", c))
	}
	if c := counts[types.UserTypeBanned]; c > 0 {
		parts = append(parts, fmt.Sprintf("%d 🔨", c))
	}
	if c := counts[types.UserTypeCleared]; c > 0 {
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
	counts := make(map[types.GroupType]int)
	for _, group := range b.flaggedGroups {
		counts[group.Status]++
	}

	// Build status parts
	var parts []string
	if c := counts[types.GroupTypeConfirmed]; c > 0 {
		parts = append(parts, fmt.Sprintf("%d ⚠️", c))
	}
	if c := counts[types.GroupTypeFlagged]; c > 0 {
		parts = append(parts, fmt.Sprintf("%d ⏳", c))
	}
	if c := counts[types.GroupTypeCleared]; c > 0 {
		parts = append(parts, fmt.Sprintf("%d ✅", c))
	}
	if c := counts[types.GroupTypeLocked]; c > 0 {
		parts = append(parts, fmt.Sprintf("%d 🔒", c))
	}

	if len(parts) > 0 {
		return "Groups (" + strings.Join(parts, ", ") + ")"
	}
	return "Groups"
}
