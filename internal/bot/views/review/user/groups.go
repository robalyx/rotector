package user

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"

	"github.com/disgoorg/disgo/discord"
	apiTypes "github.com/jaxron/roapi.go/pkg/api/types"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/bot/utils"
	"github.com/robalyx/rotector/internal/bot/views/review/shared"
	"github.com/robalyx/rotector/internal/database/types"
	"github.com/robalyx/rotector/internal/database/types/enum"
)

// GroupsBuilder creates the visual layout for viewing a user's groups.
type GroupsBuilder struct {
	user          *types.ReviewUser
	groups        []*apiTypes.UserGroupRoles
	flaggedGroups map[uint64]*types.ReviewGroup
	start         int
	page          int
	totalItems    int
	totalPages    int
	imageBuffer   *bytes.Buffer
	isStreaming   bool
	isReviewer    bool
	trainingMode  bool
	privacyMode   bool
}

// NewGroupsBuilder creates a new groups builder.
func NewGroupsBuilder(s *session.Session) *GroupsBuilder {
	trainingMode := session.UserReviewMode.Get(s) == enum.ReviewModeTraining

	return &GroupsBuilder{
		user:          session.UserTarget.Get(s),
		groups:        session.UserGroups.Get(s),
		flaggedGroups: session.UserFlaggedGroups.Get(s),
		start:         session.PaginationOffset.Get(s),
		page:          session.PaginationPage.Get(s),
		totalItems:    session.PaginationTotalItems.Get(s),
		totalPages:    session.PaginationTotalPages.Get(s),
		imageBuffer:   session.ImageBuffer.Get(s),
		isStreaming:   session.PaginationIsStreaming.Get(s),
		isReviewer:    s.BotSettings().IsReviewer(session.UserID.Get(s)),
		trainingMode:  trainingMode,
		privacyMode:   trainingMode || session.UserStreamerMode.Get(s),
	}
}

// Build creates a Discord message with a grid of group thumbnails and information.
func (b *GroupsBuilder) Build() *discord.MessageUpdateBuilder {
	// Create file attachment for the group thumbnails grid
	fileName := fmt.Sprintf("groups_%d_%d.png", b.user.ID, b.page)

	// Build content
	var content strings.Builder
	content.WriteString("## User Groups\n")
	content.WriteString(fmt.Sprintf("```%s (%s)```",
		utils.CensorString(b.user.Name, b.privacyMode),
		utils.CensorString(strconv.FormatUint(b.user.ID, 10), b.privacyMode),
	))

	// Add groups list
	for i, group := range b.groups {
		// Add row indicator at the start of each row
		if i%constants.GroupsGridColumns == 0 {
			content.WriteString("\n\n**Row " + strconv.Itoa((i/constants.GroupsGridColumns)+1) + "**")
		}

		// Add group name with status indicators
		content.WriteString("\n" + b.getGroupFieldName(group))

		// Add group details
		details := b.getGroupFieldValue(group)
		if details != "" {
			content.WriteString("\n" + details)
		}
	}

	// Add page info at the bottom
	content.WriteString(fmt.Sprintf("\n\n-# Page %d/%d", b.page+1, b.totalPages+1))

	// Build container with all components
	container := discord.NewContainer(
		discord.NewTextDisplay(content.String()),
		discord.NewMediaGallery(discord.MediaGalleryItem{
			Media: discord.UnfurledMediaItem{
				URL: "attachment://" + fileName,
			},
		}),
	).WithAccentColor(utils.GetContainerColor(b.privacyMode))

	// Add group reason section if exists
	if reason, ok := b.user.Reasons[enum.UserReasonTypeGroup]; ok {
		var reasonContent strings.Builder
		reasonContent.WriteString(shared.BuildSingleReasonDisplay(
			b.privacyMode,
			enum.UserReasonTypeGroup,
			reason,
			200,
			strconv.FormatUint(b.user.ID, 10),
			b.user.Name,
			b.user.DisplayName))

		container = container.AddComponents(
			discord.NewLargeSeparator(),
			discord.NewTextDisplay(reasonContent.String()),
			discord.NewLargeSeparator(),
		)
	}

	// Add edit button if reviewer and not in training mode
	if b.isReviewer && !b.trainingMode && !b.isStreaming {
		container = container.AddComponents(
			discord.NewActionRow(
				discord.NewSecondaryButton("Edit Reason", constants.EditReasonButtonCustomID),
			),
		)
	}

	// Add pagination buttons if not streaming
	if !b.isStreaming {
		container = container.AddComponents(
			discord.NewActionRow(
				discord.NewSecondaryButton("⏮️", string(session.ViewerFirstPage)).WithDisabled(b.page == 0),
				discord.NewSecondaryButton("◀️", string(session.ViewerPrevPage)).WithDisabled(b.page == 0),
				discord.NewSecondaryButton("▶️", string(session.ViewerNextPage)).WithDisabled(b.page == b.totalPages),
				discord.NewSecondaryButton("⏭️", string(session.ViewerLastPage)).WithDisabled(b.page == b.totalPages),
			),
		)
	}

	// Build message with container and back button
	builder := discord.NewMessageUpdateBuilder().
		SetFiles(discord.NewFile(fileName, "", b.imageBuffer)).
		AddComponents(container)

	// Add back button if not streaming
	if !b.isStreaming {
		builder.AddComponents(
			discord.NewActionRow(
				discord.NewSecondaryButton("◀️ Back", constants.BackButtonCustomID),
			),
		)
	}

	return builder
}

