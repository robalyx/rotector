package user

import (
	"bytes"
	"context"
	"strconv"

	"github.com/disgoorg/disgo/discord"
	"github.com/jaxron/roapi.go/pkg/api/resources/thumbnails"
	apiTypes "github.com/jaxron/roapi.go/pkg/api/types"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/interaction"
	"github.com/robalyx/rotector/internal/bot/core/session"
	view "github.com/robalyx/rotector/internal/bot/views/review/user"
	"github.com/robalyx/rotector/internal/database/types"
	"github.com/robalyx/rotector/internal/database/types/enum"
	"go.uber.org/zap"
)

// GroupsMenu handles the display and interaction logic for viewing a user's groups.
type GroupsMenu struct {
	layout *Layout
	page   *interaction.Page
}

// NewGroupsMenu creates a new groups menu.
func NewGroupsMenu(layout *Layout) *GroupsMenu {
	m := &GroupsMenu{layout: layout}
	m.page = &interaction.Page{
		Name: constants.UserGroupsPageName,
		Message: func(s *session.Session) *discord.MessageUpdateBuilder {
			return view.NewGroupsBuilder(s).Build()
		},
		ShowHandlerFunc:   m.Show,
		ButtonHandlerFunc: m.handlePageNavigation,
	}
	return m
}

// Show prepares and displays the groups interface for a specific page.
func (m *GroupsMenu) Show(ctx *interaction.Context, s *session.Session) {
	user := session.UserTarget.Get(s)

	// Return to review menu if user has no groups
	if len(user.Groups) == 0 {
		ctx.Cancel("No groups found for this user.")
		return
	}

	// Get group types from session and sort groups by status
	flaggedGroups := session.UserFlaggedGroups.Get(s)
	sortedGroups := m.sortGroupsByStatus(user.Groups, flaggedGroups)

	// Calculate page boundaries
	page := session.PaginationPage.Get(s)
	totalPages := max((len(sortedGroups)-1)/constants.GroupsPerPage, 0)

	start := page * constants.GroupsPerPage
	end := min(start+constants.GroupsPerPage, len(sortedGroups))
	pageGroups := sortedGroups[start:end]

	// Store data in session for the message builder
	session.UserGroups.Set(s, pageGroups)
	session.PaginationOffset.Set(s, start)
	session.PaginationTotalItems.Set(s, len(sortedGroups))
	session.PaginationTotalPages.Set(s, totalPages)

	// Start streaming images
	m.layout.imageStreamer.Stream(interaction.StreamRequest{
		Event:    ctx.Event(),
		Session:  s,
		Page:     m.page,
		URLFunc:  func() []string { return m.fetchGroupThumbnails(ctx.Context(), pageGroups) },
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
func (m *GroupsMenu) handlePageNavigation(ctx *interaction.Context, s *session.Session, customID string) {
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
		m.layout.logger.Warn("Invalid groups viewer action", zap.String("action", string(action)))
		ctx.Error("Invalid interaction.")
	}
}

// fetchGroupThumbnails gets the thumbnail URLs for a list of groups.
func (m *GroupsMenu) fetchGroupThumbnails(ctx context.Context, groups []*apiTypes.UserGroupRoles) []string {
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
	thumbnailMap := m.layout.thumbnailFetcher.ProcessBatchThumbnails(ctx, requests)

	// Convert map to ordered slice of URLs
	thumbnailURLs := make([]string, len(groups))
	for i, group := range groups {
		if url, ok := thumbnailMap[group.Group.ID]; ok {
			thumbnailURLs[i] = url
		}
	}

	return thumbnailURLs
}
