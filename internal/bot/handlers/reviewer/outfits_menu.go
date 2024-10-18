package reviewer

import (
	"bytes"
	"context"
	"fmt"
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
	OutfitsPerPage    = 15
	OutfitGridColumns = 3
	OutfitGridRows    = 5

	OutfitsMenuPrefix             = "outfit_viewer:"
	OpenOutfitsMenuButtonCustomID = "open_outfit_viewer"
)

// OutfitsMenu represents the outfit viewer handler.
type OutfitsMenu struct {
	handler *Handler
}

// NewOutfitsMenu returns a new outfit viewer handler.
func NewOutfitsMenu(h *Handler) *OutfitsMenu {
	return &OutfitsMenu{handler: h}
}

// ShowOutfitsMenu displays the outfit viewer page.
func (o *OutfitsMenu) ShowOutfitsMenu(event *events.ComponentInteractionCreate, s *session.Session, page int) {
	// Get the user from the session
	user := s.GetPendingUser(session.KeyTarget)
	if user == nil {
		o.handler.respondWithError(event, "Bot lost track of the user. Please try again.")
		return
	}

	// Check if the user has outfits
	if len(user.Outfits) == 0 {
		o.handler.reviewMenu.ShowReviewMenu(event, s, "No outfits found for this user.")
		return
	}

	outfits := user.Outfits

	// Get outfits for the current page
	start := page * OutfitsPerPage
	end := start + OutfitsPerPage
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
	buf, err := o.handler.mergeImages(thumbnailURLs, OutfitGridColumns, OutfitGridRows, OutfitsPerPage)
	if err != nil {
		o.handler.logger.Error("Failed to merge outfit images", zap.Error(err))
		o.handler.respondWithError(event, "Failed to process outfit images. Please try again.")
		return
	}

	// Calculate total pages
	totalPages := (len(outfits) + OutfitsPerPage - 1) / OutfitsPerPage

	// Create necessary embed and components
	fileName := fmt.Sprintf("outfits_%d_%d.png", user.ID, page)
	file := discord.NewFile(fileName, "", bytes.NewReader(buf.Bytes()))

	embed := builders.NewOutfitsEmbed(user, pageOutfits, start, page, totalPages, fileName).Build()
	components := []discord.ContainerComponent{
		discord.NewActionRow(
			discord.NewSecondaryButton("◀️", OutfitsMenuPrefix+string(ViewerBackToReview)),
			discord.NewSecondaryButton("⏮️", OutfitsMenuPrefix+string(ViewerFirstPage)).WithDisabled(page == 0),
			discord.NewSecondaryButton("◀️", OutfitsMenuPrefix+string(ViewerPrevPage)).WithDisabled(page == 0),
			discord.NewSecondaryButton("▶️", OutfitsMenuPrefix+string(ViewerNextPage)).WithDisabled(page == totalPages-1),
			discord.NewSecondaryButton("⏭️", OutfitsMenuPrefix+string(ViewerLastPage)).WithDisabled(page == totalPages-1),
		),
	}

	// Update the interaction response with the outfit viewer page
	o.handler.respond(event, builders.NewResponse().
		SetEmbeds(embed).
		SetComponents(components...).
		AddFile(file))
}

// HandleOutfitsMenu processes the outfit viewer interaction.
func (o *OutfitsMenu) HandleOutfitsMenu(event *events.ComponentInteractionCreate, s *session.Session) {
	// Parse the custom ID
	parts := strings.Split(event.Data.CustomID(), ":")
	if len(parts) < 2 {
		o.handler.logger.Warn("Invalid outfit viewer custom ID format", zap.String("customID", event.Data.CustomID()))
		o.handler.respondWithError(event, "Invalid interaction.")
		return
	}

	// Determine the action based on the custom ID
	action := ViewerAction(parts[1])
	switch action {
	case ViewerFirstPage, ViewerPrevPage, ViewerNextPage, ViewerLastPage:
		// Get the user from the session
		user := s.GetPendingUser(session.KeyTarget)
		if user == nil {
			o.handler.respondWithError(event, "Bot lost track of the user. Please try again.")
			return
		}

		// Get the page number for the action
		maxPage := (len(user.Outfits) - 1) / OutfitsPerPage
		page, ok := o.handler.parsePageAction(s, action, maxPage)
		if !ok {
			o.handler.respondWithError(event, "Invalid interaction.")
			return
		}

		o.ShowOutfitsMenu(event, s, page)
	case ViewerBackToReview:
		o.handler.reviewMenu.ShowReviewMenu(event, s, "")
	default:
		o.handler.logger.Warn("Invalid outfit viewer action", zap.String("action", string(action)))
		o.handler.respondWithError(event, "Invalid interaction.")
	}
}

// fetchOutfitThumbnails fetches thumbnails for the given outfits.
func (o *OutfitsMenu) fetchOutfitThumbnails(outfits []types.OutfitData) ([]string, error) {
	thumbnailURLs := make([]string, OutfitsPerPage)

	// Create thumbnail requests for each outfit
	requests := thumbnails.NewBatchThumbnailsBuilder()
	for i, outfit := range outfits {
		if i >= OutfitsPerPage {
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
