package user

import (
	"bytes"
	"strconv"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/jaxron/roapi.go/pkg/api/resources/thumbnails"
	apiTypes "github.com/jaxron/roapi.go/pkg/api/types"
	builder "github.com/robalyx/rotector/internal/bot/builder/review/user"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/pagination"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/bot/utils"
	"github.com/robalyx/rotector/internal/common/storage/database/types"
	"go.uber.org/zap"
)

// GroupsMenu handles the display and interaction logic for viewing a user's groups.
type GroupsMenu struct {
	layout *Layout
	page   *pagination.Page
}

// NewGroupsMenu creates a GroupsMenu and sets up its page with message builders
// and interaction handlers. The page is configured to show group information
// and handle navigation.
func NewGroupsMenu(layout *Layout) *GroupsMenu {
	m := &GroupsMenu{layout: layout}
	m.page = &pagination.Page{
		Name: "Groups Menu",
		Message: func(s *session.Session) *discord.MessageUpdateBuilder {
			return builder.NewGroupsBuilder(s).Build()
		},
		ButtonHandlerFunc: m.handlePageNavigation,
	}
	return m
}

// Show prepares and displays the groups interface for a specific page.
func (m *GroupsMenu) Show(event *events.ComponentInteractionCreate, s *session.Session, page int) {
	var user *types.ReviewUser
	s.GetInterface(constants.SessionKeyTarget, &user)

	// Return to review menu if user has no groups
	if len(user.Groups) == 0 {
		m.layout.reviewMenu.Show(event, s, "No groups found for this user.")
		return
	}

	// Get group types from session and sort groups by status
	var flaggedGroups map[uint64]*types.ReviewGroup
	s.GetInterface(constants.SessionKeyFlaggedGroups, &flaggedGroups)
	sortedGroups := m.sortGroupsByStatus(user.Groups, flaggedGroups)

	// Calculate page boundaries
	start := page * constants.GroupsPerPage
	end := start + constants.GroupsPerPage
	if end > len(sortedGroups) {
		end = len(sortedGroups)
	}
	pageGroups := sortedGroups[start:end]

	// Store data in session for the message builder
	s.Set(constants.SessionKeyGroups, pageGroups)
	s.Set(constants.SessionKeyStart, start)
	s.Set(constants.SessionKeyPaginationPage, page)
	s.Set(constants.SessionKeyTotalItems, len(sortedGroups))

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
			s.SetBuffer(constants.SessionKeyImageBuffer, buf)
		},
	})
}

// sortGroupsByStatus sorts groups into categories based on their status.
func (m *GroupsMenu) sortGroupsByStatus(groups []*apiTypes.UserGroupRoles, flaggedGroups map[uint64]*types.ReviewGroup) []*apiTypes.UserGroupRoles {
	// Group groups by their status
	groupedGroups := make(map[types.GroupType][]*apiTypes.UserGroupRoles)
	for _, group := range groups {
		status := types.GroupTypeUnflagged
		if reviewGroup, exists := flaggedGroups[group.Group.ID]; exists {
			status = reviewGroup.Status
		}
		groupedGroups[status] = append(groupedGroups[status], group)
	}

	// Define status priority order
	statusOrder := []types.GroupType{
		types.GroupTypeConfirmed,
		types.GroupTypeFlagged,
		types.GroupTypeLocked,
		types.GroupTypeCleared,
		types.GroupTypeUnflagged,
	}

	// Combine groups in priority order
	sortedGroups := make([]*apiTypes.UserGroupRoles, 0, len(groups))
	for _, status := range statusOrder {
		sortedGroups = append(sortedGroups, groupedGroups[status]...)
	}

	return sortedGroups
}

// handlePageNavigation processes navigation button clicks by calculating
// the target page number and refreshing the display.
func (m *GroupsMenu) handlePageNavigation(event *events.ComponentInteractionCreate, s *session.Session, customID string) {
	action := utils.ViewerAction(customID)
	switch action {
	case utils.ViewerFirstPage, utils.ViewerPrevPage, utils.ViewerNextPage, utils.ViewerLastPage:
		var user *types.ReviewUser
		s.GetInterface(constants.SessionKeyTarget, &user)

		// Calculate max page and validate navigation action
		maxPage := (len(user.Groups) - 1) / constants.GroupsPerPage
		page := action.ParsePageAction(s, action, maxPage)

		m.Show(event, s, page)

	case constants.BackButtonCustomID:
		m.layout.paginationManager.NavigateBack(event, s, "")

	default:
		m.layout.logger.Warn("Invalid groups viewer action", zap.String("action", string(action)))
		m.layout.paginationManager.RespondWithError(event, "Invalid interaction.")
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
	thumbnailMap := m.layout.thumbnailFetcher.ProcessBatchThumbnails(requests)

	// Convert map to ordered slice of URLs
	thumbnailURLs := make([]string, len(groups))
	for i, group := range groups {
		if url, ok := thumbnailMap[group.Group.ID]; ok {
			thumbnailURLs[i] = url
		}
	}

	return thumbnailURLs
}
