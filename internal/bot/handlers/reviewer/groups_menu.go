package reviewer

import (
	"bytes"
	"context"
	"fmt"
	"strconv"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/jaxron/roapi.go/pkg/api/resources/thumbnails"
	"github.com/jaxron/roapi.go/pkg/api/types"
	"github.com/rotector/rotector/internal/bot/constants"
	"github.com/rotector/rotector/internal/bot/handlers/reviewer/builders"
	"github.com/rotector/rotector/internal/bot/pagination"
	"github.com/rotector/rotector/internal/bot/session"
	"github.com/rotector/rotector/internal/bot/utils"
	"go.uber.org/zap"
)

// GroupsMenu handles the groups viewer functionality.
type GroupsMenu struct {
	handler *Handler
	page    *pagination.Page
}

// NewGroupsMenu creates a new GroupsMenu instance.
func NewGroupsMenu(h *Handler) *GroupsMenu {
	m := GroupsMenu{handler: h}
	m.page = &pagination.Page{
		Name: "Groups Menu",
		Message: func(s *session.Session) *discord.MessageUpdateBuilder {
			return builders.NewGroupsEmbed(s).Build()
		},
		ButtonHandlerFunc: m.handlePageNavigation,
	}
	return &m
}

// ShowGroupsMenu shows the groups menu for the given page.
func (m *GroupsMenu) ShowGroupsMenu(event *events.ComponentInteractionCreate, s *session.Session, page int) {
	user := s.GetFlaggedUser(constants.SessionKeyTarget)

	// Check if the user has groups
	if len(user.Groups) == 0 {
		m.handler.reviewMenu.ShowReviewMenu(event, s, "No groups found for this user.")
		return
	}
	groups := user.Groups

	// Get groups for the current page
	start := page * constants.GroupsPerPage
	end := start + constants.GroupsPerPage
	if end > len(groups) {
		end = len(groups)
	}
	pageGroups := groups[start:end]

	// Check which groups are flagged
	flaggedGroups, err := m.getFlaggedGroups(pageGroups)
	if err != nil {
		m.handler.logger.Error("Failed to get flagged groups", zap.Error(err))
		m.handler.paginationManager.RespondWithError(event, "Failed to get flagged groups. Please try again.")
		return
	}

	// Fetch thumbnails for the page groups
	groupsThumbnailURLs, err := m.fetchGroupsThumbnails(pageGroups)
	if err != nil {
		m.handler.logger.Error("Failed to fetch groups thumbnails", zap.Error(err))
		m.handler.paginationManager.RespondWithError(event, "Failed to fetch groups thumbnails. Please try again.")
		return
	}

	// Get thumbnail URLs for the page groups
	pageThumbnailURLs := make([]string, len(pageGroups))
	for i, group := range pageGroups {
		if url, ok := groupsThumbnailURLs[group.Group.ID]; ok {
			pageThumbnailURLs[i] = url
		}
	}

	// Download and merge group images
	buf, err := utils.MergeImages(m.handler.roAPI.GetClient(), pageThumbnailURLs, constants.GroupsGridColumns, constants.GroupsGridRows, constants.GroupsPerPage)
	if err != nil {
		m.handler.logger.Error("Failed to merge group images", zap.Error(err))
		m.handler.paginationManager.RespondWithError(event, "Failed to process group images. Please try again.")
		return
	}

	// Create file attachment
	fileName := fmt.Sprintf("groups_%d_%d.png", user.ID, page)
	file := discord.NewFile(fileName, "", bytes.NewReader(buf.Bytes()))

	// Get user settings
	settings, err := m.handler.db.Settings().GetUserSettings(uint64(event.User().ID))
	if err != nil {
		m.handler.logger.Error("Failed to get user settings", zap.Error(err))
		m.handler.paginationManager.RespondWithError(event, "Failed to get user settings. Please try again.")
		return
	}

	// Set the data for the page
	s.Set(constants.SessionKeyGroups, pageGroups)
	s.Set(constants.SessionKeyFlaggedGroups, flaggedGroups)
	s.Set(constants.SessionKeyStart, start)
	s.Set(constants.SessionKeyPaginationPage, page)
	s.Set(constants.SessionKeyTotalItems, len(groups))
	s.Set(constants.SessionKeyFile, file)
	s.Set(constants.SessionKeyFileName, fileName)
	s.Set(constants.SessionKeyStreamerMode, settings.StreamerMode)

	// Navigate to the groups menu and update the message
	m.handler.paginationManager.NavigateTo(m.page.Name, s)
	m.handler.paginationManager.UpdateMessage(event, s, m.page, "")
}

// getFlaggedGroups fetches the flagged groups for the given user.
func (m *GroupsMenu) getFlaggedGroups(groups []types.UserGroupRoles) (map[uint64]bool, error) {
	groupIDs := make([]uint64, len(groups))
	for i, group := range groups {
		groupIDs[i] = group.Group.ID
	}

	flaggedGroups, err := m.handler.db.Groups().CheckConfirmedGroups(groupIDs)
	if err != nil {
		return nil, err
	}

	flaggedGroupsMap := make(map[uint64]bool)
	for _, id := range flaggedGroups {
		flaggedGroupsMap[id] = true
	}

	return flaggedGroupsMap, nil
}

// handlePageNavigation handles the page navigation for the groups menu.
func (m *GroupsMenu) handlePageNavigation(event *events.ComponentInteractionCreate, s *session.Session, customID string) {
	action := utils.ViewerAction(customID)
	switch action {
	case utils.ViewerFirstPage, utils.ViewerPrevPage, utils.ViewerNextPage, utils.ViewerLastPage:
		user := s.GetFlaggedUser(constants.SessionKeyTarget)

		// Get the page number for the action
		maxPage := (len(user.Groups) - 1) / constants.GroupsPerPage
		page, ok := action.ParsePageAction(s, action, maxPage)
		if !ok {
			m.handler.paginationManager.RespondWithError(event, "Invalid interaction.")
			return
		}

		m.ShowGroupsMenu(event, s, page)
	case constants.BackButtonCustomID:
		m.handler.reviewMenu.ShowReviewMenu(event, s, "")
	default:
		m.handler.logger.Warn("Invalid groups viewer action", zap.String("action", string(action)))
		m.handler.paginationManager.RespondWithError(event, "Invalid interaction.")
	}
}

// fetchGroupsThumbnails fetches thumbnails for the given groups.
func (m *GroupsMenu) fetchGroupsThumbnails(groups []types.UserGroupRoles) (map[uint64]string, error) {
	thumbnailURLs := make(map[uint64]string)

	// Create thumbnail requests for each group
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

	// Fetch batch thumbnails
	thumbnailResponses, err := m.handler.roAPI.Thumbnails().GetBatchThumbnails(context.Background(), requests.Build())
	if err != nil {
		m.handler.logger.Error("Error fetching batch thumbnails", zap.Error(err))
		return thumbnailURLs, err
	}

	// Process thumbnail responses
	for _, response := range thumbnailResponses {
		if response.State == types.ThumbnailStateCompleted && response.ImageURL != nil {
			thumbnailURLs[response.TargetID] = *response.ImageURL
		} else {
			thumbnailURLs[response.TargetID] = "-"
		}
	}

	m.handler.logger.Info("Fetched batch thumbnails",
		zap.Int("groups", len(groups)),
		zap.Int("fetchedThumbnails", len(thumbnailResponses)))

	return thumbnailURLs, nil
}
