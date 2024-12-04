package user

import (
	"strconv"

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
// It loads outfit data and creates a grid of thumbnails.
func (m *OutfitsMenu) Show(event *events.ComponentInteractionCreate, s *session.Session, page int) {
	var user *types.FlaggedUser
	s.GetInterface(constants.SessionKeyTarget, &user)

	// Return to review menu if user has no outfits
	if len(user.Outfits) == 0 {
		m.layout.reviewMenu.Show(event, s, "No outfits found for this user.")
		return
	}

	outfits := user.Outfits

	// Calculate page boundaries and get subset of outfits
	start := page * constants.OutfitsPerPage
	end := start + constants.OutfitsPerPage
	if end > len(outfits) {
		end = len(outfits)
	}
	pageOutfits := outfits[start:end]

	// Download and process outfit thumbnails
	thumbnailMap := m.fetchOutfitThumbnails(outfits)

	// Extract URLs for current page
	thumbnailURLs := make([]string, len(pageOutfits))
	for i, outfit := range pageOutfits {
		if url, ok := thumbnailMap[outfit.ID]; ok {
			thumbnailURLs[i] = url
		}
	}

	// Create grid image from thumbnails
	buf, err := utils.MergeImages(
		m.layout.roAPI.GetClient(),
		thumbnailURLs,
		constants.OutfitGridColumns,
		constants.OutfitGridRows,
		constants.OutfitsPerPage,
	)
	if err != nil {
		m.layout.logger.Error("Failed to merge outfit images", zap.Error(err))
		m.layout.paginationManager.RespondWithError(event, "Failed to process outfit images. Please try again.")
		return
	}

	// Store data in session for the message builder
	s.Set(constants.SessionKeyOutfits, pageOutfits)
	s.Set(constants.SessionKeyStart, start)
	s.Set(constants.SessionKeyPaginationPage, page)
	s.Set(constants.SessionKeyTotalItems, len(outfits))
	s.SetBuffer(constants.SessionKeyImageBuffer, buf)

	m.layout.paginationManager.NavigateTo(event, s, m.page, "")
}

// handlePageNavigation processes navigation button clicks by calculating
// the target page number and refreshing the display.
func (m *OutfitsMenu) handlePageNavigation(event *events.ComponentInteractionCreate, s *session.Session, customID string) {
	action := utils.ViewerAction(customID)
	switch action {
	case utils.ViewerFirstPage, utils.ViewerPrevPage, utils.ViewerNextPage, utils.ViewerLastPage:
		var user *types.FlaggedUser
		s.GetInterface(constants.SessionKeyTarget, &user)

		// Calculate max page and validate navigation action
		maxPage := (len(user.Outfits) - 1) / constants.OutfitsPerPage
		page, ok := action.ParsePageAction(s, action, maxPage)
		if !ok {
			m.layout.paginationManager.RespondWithError(event, "Invalid interaction.")
			return
		}

		m.Show(event, s, page)

	case constants.BackButtonCustomID:
		m.layout.reviewMenu.Show(event, s, "")

	default:
		m.layout.logger.Warn("Invalid outfits viewer action", zap.String("action", string(action)))
		m.layout.paginationManager.RespondWithError(event, "Invalid interaction.")
	}
}

// fetchOutfitThumbnails downloads thumbnail images for all outfits.
// Returns a map of outfit IDs to their thumbnail URLs.
func (m *OutfitsMenu) fetchOutfitThumbnails(outfits []apiTypes.Outfit) map[uint64]string {
	// Create batch request for all outfit thumbnails
	requests := thumbnails.NewBatchThumbnailsBuilder()
	for _, outfit := range outfits {
		requests.AddRequest(apiTypes.ThumbnailRequest{
			Type:      apiTypes.OutfitType,
			Size:      apiTypes.Size150x150,
			RequestID: strconv.FormatUint(outfit.ID, 10),
			TargetID:  outfit.ID,
			Format:    apiTypes.WEBP,
		})
	}

	return m.layout.thumbnailFetcher.ProcessBatchThumbnails(requests)
}
