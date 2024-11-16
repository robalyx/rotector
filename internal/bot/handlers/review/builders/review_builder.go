package builders

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/rotector/rotector/assets"
	"github.com/rotector/rotector/internal/bot/constants"
	"github.com/rotector/rotector/internal/bot/session"
	"github.com/rotector/rotector/internal/bot/utils"
	"github.com/rotector/rotector/internal/common/database"
	"github.com/rotector/rotector/internal/common/fetcher"
	"github.com/rotector/rotector/internal/common/translator"
)

const (
	// FriendsLimit caps the number of friends shown in the main review embed
	// to prevent the embed from becoming too long.
	FriendsLimit = 10

	// ReviewHistoryLimit caps the number of review history entries shown
	// to keep the embed focused on recent activity.
	ReviewHistoryLimit = 5
)

// Regular expression to clean up excessive newlines in descriptions.
var multipleNewlinesRegex = regexp.MustCompile(`\n{4,}`)

// ReviewEmbed creates the visual layout for reviewing a flagged user.
type ReviewEmbed struct {
	db          *database.Database
	settings    *database.UserSetting
	user        *database.FlaggedUser
	translator  *translator.Translator
	friendTypes map[uint64]string
}

// NewReviewEmbed loads user data and settings from the session state to create
// a new embed builder. The translator is used for description localization.
func NewReviewEmbed(s *session.Session, translator *translator.Translator, db *database.Database) *ReviewEmbed {
	var settings *database.UserSetting
	s.GetInterface(constants.SessionKeyUserSettings, &settings)
	var user *database.FlaggedUser
	s.GetInterface(constants.SessionKeyTarget, &user)
	var friendTypes map[uint64]string
	s.GetInterface(constants.SessionKeyFriendTypes, &friendTypes)

	return &ReviewEmbed{
		db:          db,
		settings:    settings,
		user:        user,
		translator:  translator,
		friendTypes: friendTypes,
	}
}

// Build creates a Discord message with user information in an embed and adds
// interactive components for reviewing the user. The embed includes:
// - Basic user info (ID, name, creation date)
// - User description (translated if non-English)
// - Lists of groups, friends, and outfits
// - Review history from the database.
func (b *ReviewEmbed) Build() *discord.MessageUpdateBuilder {
	// Create the review mode info embed
	var modeTitle string
	var modeDescription string

	switch b.settings.ReviewMode {
	case database.TrainingReviewMode:
		modeTitle = "🎓 " + database.FormatReviewMode(b.settings.ReviewMode)
		modeDescription = "Use upvotes/downvotes to help moderators focus on the most important cases. In this view, information is censored and you will not see any external links."
	case database.StandardReviewMode:
		modeTitle = "⚠️ " + database.FormatReviewMode(b.settings.ReviewMode)
		modeDescription = "Your actions will be recorded and will affect the database. Review users carefully before confirming or clearing them."
	default:
		modeTitle = "❌ Unknown Mode"
		modeDescription = "Error encountered. Please check your settings."
	}

	modeEmbed := discord.NewEmbedBuilder().
		SetTitle(modeTitle).
		SetDescription(modeDescription).
		SetColor(utils.GetMessageEmbedColor(b.settings.StreamerMode))

	// Create main review embed
	reviewEmbed := discord.NewEmbedBuilder().
		SetColor(utils.GetMessageEmbedColor(b.settings.StreamerMode))

	// Add fields based on review mode
	if b.settings.ReviewMode == database.TrainingReviewMode {
		// Training mode - show limited information without links
		reviewEmbed.AddField("ID", utils.CensorString(strconv.FormatUint(b.user.ID, 10), true), true).
			AddField("Name", utils.CensorString(b.user.Name, true), true).
			AddField("Display Name", utils.CensorString(b.user.DisplayName, true), true).
			AddField("Created At", fmt.Sprintf("<t:%d:R>", b.user.CreatedAt.Unix()), true).
			AddField("Last Updated", fmt.Sprintf("<t:%d:R>", b.user.LastUpdated.Unix()), true).
			AddField("Confidence", fmt.Sprintf("%.2f", b.user.Confidence), true).
			AddField("Training Votes", fmt.Sprintf("👍 %d | 👎 %d", b.user.Upvotes, b.user.Downvotes), true).
			AddField("Game Visits", b.getTotalVisits(), true).
			AddField("Reason", b.user.Reason, false).
			AddField("Description", b.getDescription(), false).
			AddField(b.getFriendsField(), b.getFriends(), false).
			AddField("Groups", b.getGroups(), false).
			AddField("Outfits", b.getOutfits(), false).
			AddField("Games", b.getGames(), false).
			AddField(b.getFlaggedType(), b.getFlaggedContent(), false)
	} else {
		// Standard mode - show all information with links
		reviewEmbed.AddField("ID", fmt.Sprintf(
			"[%s](https://www.roblox.com/users/%d/profile)",
			utils.CensorString(strconv.FormatUint(b.user.ID, 10), b.settings.StreamerMode),
			b.user.ID,
		), true).
			AddField("Name", utils.CensorString(b.user.Name, b.settings.StreamerMode), true).
			AddField("Display Name", utils.CensorString(b.user.DisplayName, b.settings.StreamerMode), true).
			AddField("Created At", fmt.Sprintf("<t:%d:R>", b.user.CreatedAt.Unix()), true).
			AddField("Last Updated", fmt.Sprintf("<t:%d:R>", b.user.LastUpdated.Unix()), true).
			AddField("Confidence", fmt.Sprintf("%.2f", b.user.Confidence), true).
			AddField("Training Votes", fmt.Sprintf("👍 %d | 👎 %d", b.user.Upvotes, b.user.Downvotes), true).
			AddField("Game Visits", b.getTotalVisits(), true).
			AddField("Reason", b.user.Reason, false).
			AddField("Description", b.getDescription(), false).
			AddField(b.getFriendsField(), b.getFriends(), false).
			AddField("Groups", b.getGroups(), false).
			AddField("Outfits", b.getOutfits(), false).
			AddField("Games", b.getGames(), false).
			AddField(b.getFlaggedType(), b.getFlaggedContent(), false).
			AddField("Review History", b.getReviewHistory(), false)
	}

	// Add user thumbnail or placeholder image (existing code)
	builder := discord.NewMessageUpdateBuilder()
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

	// Add both embeds and components to the message
	return builder.
		SetEmbeds(modeEmbed.Build(), reviewEmbed.Build()). // Add both embeds
		AddContainerComponents(b.buildComponents()...)     // Move components to separate method for cleaner code
}

