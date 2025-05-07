package user

import (
	"bytes"
	"context"
	"strconv"

	"github.com/disgoorg/disgo/discord"
	"github.com/jaxron/roapi.go/pkg/api/resources/thumbnails"
	apiTypes "github.com/jaxron/roapi.go/pkg/api/types"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/interaction"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/bot/handlers/review/shared"
	view "github.com/robalyx/rotector/internal/bot/views/review/user"
	"github.com/robalyx/rotector/internal/database/types"
	"github.com/robalyx/rotector/internal/database/types/enum"
)

// FriendsMenu handles the display and interaction logic for viewing a user's friends.
type FriendsMenu struct {
	layout *Layout
	page   *interaction.Page
}

// NewFriendsMenu creates a new friends menu.
func NewFriendsMenu(layout *Layout) *FriendsMenu {
	m := &FriendsMenu{layout: layout}
	m.page = &interaction.Page{
		Name: constants.UserFriendsPageName,
		Message: func(s *session.Session) *discord.MessageUpdateBuilder {
			return view.NewFriendsBuilder(s).Build()
		},
		DisableSelectMenuReset: true,
		ShowHandlerFunc:        m.Show,
		ButtonHandlerFunc:      m.handleButton,
		ModalHandlerFunc:       m.handleModal,
		CleanupHandlerFunc: func(s *session.Session) {
			session.ImageBuffer.Delete(s)
		},
	}
	return m
}

// Show prepares and displays the friends interface.
func (m *FriendsMenu) Show(ctx *interaction.Context, s *session.Session) {
	// If ImageBuffer exists, we can skip fetching data
	if session.ImageBuffer.Get(s) != nil {
		return
	}

	user := session.UserTarget.Get(s)

	// Return to review menu if user has no friends
	if len(user.Friends) == 0 {
		ctx.Cancel("No friends found for this user.")
		return
	}

	// Get friend types from session and sort friends by status
	flaggedFriends := session.UserFlaggedFriends.Get(s)
	sortedFriends := m.sortFriendsByStatus(user.Friends, flaggedFriends)

	// Calculate page boundaries
	page := session.PaginationPage.Get(s)
	totalPages := max((len(sortedFriends)-1)/constants.FriendsPerPage, 0)

	start := page * constants.FriendsPerPage
	end := min(start+constants.FriendsPerPage, len(sortedFriends))
	pageFriends := sortedFriends[start:end]

	// Start fetching presences for visible friends in background
	friendIDs := make([]uint64, len(pageFriends))
	for i, friend := range pageFriends {
		friendIDs[i] = friend.ID
	}
	presenceChan := m.layout.presenceFetcher.FetchPresencesConcurrently(ctx.Context(), friendIDs)

	// Store initial data in session
	session.UserFriends.Set(s, pageFriends)
	session.PaginationOffset.Set(s, start)
	session.PaginationTotalItems.Set(s, len(sortedFriends))
	session.PaginationTotalPages.Set(s, totalPages)

	// Start streaming images
	m.layout.imageStreamer.Stream(interaction.StreamRequest{
		Event:    ctx.Event(),
		Session:  s,
		Page:     m.page,
		URLFunc:  func() []string { return m.fetchFriendThumbnails(ctx.Context(), pageFriends) },
		Columns:  constants.FriendsGridColumns,
		Rows:     constants.FriendsGridRows,
		MaxItems: constants.FriendsPerPage,
		OnSuccess: func(buf *bytes.Buffer) {
			session.ImageBuffer.Set(s, buf)
		},
	})

	// Store presences when they arrive
	presenceMap := <-presenceChan
	session.UserPresences.Set(s, presenceMap)
}

