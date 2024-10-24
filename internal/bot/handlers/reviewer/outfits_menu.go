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

// OutfitsMenu represents the outfit viewer handler.
type OutfitsMenu struct {
	handler *Handler
	page    *pagination.Page
}

// NewOutfitsMenu returns a new outfit viewer handler.
func NewOutfitsMenu(h *Handler) *OutfitsMenu {
	m := OutfitsMenu{handler: h}
	m.page = &pagination.Page{
		Name: "Outfits Menu",
		Message: func(s *session.Session) *discord.MessageUpdateBuilder {
			return builders.NewOutfitsEmbed(s).Build()
		},
		ButtonHandlerFunc: m.handlePageNavigation,
	}
	return &m
}

// ShowOutfitsMenu shows the outfits menu for the given page.
func (m *OutfitsMenu) ShowOutfitsMenu(event *events.ComponentInteractionCreate, s *session.Session, page int) {
	user := s.GetPendingUser(constants.KeyTarget)

	// Check if the user has outfits
	if len(user.Outfits) == 0 {
		m.handler.reviewMenu.ShowReviewMenu(event, s, "No outfits found for this user.")
		return
	}

	outfits := user.Outfits

	// Get outfits for the current page
	start := page * constants.OutfitsPerPage
	end := start + constants.OutfitsPerPage
	if end > len(outfits) {
		end = len(outfits)
	}
	pageOutfits := outfits[start:end]

	// Fetch thumbnails for the outfits
	thumbnailURLs, err := m.fetchOutfitThumbnails(pageOutfits)
	if err != nil {
		m.handler.logger.Error("Failed to fetch outfit thumbnails", zap.Error(err))
		utils.RespondWithError(event, "Failed to fetch outfit thumbnails. Please try again.")
		return
	}

	// Download and merge outfit images
	buf, err := utils.MergeImages(m.handler.roAPI.GetClient(), thumbnailURLs, constants.OutfitGridColumns, constants.OutfitGridRows, constants.OutfitsPerPage)
	if err != nil {
		m.handler.logger.Error("Failed to merge outfit images", zap.Error(err))
		utils.RespondWithError(event, "Failed to process outfit images. Please try again.")
		return
	}

	// Calculate total pages
	total := (len(outfits) + constants.OutfitsPerPage - 1) / constants.OutfitsPerPage

	// Create necessary embed and components
	fileName := fmt.Sprintf("outfits_%d_%d.png", user.ID, page)
	file := discord.NewFile(fileName, "", bytes.NewReader(buf.Bytes()))

	// Get user settings
	settings, err := m.handler.db.Settings().GetUserSettings(uint64(event.User().ID))
	if err != nil {
		m.handler.logger.Error("Failed to get user settings", zap.Error(err))
		utils.RespondWithError(event, "Failed to get user settings. Please try again.")
		return
	}

	s.Set(constants.SessionKeyUser, user)
	s.Set(constants.SessionKeyOutfits, pageOutfits)
	s.Set(constants.SessionKeyStart, start)
	s.Set(constants.SessionKeyPage, page)
	s.Set(constants.SessionKeyTotal, total)
	s.Set(constants.SessionKeyFile, file)
	s.Set(constants.SessionKeyFileName, fileName)
	s.Set(constants.SessionKeyStreamerMode, settings.StreamerMode)

	// Navigate to the outfits menu and update the message
	m.handler.paginationManager.NavigateTo(m.page.Name, s)
	m.handler.paginationManager.UpdateMessage(event, s, m.page, "")
}

// handlePageNavigation handles the page navigation for the outfits menu.
func (m *OutfitsMenu) handlePageNavigation(event *events.ComponentInteractionCreate, s *session.Session, customID string) {
	action := builders.ViewerAction(customID)
	switch action {
	case builders.ViewerFirstPage, builders.ViewerPrevPage, builders.ViewerNextPage, builders.ViewerLastPage:
		user := s.GetPendingUser(constants.KeyTarget)

		// Get the page numbers for the action
		maxPage := (len(user.Outfits) - 1) / constants.OutfitsPerPage
		page, ok := action.ParsePageAction(s, action, maxPage)
		if !ok {
			utils.RespondWithError(event, "Invalid interaction.")
			return
		}

		m.ShowOutfitsMenu(event, s, page)
	case builders.ViewerBackToReview:
		m.handler.reviewMenu.ShowReviewMenu(event, s, "")
	default:
		m.handler.logger.Warn("Invalid outfits viewer action", zap.String("action", string(action)))
		utils.RespondWithError(event, "Invalid interaction.")
	}
}

// fetchOutfitThumbnails fetches thumbnails for the given outfits.
func (m *OutfitsMenu) fetchOutfitThumbnails(outfits []types.Outfit) ([]string, error) {
	thumbnailURLs := make([]string, constants.OutfitsPerPage)

	// Create thumbnail requests for each outfit
	requests := thumbnails.NewBatchThumbnailsBuilder()
	for i, outfit := range outfits {
		if i >= constants.OutfitsPerPage {
			break
		}

		requests.AddRequest(types.ThumbnailRequest{
			Type:      types.OutfitType,
			Size:      types.Size150x150,
			RequestID: strconv.FormatUint(outfit.ID, 10),
			TargetID:  outfit.ID,
			Format:    types.WEBP,
		})
	}

	// Fetch batch thumbnails
	thumbnailResponses, err := m.handler.roAPI.Thumbnails().GetBatchThumbnails(context.Background(), requests.Build())
	if err != nil {
		return thumbnailURLs, err
	}

	// Process thumbnail responses
	for i, response := range thumbnailResponses {
		if response.State == types.ThumbnailStateCompleted && response.ImageURL != nil {
			thumbnailURLs[i] = *response.ImageURL
		} else {
			thumbnailURLs[i] = "-"
		}
	}

	m.handler.logger.Info("Fetched thumbnail URLs", zap.Strings("urls", thumbnailURLs))

	return thumbnailURLs, nil
}