// buildComponents creates the interactive components for the review menu.
func (b *ReviewEmbed) buildComponents() []discord.ContainerComponent {
	// Create the mode switch option based on current mode
	var modeSwitchOption discord.StringSelectMenuOption
	var confirmButtonLabel string
	var clearButtonLabel string

	switch b.settings.ReviewMode {
	case database.TrainingReviewMode:
		modeSwitchOption = discord.NewStringSelectMenuOption("Switch to Standard Mode", constants.SwitchReviewModeCustomID).
			WithEmoji(discord.ComponentEmoji{Name: "⚠️"}).
			WithDescription("Switch to standard mode for actual moderation")
		confirmButtonLabel = "Upvote"
		clearButtonLabel = "Downvote"
	case database.StandardReviewMode:
		modeSwitchOption = discord.NewStringSelectMenuOption("Switch to Training Mode", constants.SwitchReviewModeCustomID).
			WithEmoji(discord.ComponentEmoji{Name: "🎓"}).
			WithDescription("Switch to training mode to practice")
		confirmButtonLabel = "Confirm"
		clearButtonLabel = "Clear"
	}

	return []discord.ContainerComponent{
		// Sorting options menu
		discord.NewActionRow(
			discord.NewStringSelectMenu(constants.SortOrderSelectMenuCustomID, "Sorting",
				discord.NewStringSelectMenuOption("Selected by random", database.SortByRandom).
					WithDefault(b.settings.DefaultSort == database.SortByRandom).
					WithEmoji(discord.ComponentEmoji{Name: "🔀"}),
				discord.NewStringSelectMenuOption("Selected by confidence", database.SortByConfidence).
					WithDefault(b.settings.DefaultSort == database.SortByConfidence).
					WithEmoji(discord.ComponentEmoji{Name: "🔮"}),
				discord.NewStringSelectMenuOption("Selected by last updated time", database.SortByLastUpdated).
					WithDefault(b.settings.DefaultSort == database.SortByLastUpdated).
					WithEmoji(discord.ComponentEmoji{Name: "📅"}),
				discord.NewStringSelectMenuOption("Selected by community votes", database.SortByReputation).
					WithDefault(b.settings.DefaultSort == database.SortByReputation).
					WithEmoji(discord.ComponentEmoji{Name: "👥"}),
			),
		),
		// Action options menu
		discord.NewActionRow(
			discord.NewStringSelectMenu(constants.ActionSelectMenuCustomID, "Actions",
				discord.NewStringSelectMenuOption("Confirm with reason", constants.ConfirmWithReasonButtonCustomID).
					WithEmoji(discord.ComponentEmoji{Name: "🚫"}).
					WithDescription("Confirm the user with a custom reason"),
				discord.NewStringSelectMenuOption("Recheck user", constants.RecheckButtonCustomID).
					WithEmoji(discord.ComponentEmoji{Name: "🔄"}).
					WithDescription("Add user to high priority queue for recheck"),
				discord.NewStringSelectMenuOption("View user logs", constants.ViewUserLogsButtonCustomID).
					WithEmoji(discord.ComponentEmoji{Name: "📋"}).
					WithDescription("View activity logs for this user"),
				modeSwitchOption,
				discord.NewStringSelectMenuOption("Open friends viewer", constants.OpenFriendsMenuButtonCustomID).
					WithEmoji(discord.ComponentEmoji{Name: "👫"}).
					WithDescription("View all user friends"),
				discord.NewStringSelectMenuOption("Open group viewer", constants.OpenGroupsMenuButtonCustomID).
					WithEmoji(discord.ComponentEmoji{Name: "🌐"}).
					WithDescription("View all user groups"),
				discord.NewStringSelectMenuOption("Open outfit viewer", constants.OpenOutfitsMenuButtonCustomID).
					WithEmoji(discord.ComponentEmoji{Name: "👕"}).
					WithDescription("View all user outfits"),
			),
		),
		// Quick action buttons
		discord.NewActionRow(
			discord.NewSecondaryButton("◀️", constants.BackButtonCustomID),
			discord.NewDangerButton(confirmButtonLabel, constants.ConfirmButtonCustomID),
			discord.NewSuccessButton(clearButtonLabel, constants.ClearButtonCustomID),
			discord.NewSecondaryButton("Skip", constants.SkipButtonCustomID),
		),
	}
}