// getGroupFieldName creates the field name for a group entry.
func (b *GroupsBuilder) getGroupFieldName(group *apiTypes.UserGroupRoles) string {
	var indicators []string

	// Add status indicator based on group status
	if reviewGroup, ok := b.flaggedGroups[group.Group.ID]; ok {
		switch reviewGroup.Status {
		case enum.GroupTypeConfirmed:
			indicators = append(indicators, "⚠️")
		case enum.GroupTypeFlagged:
			indicators = append(indicators, "⏳")
		case enum.GroupTypeCleared:
			indicators = append(indicators, "✅")
		}

		// Add locked status if applicable
		if reviewGroup.IsLocked {
			indicators = append(indicators, "🔒")
		}
	}

	// Add verification badge if group is verified
	if group.Group.HasVerifiedBadge {
		indicators = append(indicators, "✓")
	}

	// Add group name (with link in standard mode)
	name := utils.CensorString(group.Group.Name, b.privacyMode)
	if !b.privacyMode {
		name = fmt.Sprintf("[%s](https://www.roblox.com/communities/%d)", name, group.Group.ID)
	}

	if len(indicators) > 0 {
		return fmt.Sprintf("### %s %s", name, strings.Join(indicators, " "))
	}

	return "### " + name
}

// getGroupFieldValue creates the field value for a group entry.
func (b *GroupsBuilder) getGroupFieldValue(group *apiTypes.UserGroupRoles) string {
	var info []string

	// Add member count and user's role
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("👥 `%s` • 👤 `%s`",
		utils.FormatNumber(group.Group.MemberCount),
		group.Role.Name))

	// Add owner info if available
	if group.Group.Owner != nil {
		username := utils.CensorString(group.Group.Owner.Username, b.privacyMode)

		builder.WriteString(" • 👑 ")

		if b.privacyMode {
			builder.WriteString(username)
		} else {
			builder.WriteString(fmt.Sprintf("[%s](https://www.roblox.com/users/%d/profile)",
				username,
				group.Group.Owner.UserID))
		}
	}

	info = append(info, builder.String())

	// Add status indicators (locked, private)
	var status []string
	if group.Group.IsLocked != nil && *group.Group.IsLocked {
		status = append(status, "🔒 Locked")
	}

	if !group.Group.PublicEntryAllowed {
		status = append(status, "🚫 Private")
	}

	if len(status) > 0 {
		info = append(info, strings.Join(status, " • "))
	}

	// Add flagged info and confidence if available
	if flaggedGroup, ok := b.flaggedGroups[group.Group.ID]; ok {
		if flaggedGroup.Confidence > 0 {
			info = append(info, fmt.Sprintf("🔮 `%.2f`", flaggedGroup.Confidence))
		}

		if len(flaggedGroup.Reasons) > 0 {
			for reasonType, reason := range flaggedGroup.Reasons {
				info = append(info, fmt.Sprintf("`[%s] %s`", reasonType, reason.Message))
			}
		}
	}

	// Add truncated description if available
	if group.Group.Description != "" {
		desc := utils.NormalizeString(group.Group.Description)
		desc = utils.TruncateString(desc, 200)
		desc = utils.FormatString(desc)
		info = append(info, desc)
	}

	return strings.Join(info, "\n")
}
