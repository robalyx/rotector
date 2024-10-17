package reviewer

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/disgoorg/disgo/discord"
	"github.com/rotector/rotector/internal/common/database"
)

const (
	NotApplicable = "N/A"
)

var multipleNewlinesRegex = regexp.MustCompile(`\n{4,}`)

// ReviewEmbedBuilder builds the embed for the review message.
type ReviewEmbedBuilder struct {
	user *database.PendingUser
}

// NewReviewEmbedBuilder creates a new ReviewEmbedBuilder.
func NewReviewEmbedBuilder(user *database.PendingUser) *ReviewEmbedBuilder {
	return &ReviewEmbedBuilder{user: user}
}

// Build constructs and returns the discord.Embed.
func (b *ReviewEmbedBuilder) Build() discord.Embed {
	embedBuilder := discord.NewEmbedBuilder().
		AddField("ID", fmt.Sprintf("[%d](https://www.roblox.com/users/%d/profile)", b.user.ID, b.user.ID), true).
		AddField("Name", b.user.Name, true).
		AddField("Display Name", b.user.DisplayName, true).
		AddField("Created At", fmt.Sprintf("<t:%d:R>", b.user.CreatedAt.Unix()), true).
		AddField("Confidence", fmt.Sprintf("%.2f", b.user.Confidence), true).
		AddField("Reason", b.user.Reason, false).
		AddField("Description", b.getDescription(), false).
		AddField("Groups", b.getGroups(), false).
		AddField("Friends", b.getFriends(), false).
		AddField("Outfits", b.getOutfits(), false).
		AddField(b.getFlaggedType(), b.getFlaggedContent(), false).
		AddField("Last Updated", fmt.Sprintf("<t:%d:R>", b.user.LastUpdated.Unix()), true).
		AddField("Last Reviewed", b.getLastReviewed(), true).
		SetColor(0x312D2B)

	// Set thumbnail URL or use placeholder image
	if b.user.ThumbnailURL != "" {
		embedBuilder.SetThumbnail(b.user.ThumbnailURL)
	} else {
		embedBuilder.SetThumbnail("attachment://content_deleted.png")
	}

	return embedBuilder.Build()
}

// getDescription returns the description field for the embed.
func (b *ReviewEmbedBuilder) getDescription() string {
	description := b.user.Description

	// Check if description is empty
	if description == "" {
		description = NotApplicable
	}
	// Trim leading and trailing whitespace
	description = strings.TrimSpace(description)
	// Replace multiple newlines with a single newline
	description = multipleNewlinesRegex.ReplaceAllString(description, "\n")
	// Remove all backticks
	description = strings.ReplaceAll(description, "`", "")
	// Enclose in markdown
	description = fmt.Sprintf("```\n%s\n```", description)

	return description
}

// getGroups returns the groups field for the embed.
func (b *ReviewEmbedBuilder) getGroups() string {
	groups := []string{}
	for i, group := range b.user.Groups {
		if i >= 10 {
			groups = append(groups, fmt.Sprintf("... and %d more", len(b.user.Groups)-10))
			break
		}
		groups = append(groups, fmt.Sprintf("[%s](https://www.roblox.com/groups/%d)", group.Group.Name, group.Group.ID))
	}

	if len(groups) == 0 {
		return NotApplicable
	}

	return strings.Join(groups, ", ")
}

// getFriends returns the friends field for the embed.
func (b *ReviewEmbedBuilder) getFriends() string {
	friends := []string{}
	for i, friend := range b.user.Friends {
		if i >= 10 {
			friends = append(friends, fmt.Sprintf("... and %d more", len(b.user.Friends)-10))
			break
		}
		friends = append(friends, fmt.Sprintf("[%s](https://www.roblox.com/users/%d/profile)", friend.Name, friend.ID))
	}

	if len(friends) == 0 {
		return NotApplicable
	}

	return strings.Join(friends, ", ")
}

// getOutfits returns the outfits field for the embed.
func (b *ReviewEmbedBuilder) getOutfits() string {
	outfits := []string{}
	for i, outfit := range b.user.Outfits {
		if i >= 10 {
			outfits = append(outfits, fmt.Sprintf("... and %d more", len(b.user.Outfits)-10))
			break
		}
		outfits = append(outfits, outfit.Name)
	}

	if len(outfits) == 0 {
		return NotApplicable
	}

	return strings.Join(outfits, ", ")
}

// getFlaggedType returns the flagged type field for the embed.
func (b *ReviewEmbedBuilder) getFlaggedType() string {
	if len(b.user.FlaggedGroups) > 0 {
		return "Flagged Groups"
	}
	return "Flagged Content"
}

// getFlaggedContent returns the flagged content field for the embed.
func (b *ReviewEmbedBuilder) getFlaggedContent() string {
	if len(b.user.FlaggedGroups) > 0 {
		var content strings.Builder
		for _, flaggedGroupID := range b.user.FlaggedGroups {
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
			flaggedContent[i] = strings.ReplaceAll(flaggedContent[i], "\n", " ")
		}
		return fmt.Sprintf("- `%s`", strings.Join(flaggedContent, "`\n- `"))
	}

	return NotApplicable
}

// getLastReviewed returns the last reviewed field for the embed.
func (b *ReviewEmbedBuilder) getLastReviewed() string {
	if b.user.LastReviewed.IsZero() {
		return "Never Reviewed"
	}
	return fmt.Sprintf("<t:%d:R>", b.user.LastReviewed.Unix())
}
