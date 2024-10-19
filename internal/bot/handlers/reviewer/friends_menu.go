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
	"github.com/rotector/rotector/internal/bot/handlers/reviewer/builders"
	"github.com/rotector/rotector/internal/bot/handlers/reviewer/constants"
	"github.com/rotector/rotector/internal/bot/pagination"
	"github.com/rotector/rotector/internal/bot/session"
	"github.com/rotector/rotector/internal/common/database"
	"github.com/rotector/rotector/internal/common/utils"
	"go.uber.org/zap"
)

// FriendsMenu handles the friends viewer functionality.
type FriendsMenu struct {
	handler *Handler
	page    *pagination.Page
}

// NewFriendsMenu creates a new FriendsMenu instance.
func NewFriendsMenu(h *Handler) *FriendsMenu {
	f := FriendsMenu{handler: h}
	f.page = &pagination.Page{
		Name: "Friends Menu",
		Data: make(map[string]interface{}),
		Message: func(data map[string]interface{}) *discord.MessageUpdateBuilder {
			user := data["user"].(*database.PendingUser)
			friends := data["friends"].([]types.UserResponse)
			flaggedFriends := data["flaggedFriends"].(map[uint64]string)
			start := data["start"].(int)
			page := data["page"].(int)
			total := data["total"].(int)
			file := data["file"].(*discord.File)
			fileName := data["fileName"].(string)

			return builders.NewFriendsEmbed(user, friends, flaggedFriends, start, page, total, file, fileName).Build()
		},
		ButtonHandlerFunc: func(event *events.ComponentInteractionCreate, s *session.Session, option string) {
			switch option {
			case string(builders.ViewerFirstPage), string(builders.ViewerPrevPage), string(builders.ViewerNextPage), string(builders.ViewerLastPage):
				f.handlePageNavigation(event, s, builders.ViewerAction(option))
			case string(builders.ViewerBackToReview):
				h.reviewMenu.ShowReviewMenu(event, s, "")
			}
		},
	}

	return &f
}

// ShowFriendsMenu shows the friends menu for the given page.
func (f *FriendsMenu) ShowFriendsMenu(event *events.ComponentInteractionCreate, s *session.Session, page int) {
	user := s.GetPendingUser(session.KeyTarget)

	// Check if the user has friends
	if len(user.Friends) == 0 {
		f.handler.reviewMenu.ShowReviewMenu(event, s, "No friends found for this user.")
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
	friendIDs := make([]uint64, len(pageFriends))
	for i, friend := range pageFriends {
		friendIDs[i] = friend.ID
	}

	flaggedFriends, err := f.handler.db.Users().CheckExistingUsers(friendIDs)
	if err != nil {
		f.handler.logger.Error("Failed to check existing friends", zap.Error(err))
		f.handler.respondWithError(event, "Failed to check existing friends. Please try again.")
		return
	}

	// Fetch thumbnails for the page friends
	friendsThumbnailURLs, err := f.fetchFriendsThumbnails(pageFriends)
	if err != nil {
		f.handler.logger.Error("Failed to fetch friends thumbnails", zap.Error(err))
		f.handler.respondWithError(event, "Failed to fetch friends thumbnails. Please try again.")
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
	buf, err := utils.MergeImages(f.handler.roAPI.GetClient(), pageThumbnailURLs, constants.FriendsGridColumns, constants.FriendsGridRows, constants.FriendsPerPage)
	if err != nil {
		f.handler.logger.Error("Failed to merge friend images", zap.Error(err))
		f.handler.respondWithError(event, "Failed to process friend images. Please try again.")
		return
	}

	// Create file attachment
	fileName := fmt.Sprintf("friends_%d_%d.png", user.ID, page)
	file := discord.NewFile(fileName, "", bytes.NewReader(buf.Bytes()))

	// Calculate total pages
	total := (len(friends) + constants.FriendsPerPage - 1) / constants.FriendsPerPage

	// Set the data for the page
	f.page.Data["user"] = user
	f.page.Data["friends"] = pageFriends
	f.page.Data["flaggedFriends"] = flaggedFriends
	f.page.Data["start"] = start
	f.page.Data["page"] = page
	f.page.Data["total"] = total
	f.page.Data["file"] = file
	f.page.Data["fileName"] = fileName

	// Navigate to the friends menu and update the message
	f.handler.paginationManager.NavigateTo(f.page.Name, s)
	f.handler.paginationManager.UpdateMessage(event, s, f.page, "")
}

// handlePageNavigation handles the page navigation for the friends menu.
func (f *FriendsMenu) handlePageNavigation(event *events.ComponentInteractionCreate, s *session.Session, action builders.ViewerAction) {
	switch action {
	case builders.ViewerFirstPage, builders.ViewerPrevPage, builders.ViewerNextPage, builders.ViewerLastPage:
		user := s.GetPendingUser(session.KeyTarget)

		// Get the page number for the action
		maxPage := (len(user.Friends) - 1) / constants.FriendsPerPage
		page, ok := action.ParsePageAction(s, action, maxPage)
		if !ok {
			f.handler.respondWithError(event, "Invalid interaction.")
			return
		}

		f.ShowFriendsMenu(event, s, page)
	case builders.ViewerBackToReview:
		f.handler.reviewMenu.ShowReviewMenu(event, s, "")
	default:
		f.handler.logger.Warn("Invalid friends viewer action", zap.String("action", string(action)))
		f.handler.respondWithError(event, "Invalid interaction.")
	}
}

// fetchFriendsThumbnails fetches thumbnails for the given friends.
func (f *FriendsMenu) fetchFriendsThumbnails(friends []types.UserResponse) (map[uint64]string, error) {
	thumbnailURLs := make(map[uint64]string)

	// Create thumbnail requests for each friend
	requests := thumbnails.NewBatchThumbnailsBuilder()
	for _, user := range friends {
		requests.AddRequest(types.ThumbnailRequest{
			Type:      types.AvatarType,
			TargetID:  user.ID,
			RequestID: strconv.FormatUint(user.ID, 10),
			Size:      types.Size150x150,
			Format:    "webp",
		})
	}

	// Fetch batch thumbnails
	thumbnailResponses, err := f.handler.roAPI.Thumbnails().GetBatchThumbnails(context.Background(), requests.Build())
	if err != nil {
		f.handler.logger.Error("Error fetching batch thumbnails", zap.Error(err))
		return thumbnailURLs, err
	}

	// Process thumbnail responses
	for _, response := range thumbnailResponses {
		if response.State == "Completed" && response.ImageURL != nil {
			thumbnailURLs[response.TargetID] = *response.ImageURL
		} else {
			thumbnailURLs[response.TargetID] = "-"
		}
	}

	f.handler.logger.Info("Fetched batch thumbnails",
		zap.Int("friends", len(friends)),
		zap.Int("fetchedThumbnails", len(thumbnailResponses)))

	return thumbnailURLs, nil
}
