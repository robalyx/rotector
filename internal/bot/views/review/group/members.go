package group

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

// MembersBuilder creates the visual layout for viewing a group's flagged members.
type MembersBuilder struct {
	group        *types.ReviewGroup
	presences    map[int64]*apiTypes.UserPresenceResponse
	members      map[int64]*types.ReviewUser
	memberIDs    []int64
	start        int
	page         int
	totalItems   int
	totalPages   int
	imageBuffer  *bytes.Buffer
	isStreaming  bool
	isReviewer   bool
	trainingMode bool
	privacyMode  bool
}

// NewMembersBuilder creates a new members builder.
func NewMembersBuilder(s *session.Session) *MembersBuilder {
	trainingMode := session.UserReviewMode.Get(s) == enum.ReviewModeTraining

	return &MembersBuilder{
		group:        session.GroupTarget.Get(s),
		presences:    session.UserPresences.Get(s),
		members:      session.GroupPageFlaggedMembers.Get(s),
		memberIDs:    session.GroupPageFlaggedMemberIDs.Get(s),
		start:        session.PaginationOffset.Get(s),
		page:         session.PaginationPage.Get(s),
		totalItems:   session.PaginationTotalItems.Get(s),
		totalPages:   session.PaginationTotalPages.Get(s),
		imageBuffer:  session.ImageBuffer.Get(s),
		isStreaming:  session.PaginationIsStreaming.Get(s),
		isReviewer:   s.BotSettings().IsReviewer(session.UserID.Get(s)),
		trainingMode: trainingMode,
		privacyMode:  trainingMode || session.UserStreamerMode.Get(s),
	}
}

// Build creates a Discord message with a grid of member avatars and information.
func (b *MembersBuilder) Build() *discord.MessageUpdateBuilder {
	// Create file attachment for the member avatars grid
	fileName := fmt.Sprintf("members_%d_%d.png", b.group.ID, b.page)

	// Build content
	var content strings.Builder
	content.WriteString("## Group Members\n")
	content.WriteString(fmt.Sprintf("```%s (%s)```\n",
		utils.CensorString(b.group.Name, b.privacyMode),
		utils.CensorString(strconv.FormatInt(b.group.ID, 10), b.privacyMode),
	))

	// Add members list
	for i, memberID := range b.memberIDs {
		// Add member name with status indicators
		content.WriteString(fmt.Sprintf("\n**%d.** %s", b.start+i+1, b.getMemberFieldName(memberID)))

		// Add member details
		details := b.getMemberFieldValue(memberID)
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

	// Add member reason section if exists
	if reason, ok := b.group.Reasons[enum.GroupReasonTypeMember]; ok {
		var reasonContent strings.Builder
		reasonContent.WriteString(shared.BuildSingleReasonDisplay(
			b.privacyMode,
			enum.GroupReasonTypeMember,
			reason,
			200,
			strconv.FormatInt(b.group.ID, 10),
			b.group.Name))

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

// getMemberFieldName creates the field name for a member entry.
func (b *MembersBuilder) getMemberFieldName(memberID int64) string {
	var indicators []string

	// Add presence indicator
	if presence, ok := b.presences[memberID]; ok {
		switch presence.UserPresenceType {
		case apiTypes.Website:
			indicators = append(indicators, "🌐")
		case apiTypes.InGame:
			indicators = append(indicators, "🎮")
		case apiTypes.InStudio:
			indicators = append(indicators, "🔨")
		case apiTypes.Offline:
			indicators = append(indicators, "💤")
		}
	}

	// Get member info
	member, ok := b.members[memberID]
	if !ok {
		return fmt.Sprintf("%d ❌", memberID)
	}

	// Add status indicator based on member status
	switch member.Status {
	case enum.UserTypeConfirmed:
		indicators = append(indicators, "⚠️")
	case enum.UserTypeFlagged:
		indicators = append(indicators, "⏳")
	case enum.UserTypeCleared:
		indicators = append(indicators, "✅")
	case enum.UserTypeQueued, enum.UserTypeBloxDB, enum.UserTypeMixed, enum.UserTypePastOffender:
		// No status indicator for these types
	}

	// Add banned status if applicable
	if member.IsBanned {
		indicators = append(indicators, "🔨")
	}

	// Add member name (with link in standard mode)
	name := utils.CensorString(member.Name, b.privacyMode)
	if !b.privacyMode {
		name = fmt.Sprintf("[%s](https://www.roblox.com/users/%d/profile)", name, member.ID)
	}

	if len(indicators) > 0 {
		return fmt.Sprintf("%s %s", name, strings.Join(indicators, " "))
	}

	return name
}

// getMemberFieldValue creates the field value for a member entry.
func (b *MembersBuilder) getMemberFieldValue(memberID int64) string {
	var info []string

	// Add presence details if available
	if presence, ok := b.presences[memberID]; ok {
		if presence.UserPresenceType != apiTypes.Offline {
			info = append(info, presence.LastLocation)
		} else if presence.LastOnline != nil {
			info = append(info, fmt.Sprintf("Last Online: <t:%d:R>", presence.LastOnline.Unix()))
		}
	}

	// Add confidence and reason if available
	if member, ok := b.members[memberID]; ok && len(member.Reasons) > 0 {
		reasonTypes := member.Reasons.Types()
		info = append(info, fmt.Sprintf("(%.2f) [%s]", member.Confidence, strings.Join(reasonTypes, ", ")))
	}

	if len(info) == 0 {
		return "No data available"
	}

	return strings.Join(info, " • ")
}
