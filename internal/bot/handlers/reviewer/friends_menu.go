package reviewer

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/jaxron/roapi.go/pkg/api/resources/thumbnails"
	"github.com/jaxron/roapi.go/pkg/api/types"
	"github.com/rotector/rotector/internal/bot/handlers/reviewer/builders"
	"github.com/rotector/rotector/internal/bot/session"
	"go.uber.org/zap"
)

const (
	FriendsPerPage     = 21
	FriendsGridColumns = 3
	FriendsGridRows    = 7

	FriendsMenuPrefix             = "friends_viewer:"
	OpenFriendsMenuButtonCustomID = "open_friends_viewer"
)

// FriendsMenu handles the friends viewer functionality.
type FriendsMenu struct {
	handler *Handler
}

// NewFriendsMenu creates a new FriendsMenu instance.
func NewFriendsMenu(h *Handler) *FriendsMenu {
	return &FriendsMenu{handler: h}
}

// ShowFriendsMenu displays the friends viewer page.
func (f *FriendsMenu) ShowFriendsMenu(event *events.ComponentInteractionCreate, s *session.Session, page int) {
	user := s.GetPendingUser(session.KeyTarget)
	if user == nil {
		f.handler.respondWithError(event, "Bot lost track of the user. Please try again.")
		return
	}
	if len(user.Friends) == 0 {
		f.handler.respondWithError(event, "No friends found for this user.")
		return
	}

	friends := user.Friends

	// Get friends for the current page
	start := page * FriendsPerPage
	end := start + FriendsPerPage
	if end > len(friends) {
		end = len(friends)
	}
	pageFriends := friends[start:end]

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
	buf, err := f.handler.mergeImages(pageThumbnailURLs, FriendsGridColumns, FriendsGridRows, FriendsPerPage)
	if err != nil {
		f.handler.logger.Error("Failed to merge friend images", zap.Error(err))
		f.handler.respondWithError(event, "Failed to process friend images. Please try again.")
		return
	}

	// Create file attachment
	fileName := fmt.Sprintf("friends_%d_%d.png", user.ID, page)
	file := discord.NewFile(fileName, "", bytes.NewReader(buf.Bytes()))

	// Calculate total pages
	totalPages := (len(friends) + FriendsPerPage - 1) / FriendsPerPage

	// Create embed for friends list
	embed := builders.NewFriendsEmbed(user, pageFriends, start, page, totalPages, fileName).Build()

	// Create components for navigation
	components := []discord.ContainerComponent{
		discord.NewActionRow(
			discord.NewSecondaryButton("◀️", FriendsMenuPrefix+string(ViewerBackToReview)),
			discord.NewSecondaryButton("⏮️", FriendsMenuPrefix+string(ViewerFirstPage)).WithDisabled(page == 0),
			discord.NewSecondaryButton("◀️", FriendsMenuPrefix+string(ViewerPrevPage)).WithDisabled(page == 0),
			discord.NewSecondaryButton("▶️", FriendsMenuPrefix+string(ViewerNextPage)).WithDisabled(page == totalPages-1),
			discord.NewSecondaryButton("⏭️", FriendsMenuPrefix+string(ViewerLastPage)).WithDisabled(page == totalPages-1),
		),
	}

	// Create response builder
	builder := builders.NewResponse().
		SetEmbeds(embed).
		SetComponents(components...).
		AddFile(file)

	f.handler.respond(event, builder)
}

// HandleFriendsMenu processes friends viewer interactions.
func (f *FriendsMenu) HandleFriendsMenu(event *events.ComponentInteractionCreate, s *session.Session) {
	parts := strings.Split(event.Data.CustomID(), ":")
	if len(parts) < 2 {
		f.handler.logger.Warn("Invalid friends viewer custom ID format", zap.String("customID", event.Data.CustomID()))
		f.handler.respondWithError(event, "Invalid interaction.")
		return
	}

	action := ViewerAction(parts[1])
	switch action {
	case ViewerFirstPage, ViewerPrevPage, ViewerNextPage, ViewerLastPage:
		// Get the user from the session
		user := s.GetPendingUser(session.KeyTarget)
		if user == nil {
			f.handler.respondWithError(event, "Bot lost track of the user. Please try again.")
			return
		}

		// Get the page number for the action
		maxPage := (len(user.Friends) - 1) / FriendsPerPage
		page, ok := f.handler.parsePageAction(s, action, maxPage)
		if !ok {
			f.handler.respondWithError(event, "Invalid interaction.")
			return
		}

		f.ShowFriendsMenu(event, s, page)
	case ViewerBackToReview:
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
