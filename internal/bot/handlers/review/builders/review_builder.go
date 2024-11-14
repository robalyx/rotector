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
	// Create embed with user information fields
	embed := discord.NewEmbedBuilder().
		AddField("ID", fmt.Sprintf(
			"[%s](https://www.roblox.com/users/%d/profile)",
			utils.CensorString(strconv.FormatUint(b.user.ID, 10), b.settings.StreamerMode),
			b.user.ID,
		), true).
		AddField("Name", utils.CensorString(b.user.Name, b.settings.StreamerMode), true).
		AddField("Display Name", utils.CensorString(b.user.DisplayName, b.settings.StreamerMode), true).
		AddField("Created At", fmt.Sprintf("<t:%d:R>", b.user.CreatedAt.Unix()), true).
		AddField("Last Updated", fmt.Sprintf("<t:%d:R>", b.user.LastUpdated.Unix()), true).
		AddField("Confidence", fmt.Sprintf("%.2f", b.user.Confidence), true).
		AddField("Reason", b.user.Reason, true).
		AddField("Description", b.getDescription(), false).
		AddField("Groups", b.getGroups(), false).
		AddField(b.getFriendsField(), b.getFriends(), false).
		AddField("Outfits", b.getOutfits(), false).
		AddField(b.getFlaggedType(), b.getFlaggedContent(), false).
		AddField("Review History", b.getReviewHistory(), false).
		SetColor(utils.GetMessageEmbedColor(b.settings.StreamerMode))

	// Add interactive components for sorting and actions
	components := []discord.ContainerComponent{
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
				discord.NewStringSelectMenuOption("Open outfit viewer", constants.OpenOutfitsMenuButtonCustomID).
					WithEmoji(discord.ComponentEmoji{Name: "👕"}).
					WithDescription("View all user outfits"),
				discord.NewStringSelectMenuOption("Open friends viewer", constants.OpenFriendsMenuButtonCustomID).
					WithEmoji(discord.ComponentEmoji{Name: "👫"}).
					WithDescription("View all user friends"),
				discord.NewStringSelectMenuOption("Open group viewer", constants.OpenGroupsMenuButtonCustomID).
					WithEmoji(discord.ComponentEmoji{Name: "🌐"}).
					WithDescription("View all user groups"),
			),
		),
		// Quick action buttons
		discord.NewActionRow(
			discord.NewSecondaryButton("◀️", constants.BackButtonCustomID),
			discord.NewDangerButton("Confirm", constants.ConfirmButtonCustomID),
			discord.NewSuccessButton("Clear", constants.ClearButtonCustomID),
			discord.NewSecondaryButton("Skip", constants.SkipButtonCustomID),
		),
	}

	// Create the message builder
	builder := discord.NewMessageUpdateBuilder()

	// Add user thumbnail or placeholder image
	if b.user.ThumbnailURL != "" && b.user.ThumbnailURL != fetcher.ThumbnailPlaceholder {
		embed.SetThumbnail(b.user.ThumbnailURL)
	} else {
		// Load and attach placeholder image
		placeholderImage, err := assets.Images.Open("images/content_deleted.png")
		if err == nil {
			builder.SetFiles(discord.NewFile("content_deleted.png", "", placeholderImage))
			_ = placeholderImage.Close()
		}

		embed.SetThumbnail("attachment://content_deleted.png")
	}

	return builder.
		SetEmbeds(embed.Build()).
		AddContainerComponents(components...)
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

// getGroups returns the groups field for the embed.
func (b *ReviewEmbed) getGroups() string {
	// Get the first 10 groups
	groups := []string{}
	for i, group := range b.user.Groups {
		if i >= 10 {
			groups = append(groups, fmt.Sprintf("... and %d more", len(b.user.Groups)-10))
			break
		}
		groups = append(groups, fmt.Sprintf(
			"[%s](https://www.roblox.com/groups/%d)",
			utils.CensorString(group.Group.Name, b.settings.StreamerMode),
			group.Group.ID,
		))
	}

	// If no groups are found, return NotApplicable
	if len(groups) == 0 {
		return constants.NotApplicable
	}

	return strings.Join(groups, ", ")
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

// getFriends returns the friends field for the embed.
func (b *ReviewEmbed) getFriends() string {
	// Get the first 10 friends
	friends := make([]string, 0, FriendsLimit)
	for i, friend := range b.user.Friends {
		if i >= FriendsLimit {
			break
		}
		friends = append(friends, fmt.Sprintf(
			"[%s](https://www.roblox.com/users/%d/profile)",
			utils.CensorString(friend.Name, b.settings.StreamerMode),
			friend.ID,
		))
	}

	// If no friends are found, return NotApplicable
	if len(friends) == 0 {
		return constants.NotApplicable
	}

	// Add "and more" if there are more friends
	result := strings.Join(friends, ", ")
	if len(b.user.Friends) > FriendsLimit {
		result += fmt.Sprintf(" ... and %d more", len(b.user.Friends)-FriendsLimit)
	}

	return result
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
