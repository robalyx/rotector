package reviewer

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"sync"

	gim "github.com/ozankasikci/go-image-merge"
	"github.com/rotector/rotector/assets"
	"github.com/rotector/rotector/internal/bot/session"
	"go.uber.org/zap"
	"golang.org/x/image/webp"
)

type ViewerAction string

const (
	ViewerFirstPage    ViewerAction = "first_page"
	ViewerPrevPage     ViewerAction = "prev_page"
	ViewerNextPage     ViewerAction = "next_page"
	ViewerLastPage     ViewerAction = "last_page"
	ViewerBackToReview ViewerAction = "back_to_review"
)

// parsePageAction parses the page type from the custom ID.
func (h *Handler) parsePageAction(s *session.Session, action ViewerAction, maxPage int) (int, bool) {
	switch action {
	case ViewerFirstPage:
		return 0, true
	case ViewerPrevPage:
		prevPage := s.GetInt(session.KeyPaginationPage) - 1
		if prevPage < 0 {
			prevPage = 0
		}
		s.Set(session.KeyPaginationPage, prevPage)
		return prevPage, true
	case ViewerNextPage:
		nextPage := s.GetInt(session.KeyPaginationPage) + 1
		if nextPage > maxPage {
			nextPage = maxPage
		}
		s.Set(session.KeyPaginationPage, nextPage)
		return nextPage, true
	case ViewerLastPage:
		return maxPage, true
	default:
		return 0, false
	} //exhaustive:ignore
}

// mergeImages downloads and merges images into a grid.
func (h *Handler) mergeImages(thumbnailURLs []string, columns, rows, perPage int) (*bytes.Buffer, error) {
	// Load placeholder image
	imageFile, err := assets.Images.Open("images/content_deleted.png")
	if err != nil {
		h.logger.Error("Failed to open placeholder image", zap.Error(err))
		return nil, err
	}
	defer func() { _ = imageFile.Close() }()

	placeholderImg, _, err := image.Decode(imageFile)
	if err != nil {
		h.logger.Error("Failed to decode placeholder image", zap.Error(err))
		return nil, err
	}

	// Load grids with empty image
	emptyImg := image.NewRGBA(image.Rect(0, 0, 150, 150))

	grids := make([]*gim.Grid, perPage)
	for i := range grids {
		grids[i] = &gim.Grid{Image: emptyImg}
	}

	// Download and process outfit images concurrently
	var wg sync.WaitGroup
	var mu sync.Mutex

	for i, url := range thumbnailURLs {
		// Ensure we don't exceed the perPage limit
		if i >= perPage {
			break
		}

		// Skip if the URL is empty
		if url == "" {
			continue
		}

		// Skip if the URL was in a different state
		if url == "-" {
			grids[i] = &gim.Grid{Image: placeholderImg}
			continue
		}

		wg.Add(1)
		go func(index int, imageURL string) {
			defer wg.Done()

			var img image.Image

			// Download image
			resp, err := h.roAPI.GetClient().NewRequest().URL(imageURL).Do(context.Background())
			if err != nil {
				h.logger.Warn("Failed to download image", zap.Error(err), zap.String("url", imageURL))
				img = placeholderImg
			} else {
				defer resp.Body.Close()
				decodedImg, err := webp.Decode(resp.Body)
				if err != nil {
					h.logger.Warn("Failed to decode image", zap.Error(err), zap.String("url", imageURL))
					img = placeholderImg
				} else {
					img = decodedImg
				}
			}

			// Safely update the grids slice
			mu.Lock()
			grids[index] = &gim.Grid{Image: img}
			mu.Unlock()
		}(i, url)
	}

	wg.Wait()

	// Merge images
	mergedImage, err := gim.New(grids, columns, rows).Merge()
	if err != nil {
		h.logger.Error("Failed to merge images", zap.Error(err))
		return nil, err
	}

	// Encode the merged image to PNG
	var buf bytes.Buffer
	if err := png.Encode(&buf, mergedImage); err != nil {
		h.logger.Error("Failed to encode merged image", zap.Error(err))
		return nil, err
	}

	return &buf, nil
}