// getDescription returns the description field for the embed.
func (b *ReviewEmbed) getDescription() string {
	description := b.user.Description

	// Check if description is empty
	if description == "" {
		return constants.NotApplicable
	}

	// Trim leading and trailing whitespace
	description = strings.TrimSpace(description)
	// Replace multiple newlines with a single newline
	description = multipleNewlinesRegex.ReplaceAllString(description, "\n")
	// Remove all backticks
	description = strings.ReplaceAll(description, "`", "")
	// Enclose in markdown
	description = fmt.Sprintf("```\n%s\n```", description)

	// Translate the description
	translatedDescription, err := b.translator.Translate(context.Background(), description, "auto", "en")
	if err == nil && translatedDescription != description {
		return "(translated)\n" + translatedDescription
	}

	return description
}

// getFlaggedType returns the flagged type field for the embed.
func (b *ReviewEmbed) getFlaggedType() string {
	if len(b.user.FlaggedGroups) > 0 {
		return "Flagged Groups"
	}
	return "Flagged Content"
}

// getFlaggedContent returns the flagged content field for the embed.
func (b *ReviewEmbed) getFlaggedContent() string {
	flaggedGroups := b.user.FlaggedGroups
	if len(flaggedGroups) > 0 {
		var content strings.Builder
		for _, flaggedGroupID := range flaggedGroups {
			for _, group := range b.user.Groups {
				if group.Group.ID == flaggedGroupID {
					content.WriteString(fmt.Sprintf("- [%s](https://www.roblox.com/groups/%d) (%s)\n",
						group.Group.Name, group.Group.ID, group.Role.Name))
					break
				}
			}
		}
		return content.String()
	}

	flaggedContent := b.user.FlaggedContent
	if len(flaggedContent) > 0 {
		for i := range flaggedContent {
			// Remove all newlines and backticks
			flaggedContent[i] = utils.NormalizeString(flaggedContent[i])
		}
		return fmt.Sprintf("- `%s`", strings.Join(flaggedContent, "`\n- `"))
	}

	return constants.NotApplicable
}

