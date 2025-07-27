package user

import (
	"bytes"
	"context"
	"strconv"
	"strings"

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
	"go.uber.org/zap"
)

// OutfitsMenu handles the display and interaction logic for viewing a user's outfits.
type OutfitsMenu struct {
	layout *Layout
	page   *interaction.Page
}

// NewOutfitsMenu creates a new outfits menu.
func NewOutfitsMenu(layout *Layout) *OutfitsMenu {
	m := &OutfitsMenu{layout: layout}
	m.page = &interaction.Page{
		Name: constants.UserOutfitsPageName,
		Message: func(s *session.Session) *discord.MessageUpdateBuilder {
			return view.NewOutfitsBuilder(s).Build()
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

// Show prepares and displays the outfits interface for a specific page.
func (m *OutfitsMenu) Show(ctx *interaction.Context, s *session.Session) {
	// If ImageBuffer exists, we can skip fetching data
	if session.ImageBuffer.Get(s) != nil {
		return
	}

	user := session.UserTarget.Get(s)

	// Return to review menu if user has no outfits
	if len(user.Outfits) == 0 {
		ctx.Cancel("No outfits found for this user.")
		return
	}

	// Create map of flagged outfit names from evidence
	flaggedOutfits := make(map[string]struct{})
	if outfitReason, ok := user.Reasons[enum.UserReasonTypeOutfit]; ok {
		// Create a map of outfit name to outfit for quick lookup
		outfitMap := make(map[string]*apiTypes.Outfit)
		for _, outfit := range user.Outfits {
			outfitMap[outfit.Name] = outfit
		}

		// Parse evidence using pipe delimiter pattern
		for _, evidence := range outfitReason.Evidence {
			parts := strings.Split(evidence, "|")
			if len(parts) >= 2 {
				// Check if this outfit name exists in the user's outfits
				outfitName := strings.TrimSpace(parts[0])
				if _, exists := outfitMap[outfitName]; exists {
					flaggedOutfits[outfitName] = struct{}{}
				}
			}
		}
	}

	session.UserFlaggedOutfits.Set(s, flaggedOutfits)

	// Detect duplicate outfit names
	duplicateFlaggedOutfitNames := make(map[string]struct{})
	if len(flaggedOutfits) > 0 {
		outfitNameCounts := make(map[string]int)
		for _, outfit := range user.Outfits {
			outfitNameCounts[outfit.Name]++
		}

		for flaggedName := range flaggedOutfits {
			if outfitNameCounts[flaggedName] > 1 {
				duplicateFlaggedOutfitNames[flaggedName] = struct{}{}
			}
		}
	}

	session.UserDuplicateOutfitNames.Set(s, duplicateFlaggedOutfitNames)

	// Calculate page boundaries
	page := session.PaginationPage.Get(s)
	totalPages := max((len(user.Outfits)-1)/constants.OutfitsPerPage, 0)

	start := page * constants.OutfitsPerPage
	end := min(start+constants.OutfitsPerPage, len(user.Outfits))
	pageOutfits := user.Outfits[start:end]

	// Store data in session for the message builder
	session.UserOutfits.Set(s, pageOutfits)
	session.PaginationOffset.Set(s, start)
	session.PaginationTotalItems.Set(s, len(user.Outfits))
	session.PaginationTotalPages.Set(s, totalPages)

	// Start streaming images
	m.layout.imageStreamer.Stream(interaction.StreamRequest{
		Event:    ctx.Event(),
		Session:  s,
		Page:     m.page,
		URLFunc:  func() []string { return m.fetchOutfitThumbnails(ctx.Context(), pageOutfits) },
		Columns:  constants.OutfitGridColumns,
		Rows:     constants.OutfitGridRows,
		MaxItems: constants.OutfitsPerPage,
		OnSuccess: func(buf *bytes.Buffer) {
			session.ImageBuffer.Set(s, buf)
		},
	})
}

// handleButton processes button clicks.
func (m *OutfitsMenu) handleButton(ctx *interaction.Context, s *session.Session, customID string) {
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
			ctx, s, m.layout.logger, enum.UserReasonTypeOutfit, user.Reasons,
			func(r types.Reasons[enum.UserReasonType]) {
				user.Reasons = r
				session.UserTarget.Set(s, user)
			},
		)
	case constants.BackButtonCustomID:
		ctx.NavigateBack("")
	default:
		m.layout.logger.Warn("Invalid outfits viewer action", zap.String("action", string(action)))
		ctx.Error("Invalid interaction.")
	}
}

// handleModal processes modal submissions.
func (m *OutfitsMenu) handleModal(ctx *interaction.Context, s *session.Session) {
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

// fetchOutfitThumbnails gets the thumbnail URLs for a list of outfits.
func (m *OutfitsMenu) fetchOutfitThumbnails(ctx context.Context, outfits []*apiTypes.Outfit) []string {
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
	thumbnailMap := m.layout.thumbnailFetcher.ProcessBatchThumbnails(ctx, requests)

	// Convert map to ordered slice of URLs
	thumbnailURLs := make([]string, len(outfits))
	for i, outfit := range outfits {
		if url, ok := thumbnailMap[outfit.ID]; ok {
			thumbnailURLs[i] = url
		}
	}

	return thumbnailURLs
}