// handleButton processes button clicks.
func (m *FriendsMenu) handleButton(ctx *interaction.Context, s *session.Session, customID string) {
	action := session.ViewerAction(customID)
	switch action {
	case session.ViewerFirstPage, session.ViewerPrevPage, session.ViewerNextPage, session.ViewerLastPage:
		totalPages := session.PaginationTotalPages.Get(s)
		page := action.ParsePageAction(s, totalPages)

		session.ImageBuffer.Delete(s)
		session.PaginationPage.Set(s, page)
		ctx.Reload("")
		return
	}

	switch customID {
	case constants.EditReasonButtonCustomID:
		user := session.UserTarget.Get(s)
		shared.HandleEditReason(
			ctx, s, m.layout.logger, enum.UserReasonTypeFriend, user.Reasons,
			func(r types.Reasons[enum.UserReasonType]) {
				user.Reasons = r
				session.UserTarget.Set(s, user)
			},
		)
	case constants.BackButtonCustomID:
		ctx.NavigateBack("")
	}
}

// handleModal handles modal submissions for the friends menu.
func (m *FriendsMenu) handleModal(ctx *interaction.Context, s *session.Session) {
	switch ctx.Event().CustomID() {
	case constants.AddReasonModalCustomID:
		user := session.UserTarget.Get(s)
		shared.HandleReasonModalSubmit(
			ctx, s, user.Reasons, enum.UserReasonTypeString,
			func(r types.Reasons[enum.UserReasonType]) {
				user.Reasons = r
				session.UserTarget.Set(s, user)
			},
			func(c float64) {
				user.Confidence = c
				session.UserTarget.Set(s, user)
			},
		)
	}
}

// sortFriendsByStatus sorts friends into categories based on their status.
func (m *FriendsMenu) sortFriendsByStatus(
	friends []*apiTypes.ExtendedFriend, flaggedFriends map[uint64]*types.ReviewUser,
) []*apiTypes.ExtendedFriend {
	// Group friends by their status
	groupedFriends := make(map[enum.UserType][]*apiTypes.ExtendedFriend)
	var unflaggedFriends []*apiTypes.ExtendedFriend

	// Separate flagged and unflagged friends
	for _, friend := range friends {
		if reviewUser, exists := flaggedFriends[friend.ID]; exists {
			groupedFriends[reviewUser.Status] = append(groupedFriends[reviewUser.Status], friend)
		} else {
			unflaggedFriends = append(unflaggedFriends, friend)
		}
	}

	// Define status priority order
	statusOrder := []enum.UserType{
		enum.UserTypeConfirmed,
		enum.UserTypeFlagged,
		enum.UserTypeCleared,
	}

	// Combine friends in priority order
	sortedFriends := make([]*apiTypes.ExtendedFriend, 0, len(friends))
	for _, status := range statusOrder {
		sortedFriends = append(sortedFriends, groupedFriends[status]...)
	}

	// Append unflagged friends last
	sortedFriends = append(sortedFriends, unflaggedFriends...)

	return sortedFriends
}

// fetchFriendThumbnails fetches thumbnails for a slice of friends.
func (m *FriendsMenu) fetchFriendThumbnails(ctx context.Context, friends []*apiTypes.ExtendedFriend) []string {
	// Create batch request for friend avatars
	requests := thumbnails.NewBatchThumbnailsBuilder()
	for _, friend := range friends {
		requests.AddRequest(apiTypes.ThumbnailRequest{
			Type:      apiTypes.AvatarType,
			TargetID:  friend.ID,
			RequestID: strconv.FormatUint(friend.ID, 10),
			Size:      apiTypes.Size150x150,
			Format:    apiTypes.WEBP,
		})
	}

	// Process thumbnails
	thumbnailMap := m.layout.thumbnailFetcher.ProcessBatchThumbnails(ctx, requests)

	// Convert map to ordered slice of URLs
	thumbnailURLs := make([]string, len(friends))
	for i, friend := range friends {
		if url, ok := thumbnailMap[friend.ID]; ok {
			thumbnailURLs[i] = url
		}
	}

	return thumbnailURLs
}
