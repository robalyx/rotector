package user

import (
	"context"
	"strconv"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/jaxron/roapi.go/pkg/api/resources/thumbnails"
	"github.com/jaxron/roapi.go/pkg/api/types"
	builder "github.com/rotector/rotector/internal/bot/builder/review/user"
	"github.com/rotector/rotector/internal/bot/constants"
	"github.com/rotector/rotector/internal/bot/core/pagination"
	"github.com/rotector/rotector/internal/bot/core/session"
	"github.com/rotector/rotector/internal/bot/utils"
	"github.com/rotector/rotector/internal/common/storage/database/models"
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
// It loads group data, checks their status, and creates a grid of icons.
func (m *GroupsMenu) Show(event *events.ComponentInteractionCreate, s *session.Session, page int) {
	var user *models.FlaggedUser
	s.GetInterface(constants.SessionKeyTarget, &user)

	// Return to review menu if user has no groups
	if len(user.Groups) == 0 {
		m.layout.reviewMenu.Show(event, s, "No groups found for this user.")
		return
	}
	groups := user.Groups

	// Calculate page boundaries and get subset of groups
	start := page * constants.GroupsPerPage
	end := start + constants.GroupsPerPage
	if end > len(groups) {
		end = len(groups)
	}
	pageGroups := groups[start:end]

	// Check database for flagged groups
	flaggedGroups, err := m.getFlaggedGroups(pageGroups)
	if err != nil {
		m.layout.logger.Error("Failed to get flagged groups", zap.Error(err))
		m.layout.paginationManager.RespondWithError(event, "Failed to get flagged groups. Please try again.")
		return
	}

	// Download and process group icons
	thumbnailMap := m.fetchGroupsThumbnails(groups)

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
	s.Set(constants.SessionKeyFlaggedGroups, flaggedGroups)
	s.Set(constants.SessionKeyStart, start)
	s.Set(constants.SessionKeyPaginationPage, page)
	s.Set(constants.SessionKeyTotalItems, len(groups))
	s.SetBuffer(constants.SessionKeyImageBuffer, buf)

	m.layout.paginationManager.NavigateTo(event, s, m.page, "")
}

// getFlaggedGroups checks the database to find which groups are flagged.
// Returns a map of group IDs to their flagged status.
func (m *GroupsMenu) getFlaggedGroups(groups []*types.UserGroupRoles) (map[uint64]bool, error) {
	groupIDs := make([]uint64, len(groups))
	for i, group := range groups {
		groupIDs[i] = group.Group.ID
	}

	flaggedGroups, err := m.layout.db.Groups().CheckConfirmedGroups(context.Background(), groupIDs)
	if err != nil {
		return nil, err
	}

	flaggedGroupsMap := make(map[uint64]bool)
	for _, id := range flaggedGroups {
		flaggedGroupsMap[id] = true
	}

	return flaggedGroupsMap, nil
}

// handlePageNavigation processes navigation button clicks by calculating
// the target page number and refreshing the display.
func (m *GroupsMenu) handlePageNavigation(event *events.ComponentInteractionCreate, s *session.Session, customID string) {
	action := utils.ViewerAction(customID)
	switch action {
	case utils.ViewerFirstPage, utils.ViewerPrevPage, utils.ViewerNextPage, utils.ViewerLastPage:
		var user *models.FlaggedUser
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
func (m *GroupsMenu) fetchGroupsThumbnails(groups []*types.UserGroupRoles) map[uint64]string {
	// Create batch request for all group icons
	requests := thumbnails.NewBatchThumbnailsBuilder()
	for _, group := range groups {
		requests.AddRequest(types.ThumbnailRequest{
			Type:      types.GroupIconType,
			TargetID:  group.Group.ID,
			RequestID: strconv.FormatUint(group.Group.ID, 10),
			Size:      types.Size150x150,
			Format:    types.WEBP,
		})
	}

	return m.layout.thumbnailFetcher.ProcessBatchThumbnails(requests)
}
