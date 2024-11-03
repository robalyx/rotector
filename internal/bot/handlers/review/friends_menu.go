package review

import (
	"context"
	"strconv"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/jaxron/roapi.go/pkg/api/resources/thumbnails"
	"github.com/jaxron/roapi.go/pkg/api/types"
	"github.com/rotector/rotector/internal/bot/constants"
	"github.com/rotector/rotector/internal/bot/handlers/review/builders"
	"github.com/rotector/rotector/internal/bot/pagination"
	"github.com/rotector/rotector/internal/bot/session"
	"github.com/rotector/rotector/internal/bot/utils"
	"github.com/rotector/rotector/internal/common/database"
	"go.uber.org/zap"
)

// FriendsMenu handles the display and interaction logic for viewing a user's friends.
// It works with the friends builder to create paginated views of friend avatars
// and manages friend status indicators.
type FriendsMenu struct {
	handler *Handler
	page    *pagination.Page
}

// NewFriendsMenu creates a FriendsMenu and sets up its page with message builders
// and interaction handlers. The page is configured to show friend information
// and handle navigation.
func NewFriendsMenu(h *Handler) *FriendsMenu {
	m := FriendsMenu{handler: h}
	m.page = &pagination.Page{
		Name: "Friends Menu",
		Message: func(s *session.Session) *discord.MessageUpdateBuilder {
			return builders.NewFriendsEmbed(s).Build()
		},
		ButtonHandlerFunc: m.handlePageNavigation,
	}
	return &m
}

// ShowFriendsMenu prepares and displays the friends interface for a specific page.
// It loads friend data, checks their status, and creates a grid of avatars.
func (m *FriendsMenu) ShowFriendsMenu(event *events.ComponentInteractionCreate, s *session.Session, page int) {
	var user *database.FlaggedUser
	s.GetInterface(constants.SessionKeyTarget, &user)

	// Return to review menu if user has no friends
	if len(user.Friends) == 0 {
		m.handler.reviewMenu.ShowReviewMenu(event, s, "No friends found for this user.")
		return
	}
	friends := user.Friends

	// Calculate page boundaries and get subset of friends
	start := page * constants.FriendsPerPage
	end := start + constants.FriendsPerPage
	if end > len(friends) {
		end = len(friends)
	}
	pageFriends := friends[start:end]

	// Check database for flagged/confirmed status
	flaggedFriends, err := m.getFlaggedFriends(pageFriends)
	if err != nil {
		m.handler.logger.Error("Failed to get flagged friends", zap.Error(err))
		m.handler.paginationManager.RespondWithError(event, "Failed to get flagged friends. Please try again.")
		return
	}

	// Download and process friend avatars
	friendsThumbnailURLs, err := m.fetchFriendsThumbnails(pageFriends)
	if err != nil {
		m.handler.logger.Error("Failed to fetch friends thumbnails", zap.Error(err))
		m.handler.paginationManager.RespondWithError(event, "Failed to fetch friends thumbnails. Please try again.")
		return
	}

	// Extract URLs in page order for grid creation
	pageThumbnailURLs := make([]string, len(pageFriends))
	for i, friend := range pageFriends {
		if url, ok := friendsThumbnailURLs[friend.ID]; ok {
			pageThumbnailURLs[i] = url
		}
	}

	// Create grid image from avatars
	buf, err := utils.MergeImages(
		m.handler.roAPI.GetClient(),
		pageThumbnailURLs,
		constants.FriendsGridColumns,
		constants.FriendsGridRows,
		constants.FriendsPerPage,
	)
	if err != nil {
		m.handler.logger.Error("Failed to merge friend images", zap.Error(err))
		m.handler.paginationManager.RespondWithError(event, "Failed to process friend images. Please try again.")
		return
	}

	// Store data in session for the message builder
	s.Set(constants.SessionKeyFriends, pageFriends)
	s.Set(constants.SessionKeyFlaggedFriends, flaggedFriends)
	s.Set(constants.SessionKeyStart, start)
	s.Set(constants.SessionKeyPaginationPage, page)
	s.Set(constants.SessionKeyTotalItems, len(friends))
	s.SetBuffer(constants.SessionKeyImageBuffer, buf)

	m.handler.paginationManager.NavigateTo(event, s, m.page, "")
}

// getFlaggedFriends checks the database to find which friends are flagged or confirmed.
// Returns a map of friend IDs to their status (flagged/confirmed).
func (m *FriendsMenu) getFlaggedFriends(friends []types.Friend) (map[uint64]string, error) {
	friendIDs := make([]uint64, len(friends))
	for i, friend := range friends {
		friendIDs[i] = friend.ID
	}

	flaggedFriends, err := m.handler.db.Users().CheckExistingUsers(friendIDs)
	if err != nil {
		return nil, err
	}

	return flaggedFriends, nil
}

// handlePageNavigation processes navigation button clicks by calculating
// the target page number and refreshing the display.
func (m *FriendsMenu) handlePageNavigation(event *events.ComponentInteractionCreate, s *session.Session, customID string) {
	action := utils.ViewerAction(customID)
	switch action {
	case utils.ViewerFirstPage, utils.ViewerPrevPage, utils.ViewerNextPage, utils.ViewerLastPage:
		var user *database.FlaggedUser
		s.GetInterface(constants.SessionKeyTarget, &user)

		// Calculate max page and validate navigation action
		maxPage := (len(user.Friends) - 1) / constants.FriendsPerPage
		page, ok := action.ParsePageAction(s, action, maxPage)
		if !ok {
			m.handler.paginationManager.RespondWithError(event, "Invalid interaction.")
			return
		}

		m.ShowFriendsMenu(event, s, page)

	case constants.BackButtonCustomID:
		m.handler.reviewMenu.ShowReviewMenu(event, s, "")

	default:
		m.handler.logger.Warn("Invalid friends viewer action", zap.String("action", string(action)))
		m.handler.paginationManager.RespondWithError(event, "Invalid interaction.")
	}
}

// fetchFriendsThumbnails downloads avatar images for a batch of friends.
// Returns a map of friend IDs to their avatar URLs.
func (m *FriendsMenu) fetchFriendsThumbnails(friends []types.Friend) (map[uint64]string, error) {
	thumbnailURLs := make(map[uint64]string)

	// Create batch request for all friend avatars
	requests := thumbnails.NewBatchThumbnailsBuilder()
	for _, user := range friends {
		requests.AddRequest(types.ThumbnailRequest{
			Type:      types.AvatarType,
			TargetID:  user.ID,
			RequestID: strconv.FormatUint(user.ID, 10),
			Size:      types.Size150x150,
			Format:    types.WEBP,
		})
	}

	// Send batch request to Roblox API
	thumbnailResponses, err := m.handler.roAPI.Thumbnails().GetBatchThumbnails(context.Background(), requests.Build())
	if err != nil {
		m.handler.logger.Error("Error fetching batch thumbnails", zap.Error(err))
		return thumbnailURLs, err
	}

	// Process responses and store URLs
	for _, response := range thumbnailResponses {
		if response.State == types.ThumbnailStateCompleted && response.ImageURL != nil {
			thumbnailURLs[response.TargetID] = *response.ImageURL
		} else {
			thumbnailURLs[response.TargetID] = "-"
		}
	}

	m.handler.logger.Info("Fetched batch thumbnails",
		zap.Int("friends", len(friends)),
		zap.Int("fetchedThumbnails", len(thumbnailResponses)))

	return thumbnailURLs, nil
}
