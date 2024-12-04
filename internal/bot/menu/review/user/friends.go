package user

import (
	"strconv"
	"sync"

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

// FriendsMenu handles the display and interaction logic for viewing a user's friends.
type FriendsMenu struct {
	layout *Layout
	page   *pagination.Page
}

// NewFriendsMenu creates a FriendsMenu and sets up its page with message builders
// and interaction handlers. The page is configured to show friend information
// and handle navigation.
func NewFriendsMenu(layout *Layout) *FriendsMenu {
	m := &FriendsMenu{layout: layout}
	m.page = &pagination.Page{
		Name: "Friends Menu",
		Message: func(s *session.Session) *discord.MessageUpdateBuilder {
			return builder.NewFriendsBuilder(s).Build()
		},
		ButtonHandlerFunc: m.handlePageNavigation,
	}
	return m
}

// Show prepares and displays the friends interface for a specific page.
// It loads friend data, checks their status, and creates a grid of avatars.
func (m *FriendsMenu) Show(event *events.ComponentInteractionCreate, s *session.Session, page int) {
	var user *types.FlaggedUser
	s.GetInterface(constants.SessionKeyTarget, &user)

	// Return to review menu if user has no friends
	if len(user.Friends) == 0 {
		m.layout.reviewMenu.Show(event, s, "No friends found for this user.")
		return
	}

	// Get friend types from session and sort friends by status
	var friendTypes map[uint64]types.UserType
	s.GetInterface(constants.SessionKeyFriendTypes, &friendTypes)
	sortedFriends := m.sortFriendsByStatus(user.Friends, friendTypes)

	// Calculate page boundaries
	start := page * constants.FriendsPerPage
	end := start + constants.FriendsPerPage
	if end > len(sortedFriends) {
		end = len(sortedFriends)
	}
	pageFriends := sortedFriends[start:end]

	// Fetch data concurrently
	thumbnailMap, presenceMap := m.fetchFriendData(sortedFriends)

	// Extract thumbnail URLs for the current page
	thumbnailURLs := make([]string, len(pageFriends))
	for i, friend := range pageFriends {
		if url, ok := thumbnailMap[friend.ID]; ok {
			thumbnailURLs[i] = url
		}
	}

	// Create grid image from avatars
	buf, err := utils.MergeImages(
		m.layout.roAPI.GetClient(),
		thumbnailURLs,
		constants.FriendsGridColumns,
		constants.FriendsGridRows,
		constants.FriendsPerPage,
	)
	if err != nil {
		m.layout.logger.Error("Failed to merge friend images", zap.Error(err))
		m.layout.paginationManager.RespondWithError(event, "Failed to process friend images. Please try again.")
		return
	}

	// Store data in session for the message builder
	s.Set(constants.SessionKeyFriends, pageFriends)
	s.Set(constants.SessionKeyPresences, presenceMap)
	s.Set(constants.SessionKeyStart, start)
	s.Set(constants.SessionKeyPaginationPage, page)
	s.Set(constants.SessionKeyTotalItems, len(sortedFriends))
	s.SetBuffer(constants.SessionKeyImageBuffer, buf)

	m.layout.paginationManager.NavigateTo(event, s, m.page, "")
}

// handlePageNavigation processes navigation button clicks by calculating
// the target page number and refreshing the display.
func (m *FriendsMenu) handlePageNavigation(event *events.ComponentInteractionCreate, s *session.Session, customID string) {
	action := utils.ViewerAction(customID)
	switch action {
	case utils.ViewerFirstPage, utils.ViewerPrevPage, utils.ViewerNextPage, utils.ViewerLastPage:
		var user *types.FlaggedUser
		s.GetInterface(constants.SessionKeyTarget, &user)

		// Calculate max page and validate navigation action
		maxPage := (len(user.Friends) - 1) / constants.FriendsPerPage
		page, ok := action.ParsePageAction(s, action, maxPage)
		if !ok {
			m.layout.paginationManager.RespondWithError(event, "Invalid interaction.")
			return
		}

		m.Show(event, s, page)

	case constants.BackButtonCustomID:
		m.layout.reviewMenu.Show(event, s, "")

	default:
		m.layout.logger.Warn("Invalid friends viewer action", zap.String("action", string(action)))
		m.layout.paginationManager.RespondWithError(event, "Invalid interaction.")
	}
}

// fetchFriendData handles concurrent fetching of thumbnails and presence information.
func (m *FriendsMenu) fetchFriendData(allFriends []types.ExtendedFriend) (map[uint64]string, map[uint64]*apiTypes.UserPresenceResponse) {
	var wg sync.WaitGroup
	var thumbnailMap map[uint64]string
	presenceMap := make(map[uint64]*apiTypes.UserPresenceResponse)

	wg.Add(2)

	// Fetch thumbnails for all friends
	go func() {
		defer wg.Done()
		// Create batch request for all friend avatars
		requests := thumbnails.NewBatchThumbnailsBuilder()
		for _, friend := range allFriends {
			requests.AddRequest(apiTypes.ThumbnailRequest{
				Type:      apiTypes.AvatarType,
				TargetID:  friend.ID,
				RequestID: strconv.FormatUint(friend.ID, 10),
				Size:      apiTypes.Size150x150,
				Format:    apiTypes.WEBP,
			})
		}

		// Process thumbnails
		thumbnailMap = m.layout.thumbnailFetcher.ProcessBatchThumbnails(requests)
	}()

	// Fetch presence information
	go func() {
		defer wg.Done()
		friendIDs := make([]uint64, len(allFriends))
		for i, friend := range allFriends {
			friendIDs[i] = friend.ID
		}

		presences := m.layout.presenceFetcher.FetchPresences(friendIDs)

		// Populate presence map with presence information
		for _, presence := range presences {
			presenceMap[presence.UserID] = presence
		}
	}()

	// Wait for both goroutines to complete
	wg.Wait()

	return thumbnailMap, presenceMap
}

// sortFriendsByStatus sorts friends into three categories based on their status:
// 1. Confirmed friends (⚠️) - Users that have been reviewed and confirmed
// 2. Flagged friends (⏳) - Users that are currently flagged for review
// 3. Unflagged friends - Users with no current flags or status
// Returns a new slice with friends sorted in this priority order.
func (m *FriendsMenu) sortFriendsByStatus(friends []types.ExtendedFriend, friendTypes map[uint64]types.UserType) []types.ExtendedFriend {
	// Create three slices for different status types
	var confirmedFriends, flaggedFriends, unflaggedFriends []types.ExtendedFriend

	// Categorize friends based on their status
	for _, friend := range friends {
		switch friendTypes[friend.ID] {
		case types.UserTypeConfirmed:
			confirmedFriends = append(confirmedFriends, friend)
		case types.UserTypeFlagged:
			flaggedFriends = append(flaggedFriends, friend)
		default:
			unflaggedFriends = append(unflaggedFriends, friend)
		} //exhaustive:ignore
	}

	// Combine slices in priority order (confirmed -> flagged -> unflagged)
	sortedFriends := make([]types.ExtendedFriend, 0, len(friends))
	sortedFriends = append(sortedFriends, confirmedFriends...)
	sortedFriends = append(sortedFriends, flaggedFriends...)
	sortedFriends = append(sortedFriends, unflaggedFriends...)

	return sortedFriends
}
