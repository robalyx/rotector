package group

import (
	"bytes"
	"context"
	"strconv"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/jaxron/roapi.go/pkg/api/resources/thumbnails"
	apiTypes "github.com/jaxron/roapi.go/pkg/api/types"
	builder "github.com/robalyx/rotector/internal/bot/builder/review/group"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/pagination"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/bot/interfaces"
	"github.com/robalyx/rotector/internal/common/storage/database/types"
	"github.com/robalyx/rotector/internal/common/storage/database/types/enum"
	"go.uber.org/zap"
)

// MembersMenu handles the display and interaction logic for viewing a group's flagged members.
type MembersMenu struct {
	layout *Layout
	page   *pagination.Page
}

// NewMembersMenu creates a new members menu.
func NewMembersMenu(layout *Layout) *MembersMenu {
	m := &MembersMenu{layout: layout}
	m.page = &pagination.Page{
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
func (m *MembersMenu) Show(event interfaces.CommonEvent, s *session.Session, r *pagination.Respond) {
	memberIDs := session.GroupMemberIDs.Get(s)

	// Return to review menu if group has no flagged members
	if len(memberIDs) == 0 {
		r.Cancel(event, s, "No flagged members found for this group.")
		return
	}

	// Get user data from database
	members, err := m.layout.db.Model().User().GetUsersByIDs(
		context.Background(),
		memberIDs,
		types.UserFieldBasic|types.UserFieldReasons|types.UserFieldConfidence,
	)
	if err != nil {
		m.layout.logger.Error("Failed to get user data", zap.Error(err))
		r.Error(event, "Failed to fetch member data. Please try again.")
		return
	}

	// Sort members by status
	sortedMemberIDs := m.sortMembersByStatus(memberIDs, members)

	// Calculate page boundaries
	page := session.PaginationPage.Get(s)

	start := page * constants.MembersPerPage
	end := min(start+constants.MembersPerPage, len(sortedMemberIDs))
	pageMembers := sortedMemberIDs[start:end]

	// Start fetching presences for visible members in background
	presenceChan := m.layout.presenceFetcher.FetchPresencesConcurrently(context.Background(), pageMembers)

	// Store initial data in session
	session.GroupMemberIDs.Set(s, sortedMemberIDs)
	session.GroupMembers.Set(s, members)
	session.GroupPageMembers.Set(s, pageMembers)
	session.PaginationOffset.Set(s, start)
	session.PaginationPage.Set(s, page+1)
	session.PaginationTotalItems.Set(s, len(sortedMemberIDs))

	// Start streaming images
	m.layout.imageStreamer.Stream(pagination.StreamRequest{
		Event:    event,
		Session:  s,
		Page:     m.page,
		URLFunc:  func() []string { return m.fetchMemberThumbnails(pageMembers) },
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
func (m *MembersMenu) handlePageNavigation(
	event *events.ComponentInteractionCreate, s *session.Session, r *pagination.Respond, customID string,
) {
	action := session.ViewerAction(customID)
	switch action {
	case session.ViewerFirstPage, session.ViewerPrevPage, session.ViewerNextPage, session.ViewerLastPage:
		memberIDs := session.GroupMemberIDs.Get(s)

		// Calculate max page and validate navigation action
		maxPage := (len(memberIDs) - 1) / constants.MembersPerPage
		page := action.ParsePageAction(s, action, maxPage)

		session.PaginationPage.Set(s, page)
		r.Reload(event, s, "")
		return
	}

	switch customID {
	case constants.BackButtonCustomID:
		r.NavigateBack(event, s, "")

	default:
		m.layout.logger.Warn("Invalid members viewer action", zap.String("action", string(action)))
		r.Error(event, "Invalid interaction.")
	}
}

// sortMembersByStatus sorts members by their status in priority order.
func (m *MembersMenu) sortMembersByStatus(memberIDs []uint64, flaggedUsers map[uint64]*types.ReviewUser) []uint64 {
	// Group members by status
	groupedMembers := make(map[enum.UserType][]uint64)
	var unflaggedMembers []uint64

	// Separate flagged and unflagged members
	for _, memberID := range memberIDs {
		if member, exists := flaggedUsers[memberID]; exists {
			groupedMembers[member.Status] = append(groupedMembers[member.Status], memberID)
		} else {
			unflaggedMembers = append(unflaggedMembers, memberID)
		}
	}

	// Define status priority order
	statusOrder := []enum.UserType{
		enum.UserTypeConfirmed,
		enum.UserTypeFlagged,
		enum.UserTypeCleared,
	}

	// Combine members in priority order
	sortedMembers := make([]uint64, 0, len(memberIDs))
	for _, status := range statusOrder {
		sortedMembers = append(sortedMembers, groupedMembers[status]...)
	}

	// Append unflagged members last
	sortedMembers = append(sortedMembers, unflaggedMembers...)

	return sortedMembers
}

// fetchMemberThumbnails fetches thumbnails for a slice of member IDs.
func (m *MembersMenu) fetchMemberThumbnails(members []uint64) []string {
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
	thumbnailMap := m.layout.thumbnailFetcher.ProcessBatchThumbnails(context.Background(), requests)

	// Convert map to ordered slice of URLs
	thumbnailURLs := make([]string, len(members))
	for i, memberID := range members {
		if url, ok := thumbnailMap[memberID]; ok {
			thumbnailURLs[i] = url
		}
	}

	return thumbnailURLs
}
