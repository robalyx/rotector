package reviewer

import (
	"bytes"
	"context"
	"fmt"

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

// OutfitsMenu represents the outfit viewer handler.
type OutfitsMenu struct {
	handler *Handler
	page    *pagination.Page
}

// NewOutfitsMenu returns a new outfit viewer handler.
func NewOutfitsMenu(h *Handler) *OutfitsMenu {
	o := OutfitsMenu{handler: h}
	o.page = &pagination.Page{
		Name: "Outfits Menu",
		Data: make(map[string]interface{}),
		Message: func(data map[string]interface{}) *discord.MessageUpdateBuilder {
			user := data["user"].(*database.PendingUser)
			outfits := data["outfits"].([]types.OutfitData)
			start := data["start"].(int)
			page := data["page"].(int)
			total := data["total"].(int)
			file := data["file"].(*discord.File)
			fileName := data["fileName"].(string)

			return builders.NewOutfitsEmbed(user, outfits, start, page, total, file, fileName).Build()
		},
		ButtonHandlerFunc: func(event *events.ComponentInteractionCreate, s *session.Session, customID string) {
			switch customID {
			case string(builders.ViewerFirstPage), string(builders.ViewerPrevPage), string(builders.ViewerNextPage), string(builders.ViewerLastPage):
				o.handlePageNavigation(event, s, builders.ViewerAction(customID))
			case string(builders.ViewerBackToReview):
				o.handler.reviewMenu.ShowReviewMenu(event, s, "")
			}
		},
	}

	return &o
}

// ShowOutfitsMenu shows the outfits menu for the given page.
func (o *OutfitsMenu) ShowOutfitsMenu(event *events.ComponentInteractionCreate, s *session.Session, page int) {
	user := s.GetPendingUser(session.KeyTarget)

	// Check if the user has outfits
	if len(user.Outfits) == 0 {
		o.handler.reviewMenu.ShowReviewMenu(event, s, "No outfits found for this user.")
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
	thumbnailURLs, err := o.fetchOutfitThumbnails(pageOutfits)
	if err != nil {
		o.handler.logger.Error("Failed to fetch outfit thumbnails", zap.Error(err))
		o.handler.respondWithError(event, "Failed to fetch outfit thumbnails. Please try again.")
		return
	}

	// Download and merge outfit images
	buf, err := utils.MergeImages(o.handler.roAPI.GetClient(), thumbnailURLs, constants.OutfitGridColumns, constants.OutfitGridRows, constants.OutfitsPerPage)
	if err != nil {
		o.handler.logger.Error("Failed to merge outfit images", zap.Error(err))
		o.handler.respondWithError(event, "Failed to process outfit images. Please try again.")
		return
	}

	// Calculate total pages
	total := (len(outfits) + constants.OutfitsPerPage - 1) / constants.OutfitsPerPage

	// Create necessary embed and components
	fileName := fmt.Sprintf("outfits_%d_%d.png", user.ID, page)
	file := discord.NewFile(fileName, "", bytes.NewReader(buf.Bytes()))

	// Set the data for the page
	o.page.Data["user"] = user
	o.page.Data["outfits"] = pageOutfits
	o.page.Data["start"] = start
	o.page.Data["page"] = page
	o.page.Data["total"] = total
	o.page.Data["file"] = file
	o.page.Data["fileName"] = fileName

	// Navigate to the outfits menu and update the message
	o.handler.paginationManager.NavigateTo(o.page.Name, s)
	o.handler.paginationManager.UpdateMessage(event, s, o.page, "")
}

// handlePageNavigation handles the page navigation for the outfits menu.
func (o *OutfitsMenu) handlePageNavigation(event *events.ComponentInteractionCreate, s *session.Session, action builders.ViewerAction) {
	switch action {
	case builders.ViewerFirstPage, builders.ViewerPrevPage, builders.ViewerNextPage, builders.ViewerLastPage:
		user := s.GetPendingUser(session.KeyTarget)

		// Get the page numbers for the action
		maxPage := (len(user.Outfits) - 1) / constants.OutfitsPerPage
		page, ok := action.ParsePageAction(s, action, maxPage)
		if !ok {
			o.handler.respondWithError(event, "Invalid interaction.")
			return
		}

		o.ShowOutfitsMenu(event, s, page)
	case builders.ViewerBackToReview:
		o.handler.reviewMenu.ShowReviewMenu(event, s, "")
	default:
		o.handler.logger.Warn("Invalid outfit viewer action", zap.String("action", string(action)))
		o.handler.respondWithError(event, "Invalid interaction.")
	}
}

// fetchOutfitThumbnails fetches thumbnails for the given outfits.
func (o *OutfitsMenu) fetchOutfitThumbnails(outfits []types.OutfitData) ([]string, error) {
	thumbnailURLs := make([]string, constants.OutfitsPerPage)

	// Create thumbnail requests for each outfit
	requests := thumbnails.NewBatchThumbnailsBuilder()
	for i, outfit := range outfits {
		if i >= constants.OutfitsPerPage {
			break
		}

		requests.AddRequest(types.ThumbnailRequest{
			Type:      "Outfit",
			Size:      types.Size150x150,
			RequestID: fmt.Sprintf("%d:undefined:Outfit:150x150:webp:regular", outfit.ID),
			TargetID:  outfit.ID,
			Format:    "webp",
		})
	}

	// Fetch batch thumbnails
	thumbnailResponses, err := o.handler.roAPI.Thumbnails().GetBatchThumbnails(context.Background(), requests.Build())
	if err != nil {
		return thumbnailURLs, err
	}

	// Process thumbnail responses
	for i, response := range thumbnailResponses {
		if response.State == "Completed" && response.ImageURL != nil {
			thumbnailURLs[i] = *response.ImageURL
		} else {
			thumbnailURLs[i] = "-"
		}
	}

	o.handler.logger.Info("Fetched thumbnail URLs", zap.Strings("urls", thumbnailURLs))

	return thumbnailURLs, nil
}
