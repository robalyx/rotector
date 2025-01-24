package user

import (
	"bytes"
	"context"
	"strconv"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/jaxron/roapi.go/pkg/api/resources/thumbnails"
	apiTypes "github.com/jaxron/roapi.go/pkg/api/types"
	builder "github.com/robalyx/rotector/internal/bot/builder/review/user"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/pagination"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"go.uber.org/zap"
)

// OutfitsMenu handles the display and interaction logic for viewing a user's outfits.
type OutfitsMenu struct {
	layout *Layout
	page   *pagination.Page
}

// NewOutfitsMenu creates an OutfitsMenu and sets up its page with message builders
// and interaction handlers. The page is configured to show outfit information
// and handle navigation.
func NewOutfitsMenu(layout *Layout) *OutfitsMenu {
	m := &OutfitsMenu{layout: layout}
	m.page = &pagination.Page{
		Name: "Outfits Menu",
		Message: func(s *session.Session) *discord.MessageUpdateBuilder {
			return builder.NewOutfitsBuilder(s).Build()
		},
		ButtonHandlerFunc: m.handlePageNavigation,
	}
	return m
}

// Show prepares and displays the outfits interface for a specific page.
func (m *OutfitsMenu) Show(event *events.ComponentInteractionCreate, s *session.Session, page int) {
	user := session.UserTarget.Get(s)

	// Return to review menu if user has no outfits
	if len(user.Outfits) == 0 {
		m.layout.reviewMenu.Show(event, s, "No outfits found for this user.")
		return
	}

	// Calculate page boundaries
	start := page * constants.OutfitsPerPage
	end := start + constants.OutfitsPerPage
	if end > len(user.Outfits) {
		end = len(user.Outfits)
	}
	pageOutfits := user.Outfits[start:end]

	// Store data in session for the message builder
	session.UserOutfits.Set(s, pageOutfits)
	session.PaginationOffset.Set(s, start)
	session.PaginationPage.Set(s, page)
	session.PaginationTotalItems.Set(s, len(user.Outfits))

	// Start streaming images
	m.layout.imageStreamer.Stream(pagination.StreamRequest{
		Event:    event,
		Session:  s,
		Page:     m.page,
		URLFunc:  func() []string { return m.fetchOutfitThumbnails(pageOutfits) },
		Columns:  constants.OutfitGridColumns,
		Rows:     constants.OutfitGridRows,
		MaxItems: constants.OutfitsPerPage,
		OnSuccess: func(buf *bytes.Buffer) {
			session.ImageBuffer.Set(s, buf)
		},
	})
}

// handlePageNavigation processes navigation button clicks by calculating
// the target page number and refreshing the display.
func (m *OutfitsMenu) handlePageNavigation(event *events.ComponentInteractionCreate, s *session.Session, customID string) {
	action := session.ViewerAction(customID)
	switch action {
	case session.ViewerFirstPage, session.ViewerPrevPage, session.ViewerNextPage, session.ViewerLastPage:
		user := session.UserTarget.Get(s)

		// Calculate max page and validate navigation action
		maxPage := (len(user.Outfits) - 1) / constants.OutfitsPerPage
		page := action.ParsePageAction(s, action, maxPage)

		m.Show(event, s, page)

	case constants.BackButtonCustomID:
		m.layout.paginationManager.NavigateBack(event, s, "")

	default:
		m.layout.logger.Warn("Invalid outfits viewer action", zap.String("action", string(action)))
		m.layout.paginationManager.RespondWithError(event, "Invalid interaction.")
	}
}

// fetchOutfitThumbnails gets the thumbnail URLs for a list of outfits.
func (m *OutfitsMenu) fetchOutfitThumbnails(outfits []*apiTypes.Outfit) []string {
	// Create batch request for outfit thumbnails
	requests := thumbnails.NewBatchThumbnailsBuilder()
	for _, outfit := range outfits {
		requests.AddRequest(apiTypes.ThumbnailRequest{
			Type:      apiTypes.OutfitType,
			TargetID:  outfit.ID,
			RequestID: strconv.FormatUint(outfit.ID, 10),
			Size:      apiTypes.Size150x150,
			Format:    apiTypes.WEBP,
		})
	}

	// Process thumbnails
	thumbnailMap := m.layout.thumbnailFetcher.ProcessBatchThumbnails(context.Background(), requests)

	// Convert map to ordered slice of URLs
	thumbnailURLs := make([]string, len(outfits))
	for i, outfit := range outfits {
		if url, ok := thumbnailMap[outfit.ID]; ok {
			thumbnailURLs[i] = url
		}
	}

	return thumbnailURLs
}
