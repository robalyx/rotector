package user

import (
	"bytes"
	"context"
	"strconv"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/jaxron/roapi.go/pkg/api/resources/thumbnails"
	apiTypes "github.com/jaxron/roapi.go/pkg/api/types"
	builder "github.com/robalyx/rotector/internal/bot/builder/review/user"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/pagination"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/bot/interfaces"
	"github.com/robalyx/rotector/internal/common/storage/database/types"
	"github.com/robalyx/rotector/internal/common/storage/database/types/enum"
	"go.uber.org/zap"
)

// GroupsMenu handles the display and interaction logic for viewing a user's groups.
type GroupsMenu struct {
	layout *Layout
	page   *pagination.Page
}

// NewGroupsMenu creates a new groups menu.
func NewGroupsMenu(layout *Layout) *GroupsMenu {
	m := &GroupsMenu{layout: layout}
	m.page = &pagination.Page{
		Name: constants.UserGroupsPageName,
		Message: func(s *session.Session) *discord.MessageUpdateBuilder {
			return builder.NewGroupsBuilder(s).Build()
		},
		ShowHandlerFunc:   m.Show,
		ButtonHandlerFunc: m.handlePageNavigation,
	}
	return m
}

// Show prepares and displays the groups interface for a specific page.
func (m *GroupsMenu) Show(event interfaces.CommonEvent, s *session.Session, r *pagination.Respond) {
	user := session.UserTarget.Get(s)

	// Return to review menu if user has no groups
	if len(user.Groups) == 0 {
		r.Cancel(event, s, "No groups found for this user.")
		return
	}

	// Get group types from session and sort groups by status
	flaggedGroups := session.UserFlaggedGroups.Get(s)
	sortedGroups := m.sortGroupsByStatus(user.Groups, flaggedGroups)

	// Calculate page boundaries
	page := session.PaginationPage.Get(s)

	start := page * constants.GroupsPerPage
	end := min(start+constants.GroupsPerPage, len(sortedGroups))
	pageGroups := sortedGroups[start:end]

	// Store data in session for the message builder
	session.UserGroups.Set(s, pageGroups)
	session.PaginationOffset.Set(s, start)
	session.PaginationPage.Set(s, page)
	session.PaginationTotalItems.Set(s, len(sortedGroups))

	// Start streaming images
	m.layout.imageStreamer.Stream(pagination.StreamRequest{
		Event:    event,
		Session:  s,
		Page:     m.page,
		URLFunc:  func() []string { return m.fetchGroupThumbnails(pageGroups) },
		Columns:  constants.GroupsGridColumns,
		Rows:     constants.GroupsGridRows,
		MaxItems: constants.GroupsPerPage,
		OnSuccess: func(buf *bytes.Buffer) {
			session.ImageBuffer.Set(s, buf)
		},
	})
}

// sortGroupsByStatus sorts groups into categories based on their status.
func (m *GroupsMenu) sortGroupsByStatus(
	groups []*apiTypes.UserGroupRoles, flaggedGroups map[uint64]*types.ReviewGroup,
) []*apiTypes.UserGroupRoles {
	// Group groups by their status
	groupedGroups := make(map[enum.GroupType][]*apiTypes.UserGroupRoles)
	var unflaggedGroups []*apiTypes.UserGroupRoles

	// Separate flagged and unflagged groups
	for _, group := range groups {
		if reviewGroup, exists := flaggedGroups[group.Group.ID]; exists {
			groupedGroups[reviewGroup.Status] = append(groupedGroups[reviewGroup.Status], group)
		} else {
			unflaggedGroups = append(unflaggedGroups, group)
		}
	}

	// Define status priority order
	statusOrder := []enum.GroupType{
		enum.GroupTypeConfirmed,
		enum.GroupTypeFlagged,
		enum.GroupTypeCleared,
	}

	// Combine groups in priority order
	sortedGroups := make([]*apiTypes.UserGroupRoles, 0, len(groups))
	for _, status := range statusOrder {
		sortedGroups = append(sortedGroups, groupedGroups[status]...)
	}

	// Append unflagged groups last
	sortedGroups = append(sortedGroups, unflaggedGroups...)

	return sortedGroups
}

// handlePageNavigation processes navigation button clicks.
func (m *GroupsMenu) handlePageNavigation(
	event *events.ComponentInteractionCreate, s *session.Session, r *pagination.Respond, customID string,
) {
	action := session.ViewerAction(customID)
	switch action {
	case session.ViewerFirstPage, session.ViewerPrevPage, session.ViewerNextPage, session.ViewerLastPage:
		user := session.UserTarget.Get(s)

		// Calculate max page and validate navigation action
		maxPage := (len(user.Groups) - 1) / constants.GroupsPerPage
		page := action.ParsePageAction(s, maxPage)

		session.PaginationPage.Set(s, page)
		r.Reload(event, s, "")
		return
	}

	switch customID {
	case constants.BackButtonCustomID:
		r.NavigateBack(event, s, "")
	default:
		m.layout.logger.Warn("Invalid groups viewer action", zap.String("action", string(action)))
		r.Error(event, "Invalid interaction.")
	}
}

// fetchGroupThumbnails gets the thumbnail URLs for a list of groups.
func (m *GroupsMenu) fetchGroupThumbnails(groups []*apiTypes.UserGroupRoles) []string {
	// Create batch request for group icons
	requests := thumbnails.NewBatchThumbnailsBuilder()
	for _, group := range groups {
		requests.AddRequest(apiTypes.ThumbnailRequest{
			Type:      apiTypes.GroupIconType,
			TargetID:  group.Group.ID,
			RequestID: strconv.FormatUint(group.Group.ID, 10),
			Size:      apiTypes.Size150x150,
			Format:    apiTypes.WEBP,
		})
	}

	// Process thumbnails
	thumbnailMap := m.layout.thumbnailFetcher.ProcessBatchThumbnails(context.Background(), requests)

	// Convert map to ordered slice of URLs
	thumbnailURLs := make([]string, len(groups))
	for i, group := range groups {
		if url, ok := thumbnailMap[group.Group.ID]; ok {
			thumbnailURLs[i] = url
		}
	}

	return thumbnailURLs
}
