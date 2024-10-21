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
	"github.com/rotector/rotector/internal/common/database"
	"github.com/rotector/rotector/internal/common/utils"
	"go.uber.org/zap"
)

// GroupsMenu handles the groups viewer functionality.
type GroupsMenu struct {
	handler *Handler
	page    *pagination.Page
}

// NewGroupsMenu creates a new GroupsMenu instance.
func NewGroupsMenu(h *Handler) *GroupsMenu {
	g := GroupsMenu{handler: h}
	g.page = &pagination.Page{
		Name: "Groups Menu",
		Data: make(map[string]interface{}),
		Message: func(data map[string]interface{}) *discord.MessageUpdateBuilder {
			user := data["user"].(*database.PendingUser)
			groups := data["groups"].([]types.UserGroupRoles)
			flaggedGroups := data["flaggedGroups"].(map[uint64]bool)
			start := data["start"].(int)
			page := data["page"].(int)
			total := data["total"].(int)
			file := data["file"].(*discord.File)
			fileName := data["fileName"].(string)

			return builders.NewGroupsEmbed(user, groups, flaggedGroups, start, page, total, file, fileName).Build()
		},
		ButtonHandlerFunc: func(event *events.ComponentInteractionCreate, s *session.Session, option string) {
			switch option {
			case string(builders.ViewerFirstPage), string(builders.ViewerPrevPage), string(builders.ViewerNextPage), string(builders.ViewerLastPage):
				g.handlePageNavigation(event, s, builders.ViewerAction(option))
			case string(builders.ViewerBackToReview):
				h.reviewMenu.ShowReviewMenu(event, s, "")
			}
		},
	}

	return &g
}

// ShowGroupsMenu shows the groups menu for the given page.
func (g *GroupsMenu) ShowGroupsMenu(event *events.ComponentInteractionCreate, s *session.Session, page int) {
	user := s.GetPendingUser(session.KeyTarget)

	// Check if the user has groups
	if len(user.Groups) == 0 {
		g.handler.reviewMenu.ShowReviewMenu(event, s, "No groups found for this user.")
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
	groupIDs := make([]uint64, len(pageGroups))
	for i, group := range pageGroups {
		groupIDs[i] = group.Group.ID
	}

	flaggedGroups, err := g.handler.db.Groups().CheckFlaggedGroups(groupIDs)
	if err != nil {
		g.handler.logger.Error("Failed to check flagged groups", zap.Error(err))
		g.handler.respondWithError(event, "Failed to check flagged groups. Please try again.")
		return
	}

	flaggedGroupsMap := make(map[uint64]bool)
	for _, id := range flaggedGroups {
		flaggedGroupsMap[id] = true
	}

	// Fetch thumbnails for the page groups
	groupsThumbnailURLs, err := g.fetchGroupsThumbnails(pageGroups)
	if err != nil {
		g.handler.logger.Error("Failed to fetch groups thumbnails", zap.Error(err))
		g.handler.respondWithError(event, "Failed to fetch groups thumbnails. Please try again.")
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
	buf, err := utils.MergeImages(g.handler.roAPI.GetClient(), pageThumbnailURLs, constants.GroupsGridColumns, constants.GroupsGridRows, constants.GroupsPerPage)
	if err != nil {
		g.handler.logger.Error("Failed to merge group images", zap.Error(err))
		g.handler.respondWithError(event, "Failed to process group images. Please try again.")
		return
	}

	// Create file attachment
	fileName := fmt.Sprintf("groups_%d_%d.png", user.ID, page)
	file := discord.NewFile(fileName, "", bytes.NewReader(buf.Bytes()))

	// Calculate total pages
	total := (len(groups) + constants.GroupsPerPage - 1) / constants.GroupsPerPage

	// Set the data for the page
	g.page.Data["user"] = user
	g.page.Data["groups"] = pageGroups
	g.page.Data["flaggedGroups"] = flaggedGroupsMap
	g.page.Data["start"] = start
	g.page.Data["page"] = page
	g.page.Data["total"] = total
	g.page.Data["file"] = file
	g.page.Data["fileName"] = fileName

	// Navigate to the groups menu and update the message
	g.handler.paginationManager.NavigateTo(g.page.Name, s)
	g.handler.paginationManager.UpdateMessage(event, s, g.page, "")
}

// handlePageNavigation handles the page navigation for the groups menu.
func (g *GroupsMenu) handlePageNavigation(event *events.ComponentInteractionCreate, s *session.Session, action builders.ViewerAction) {
	switch action {
	case builders.ViewerFirstPage, builders.ViewerPrevPage, builders.ViewerNextPage, builders.ViewerLastPage:
		user := s.GetPendingUser(session.KeyTarget)

		// Get the page number for the action
		maxPage := (len(user.Groups) - 1) / constants.GroupsPerPage
		page, ok := action.ParsePageAction(s, action, maxPage)
		if !ok {
			g.handler.respondWithError(event, "Invalid interaction.")
			return
		}

		g.ShowGroupsMenu(event, s, page)
	case builders.ViewerBackToReview:
		g.handler.reviewMenu.ShowReviewMenu(event, s, "")
	default:
		g.handler.logger.Warn("Invalid groups viewer action", zap.String("action", string(action)))
		g.handler.respondWithError(event, "Invalid interaction.")
	}
}

// fetchGroupsThumbnails fetches thumbnails for the given groups.
func (g *GroupsMenu) fetchGroupsThumbnails(groups []types.UserGroupRoles) (map[uint64]string, error) {
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
	thumbnailResponses, err := g.handler.roAPI.Thumbnails().GetBatchThumbnails(context.Background(), requests.Build())
	if err != nil {
		g.handler.logger.Error("Error fetching batch thumbnails", zap.Error(err))
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

	g.handler.logger.Info("Fetched batch thumbnails",
		zap.Int("groups", len(groups)),
		zap.Int("fetchedThumbnails", len(thumbnailResponses)))

	return thumbnailURLs, nil
}