// getReviewHistory returns the review history field for the embed.
func (b *ReviewEmbed) getReviewHistory() string {
	logs, total, err := b.db.UserActivity().GetLogs(context.Background(), b.user.ID, 0, database.ActivityTypeAll, time.Time{}, time.Time{}, 0, ReviewHistoryLimit)
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

	if total > ReviewHistoryLimit {
		history = append(history, fmt.Sprintf("... and %d more", total-ReviewHistoryLimit))
	}

	return strings.Join(history, "\n")
}

// getFriends returns the friends field for the embed.
func (b *ReviewEmbed) getFriends() string {
	friends := make([]string, 0, FriendsLimit)
	isTraining := b.settings.ReviewMode == database.TrainingReviewMode

	for i, friend := range b.user.Friends {
		if i >= FriendsLimit {
			break
		}

		name := utils.CensorString(friend.Name, isTraining || b.settings.StreamerMode)
		if isTraining {
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
	if len(b.user.Friends) > FriendsLimit {
		result += fmt.Sprintf(" ... and %d more", len(b.user.Friends)-FriendsLimit)
	}

	return result
}

// getGroups returns the groups field for the embed.
func (b *ReviewEmbed) getGroups() string {
	groups := []string{}
	isTraining := b.settings.ReviewMode == database.TrainingReviewMode

	for i, group := range b.user.Groups {
		if i >= 10 {
			groups = append(groups, fmt.Sprintf("... and %d more", len(b.user.Groups)-10))
			break
		}

		name := utils.CensorString(group.Group.Name, isTraining || b.settings.StreamerMode)
		if isTraining {
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

	return strings.Join(groups, ", ")
}

// getTotalVisits returns the total visits across all games.
func (b *ReviewEmbed) getTotalVisits() string {
	if len(b.user.Games) == 0 {
		return constants.NotApplicable
	}

	var totalVisits uint64
	for _, game := range b.user.Games {
		totalVisits += game.PlaceVisits
	}

	return utils.FormatNumber(totalVisits)
}

// getGames returns the games field for the embed.
func (b *ReviewEmbed) getGames() string {
	if len(b.user.Games) == 0 {
		return constants.NotApplicable
	}

	// Format games list with visit counts
	games := []string{}
	isTraining := b.settings.ReviewMode == database.TrainingReviewMode

	for i, game := range b.user.Games {
		if i >= 10 {
			games = append(games, fmt.Sprintf("... and %d more", len(b.user.Games)-10))
			break
		}

		name := utils.CensorString(game.Name, isTraining || b.settings.StreamerMode)
		visits := utils.FormatNumber(game.PlaceVisits)

		if isTraining {
			games = append(games, fmt.Sprintf("%s (%s visits)", name, visits))
		} else {
			games = append(games, fmt.Sprintf("[%s](https://www.roblox.com/games/%d) (%s visits)",
				name, game.ID, visits))
		}
	}

	return strings.Join(games, ", ")
}

// getOutfits returns the outfits field for the embed.
func (b *ReviewEmbed) getOutfits() string {
	// Get the first 10 outfits
	outfits := []string{}
	for i, outfit := range b.user.Outfits {
		if i >= 10 {
			outfits = append(outfits, fmt.Sprintf("... and %d more", len(b.user.Outfits)-10))
			break
		}
		outfits = append(outfits, outfit.Name)
	}
	// If no outfits are found, return NotApplicable
	if len(outfits) == 0 {
		return constants.NotApplicable
	}

	return strings.Join(outfits, ", ")
}

// getFriendsField returns the friends field name for the embed.
func (b *ReviewEmbed) getFriendsField() string {
	if len(b.friendTypes) > 0 {
		confirmedCount := 0
		flaggedCount := 0
		for _, friendType := range b.friendTypes {
			if friendType == database.UserTypeConfirmed {
				confirmedCount++
			} else if friendType == database.UserTypeFlagged {
				flaggedCount++
			}
		}

		return fmt.Sprintf("Friends (%d ⚠️, %d ⏳)", confirmedCount, flaggedCount)
	}
	return "Friends"
}
