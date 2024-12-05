package user

import (
	"strconv"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/jaxron/roapi.go/pkg/api/resources/thumbnails"
	apiTypes "github.com/jaxron/roapi.go/pkg/api/types"
	builder "github.com/rotector/rotector/internal/bot/builder/review/user"
	"github.com/rotector/rotector/internal/bot/constants"
	"github.com/rotector/rotector/internal/bot/core/pagination"
	"github.com/rotector/rotector/internal/bot/core/session"
	"github.com/rotector/rotector/internal/bot/utils"
	"github.com/rotector/rotector/internal/common/storage/database/types"
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
	var user *types.FlaggedUser
	s.GetInterface(constants.SessionKeyTarget, &user)

	// Return to review menu if user has no groups
	if len(user.Groups) == 0 {
		m.layout.reviewMenu.Show(event, s, "No groups found for this user.")
		return
	}

	// Get group types from session and sort groups by status
	var groupTypes map[uint64]types.GroupType
	s.GetInterface(constants.SessionKeyGroupTypes, &groupTypes)
	sortedGroups := m.sortGroupsByStatus(user.Groups, groupTypes)

	// Calculate page boundaries
	start := page * constants.GroupsPerPage
	end := start + constants.GroupsPerPage
	if end > len(sortedGroups) {
		end = len(sortedGroups)
	}
	pageGroups := sortedGroups[start:end]

	// Download and process group icons
	thumbnailMap := m.fetchGroupsThumbnails(sortedGroups)

	// Extract URLs for current page
	pageThumbnailURLs := make([]string, len(pageGroups))
	for i, group := range pageGroups {
		if url, ok := thumbnailMap[group.Group.ID]; ok {
			pageThumbnailURLs[i] = url
		}
	}

	// Create grid image from icons
	buf, err := utils.MergeImages(
		m.layout.roAPI.GetClient(),
		pageThumbnailURLs,
		constants.GroupsGridColumns,
		constants.GroupsGridRows,
		constants.GroupsPerPage,
	)
	if err != nil {
		m.layout.logger.Error("Failed to merge group images", zap.Error(err))
		m.layout.paginationManager.RespondWithError(event, "Failed to process group images. Please try again.")
		return
	}

	// Store data in session for the message builder
	s.Set(constants.SessionKeyGroups, pageGroups)
	s.Set(constants.SessionKeyStart, start)
	s.Set(constants.SessionKeyPaginationPage, page)
	s.Set(constants.SessionKeyTotalItems, len(sortedGroups))
	s.SetBuffer(constants.SessionKeyImageBuffer, buf)

	m.layout.paginationManager.NavigateTo(event, s, m.page, "")
}

// sortGroupsByStatus sorts groups into three categories based on their status:
// 1. Confirmed groups (⚠️) - Groups that have been reviewed and confirmed
// 2. Flagged groups (⏳) - Groups that are currently flagged for review
// 3. Unflagged groups - Groups with no current flags or status
// Returns a new slice with groups sorted in this priority order.
func (m *GroupsMenu) sortGroupsByStatus(groups []*apiTypes.UserGroupRoles, groupTypes map[uint64]types.GroupType) []*apiTypes.UserGroupRoles {
	// Create three slices for different status types
	var confirmedGroups, flaggedGroups, unflaggedGroups []*apiTypes.UserGroupRoles

	// Categorize groups based on their status
	for _, group := range groups {
		switch groupTypes[group.Group.ID] {
		case types.GroupTypeConfirmed:
			confirmedGroups = append(confirmedGroups, group)
		case types.GroupTypeFlagged:
			flaggedGroups = append(flaggedGroups, group)
		default:
			unflaggedGroups = append(unflaggedGroups, group)
		} //exhaustive:ignore
	}

	// Combine slices in priority order (confirmed -> flagged -> unflagged)
	sortedGroups := make([]*apiTypes.UserGroupRoles, 0, len(groups))
	sortedGroups = append(sortedGroups, confirmedGroups...)
	sortedGroups = append(sortedGroups, flaggedGroups...)
	sortedGroups = append(sortedGroups, unflaggedGroups...)

	return sortedGroups
}

// handlePageNavigation processes navigation button clicks by calculating
// the target page number and refreshing the display.
func (m *GroupsMenu) handlePageNavigation(event *events.ComponentInteractionCreate, s *session.Session, customID string) {
	action := utils.ViewerAction(customID)
	switch action {
	case utils.ViewerFirstPage, utils.ViewerPrevPage, utils.ViewerNextPage, utils.ViewerLastPage:
		var user *types.FlaggedUser
		s.GetInterface(constants.SessionKeyTarget, &user)

		// Calculate max page and validate navigation action
		maxPage := (len(user.Groups) - 1) / constants.GroupsPerPage
		page, ok := action.ParsePageAction(s, action, maxPage)
		if !ok {
			m.layout.paginationManager.RespondWithError(event, "Invalid interaction.")
			return
		}

		m.Show(event, s, page)

	case constants.BackButtonCustomID:
		m.layout.reviewMenu.Show(event, s, "")

	default:
		m.layout.logger.Warn("Invalid groups viewer action", zap.String("action", string(action)))
		m.layout.paginationManager.RespondWithError(event, "Invalid interaction.")
	}
}

// fetchGroupsThumbnails downloads icon images for all groups.
// Returns a map of group IDs to their icon URLs.
func (m *GroupsMenu) fetchGroupsThumbnails(groups []*apiTypes.UserGroupRoles) map[uint64]string {
	// Create batch request for all group icons
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

	return m.layout.thumbnailFetcher.ProcessBatchThumbnails(requests)
}
