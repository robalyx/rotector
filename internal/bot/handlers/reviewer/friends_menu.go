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

// FriendsMenu handles the friends viewer functionality.
type FriendsMenu struct {
	handler *Handler
	page    *pagination.Page
}

// NewFriendsMenu creates a new FriendsMenu instance.
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

// ShowFriendsMenu shows the friends menu for the given page.
func (m *FriendsMenu) ShowFriendsMenu(event *events.ComponentInteractionCreate, s *session.Session, page int) {
	user := s.GetPendingUser(constants.KeyTarget)

	// Check if the user has friends
	if len(user.Friends) == 0 {
		m.handler.reviewMenu.ShowReviewMenu(event, s, "No friends found for this user.")
		return
	}
	friends := user.Friends

	// Get friends for the current page
	start := page * constants.FriendsPerPage
	end := start + constants.FriendsPerPage
	if end > len(friends) {
		end = len(friends)
	}
	pageFriends := friends[start:end]

	// Check which friends are flagged
	flaggedFriends, err := m.getFlaggedFriends(pageFriends)
	if err != nil {
		m.handler.logger.Error("Failed to get flagged friends", zap.Error(err))
		utils.RespondWithError(event, "Failed to get flagged friends. Please try again.")
		return
	}

	// Fetch thumbnails for the page friends
	friendsThumbnailURLs, err := m.fetchFriendsThumbnails(pageFriends)
	if err != nil {
		m.handler.logger.Error("Failed to fetch friends thumbnails", zap.Error(err))
		utils.RespondWithError(event, "Failed to fetch friends thumbnails. Please try again.")
		return
	}

	// Get thumbnail URLs for the page friends
	pageThumbnailURLs := make([]string, len(pageFriends))
	for i, friend := range pageFriends {
		if url, ok := friendsThumbnailURLs[friend.ID]; ok {
			pageThumbnailURLs[i] = url
		}
	}

	// Download and merge friend images
	buf, err := utils.MergeImages(m.handler.roAPI.GetClient(), pageThumbnailURLs, constants.FriendsGridColumns, constants.FriendsGridRows, constants.FriendsPerPage)
	if err != nil {
		m.handler.logger.Error("Failed to merge friend images", zap.Error(err))
		utils.RespondWithError(event, "Failed to process friend images. Please try again.")
		return
	}

	// Create file attachment
	fileName := fmt.Sprintf("friends_%d_%d.png", user.ID, page)
	file := discord.NewFile(fileName, "", bytes.NewReader(buf.Bytes()))

	// Calculate total pages
	total := (len(friends) + constants.FriendsPerPage - 1) / constants.FriendsPerPage

	// Get user settings
	settings, err := m.handler.db.Settings().GetUserSettings(uint64(event.User().ID))
	if err != nil {
		m.handler.logger.Error("Failed to get user settings", zap.Error(err))
		utils.RespondWithError(event, "Failed to get user settings. Please try again.")
		return
	}

	// Set the data for the page
	s.Set(constants.KeyTarget, user)
	s.Set(constants.SessionKeyFriends, pageFriends)
	s.Set(constants.SessionKeyFlaggedFriends, flaggedFriends)
	s.Set(constants.SessionKeyStart, start)
	s.Set(constants.SessionKeyPage, page)
	s.Set(constants.SessionKeyTotal, total)
	s.Set(constants.SessionKeyFile, file)
	s.Set(constants.SessionKeyFileName, fileName)
	s.Set(constants.SessionKeyStreamerMode, settings.StreamerMode)

	// Navigate to the friends menu and update the message
	m.handler.paginationManager.NavigateTo(m.page.Name, s)
	m.handler.paginationManager.UpdateMessage(event, s, m.page, "")
}

// getFlaggedFriends gets the flagged friends for the given friends.
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

// handlePageNavigation handles the page navigation for the friends menu.
func (m *FriendsMenu) handlePageNavigation(event *events.ComponentInteractionCreate, s *session.Session, customID string) {
	action := builders.ViewerAction(customID)
	switch action {
	case builders.ViewerFirstPage, builders.ViewerPrevPage, builders.ViewerNextPage, builders.ViewerLastPage:
		user := s.GetPendingUser(constants.KeyTarget)

		// Get the page number for the action
		maxPage := (len(user.Friends) - 1) / constants.FriendsPerPage
		page, ok := action.ParsePageAction(s, action, maxPage)
		if !ok {
			utils.RespondWithError(event, "Invalid interaction.")
			return
		}

		m.ShowFriendsMenu(event, s, page)
	case builders.ViewerBackToReview:
		m.handler.reviewMenu.ShowReviewMenu(event, s, "")
	default:
		m.handler.logger.Warn("Invalid friends viewer action", zap.String("action", string(action)))
		utils.RespondWithError(event, "Invalid interaction.")
	}
}

// fetchFriendsThumbnails fetches thumbnails for the given friends.
func (m *FriendsMenu) fetchFriendsThumbnails(friends []types.Friend) (map[uint64]string, error) {
	thumbnailURLs := make(map[uint64]string)

	// Create thumbnail requests for each friend
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
		zap.Int("friends", len(friends)),
		zap.Int("fetchedThumbnails", len(thumbnailResponses)))

	return thumbnailURLs, nil
}
