package group

import (
	"bytes"
	"context"
	"strconv"

	"github.com/disgoorg/disgo/discord"
	"github.com/jaxron/roapi.go/pkg/api/resources/thumbnails"
	apiTypes "github.com/jaxron/roapi.go/pkg/api/types"
	builder "github.com/robalyx/rotector/internal/bot/builder/review/group"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/interaction"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/common/storage/database/types"
	"go.uber.org/zap"
)

// MembersMenu handles the display and interaction logic for viewing a group's flagged members.
type MembersMenu struct {
	layout *Layout
	page   *interaction.Page
}

// NewMembersMenu creates a new members menu.
func NewMembersMenu(layout *Layout) *MembersMenu {
	m := &MembersMenu{layout: layout}
	m.page = &interaction.Page{
		Name: constants.GroupMembersPageName,
		Message: func(s *session.Session) *discord.MessageUpdateBuilder {
			return builder.NewMembersBuilder(s).Build()
		},
		ShowHandlerFunc:   m.Show,
		ButtonHandlerFunc: m.handlePageNavigation,
	}
	return m
}

// Show prepares and displays the members interface for a specific page.
func (m *MembersMenu) Show(ctx *interaction.Context, s *session.Session) {
	group := session.GroupTarget.Get(s)

	// Get flagged users from tracking
	memberIDs, err := m.layout.db.Model().Tracking().GetFlaggedUsers(ctx.Context(), group.ID)
	if err != nil {
		m.layout.logger.Error("Failed to fetch flagged users", zap.Error(err))
		ctx.Error("Failed to load flagged users. Please try again.")
		return
	}

	// Return to review menu if group has no flagged members
	if len(memberIDs) == 0 {
		ctx.Error("No flagged members found for this group.")
		return
	}

	// Calculate page boundaries
	page := session.PaginationPage.Get(s)

	start := page * constants.MembersPerPage
	end := min(start+constants.MembersPerPage, len(memberIDs))
	totalPages := max((len(memberIDs)-1)/constants.MembersPerPage, 0)
	pageMembers := memberIDs[start:end]

	// Get user data from database only for the current page
	members, err := m.layout.db.Model().User().GetUsersByIDs(
		ctx.Context(),
		pageMembers,
		types.UserFieldBasic|types.UserFieldReasons|types.UserFieldConfidence,
	)
	if err != nil {
		m.layout.logger.Error("Failed to get user data", zap.Error(err))
		ctx.Error("Failed to fetch member data. Please try again.")
		return
	}

	// Start fetching presences for visible members in background
	presenceChan := m.layout.presenceFetcher.FetchPresencesConcurrently(ctx.Context(), pageMembers)

	// Store data in session
	session.GroupPageFlaggedMembers.Set(s, members)
	session.GroupPageFlaggedMemberIDs.Set(s, pageMembers)
	session.PaginationOffset.Set(s, start)
	session.PaginationTotalItems.Set(s, len(memberIDs))
	session.PaginationTotalPages.Set(s, totalPages)

	// Start streaming images
	m.layout.imageStreamer.Stream(interaction.StreamRequest{
		Event:    ctx.Event(),
		Session:  s,
		Page:     m.page,
		URLFunc:  func() []string { return m.fetchMemberThumbnails(ctx.Context(), pageMembers) },
		Columns:  constants.MembersGridColumns,
		Rows:     constants.MembersGridRows,
		MaxItems: constants.MembersPerPage,
		OnSuccess: func(buf *bytes.Buffer) {
			session.ImageBuffer.Set(s, buf)
		},
	})

	// Store presences when they arrive
	presenceMap := <-presenceChan
	session.UserPresences.Set(s, presenceMap)
}

// handlePageNavigation processes navigation button clicks.
func (m *MembersMenu) handlePageNavigation(ctx *interaction.Context, s *session.Session, customID string) {
	action := session.ViewerAction(customID)
	switch action {
	case session.ViewerFirstPage, session.ViewerPrevPage, session.ViewerNextPage, session.ViewerLastPage:
		totalPages := session.PaginationTotalPages.Get(s)
		page := action.ParsePageAction(s, totalPages)

		session.PaginationPage.Set(s, page)
		ctx.Reload("")
		return
	}

	switch customID {
	case constants.BackButtonCustomID:
		ctx.NavigateBack("")

	default:
		m.layout.logger.Warn("Invalid members viewer action", zap.String("action", string(action)))
		ctx.Error("Invalid interaction.")
	}
}

// fetchMemberThumbnails fetches thumbnails for a slice of member IDs.
func (m *MembersMenu) fetchMemberThumbnails(ctx context.Context, members []uint64) []string {
	// Create batch request for member avatars
	requests := thumbnails.NewBatchThumbnailsBuilder()
	for _, memberID := range members {
		requests.AddRequest(apiTypes.ThumbnailRequest{
			Type:      apiTypes.AvatarHeadShotType,
			TargetID:  memberID,
			RequestID: strconv.FormatUint(memberID, 10),
			Size:      apiTypes.Size150x150,
			Format:    apiTypes.WEBP,
		})
	}

	// Process thumbnails
	thumbnailMap := m.layout.thumbnailFetcher.ProcessBatchThumbnails(ctx, requests)

	// Convert map to ordered slice of URLs
	thumbnailURLs := make([]string, len(members))
	for i, memberID := range members {
		if url, ok := thumbnailMap[memberID]; ok {
			thumbnailURLs[i] = url
		}
	}

	return thumbnailURLs
}
