package interaction

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jaxron/axonet/pkg/client"
	"github.com/robalyx/rotector/assets"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/roblox/fetcher"
	"go.uber.org/zap"
	"golang.org/x/image/webp"
)

var ErrImageDownloadFailed = errors.New("failed to download image after 3 attempts")

// StreamRequest contains all the parameters needed for streaming images.
type StreamRequest struct {
	Event     CommonEvent
	Session   *session.Session
	Page      *Page
	URLFunc   func() []string
	Columns   int
	Rows      int
	MaxItems  int
	OnSuccess func(buf *bytes.Buffer)
}

// DownloadResult represents the result of an image download.
type DownloadResult struct {
	index int
	data  []byte
}

// ImageStreamer handles progressive loading and merging of images.
type ImageStreamer struct {
	manager        *Manager
	logger         *zap.Logger
	client         *client.Client
	placeholderImg image.Image
}

// NewImageStreamer creates a new ImageStreamer instance.
func NewImageStreamer(manager *Manager, logger *zap.Logger, client *client.Client) *ImageStreamer {
	// Load placeholder image for missing or failed thumbnails
	placeholderImg, _, err := image.Decode(bytes.NewReader(assets.ContentDeleted))
	if err != nil {
		logger.Fatal("Failed to decode placeholder image", zap.Error(err))
	}

	return &ImageStreamer{
		manager:        manager,
		logger:         logger.Named("streaming"),
		client:         client,
		placeholderImg: placeholderImg,
	}
}

// Stream starts the image streaming process.
func (is *ImageStreamer) Stream(req StreamRequest) {
	// Show initial loading message
	session.ImageBuffer.Set(req.Session, new(bytes.Buffer))
	session.PaginationIsStreaming.Set(req.Session, true)
	is.manager.Display(req.Event, req.Session, req.Page, "Loading images...")

	// Get URLs through URLFunc
	urls := req.URLFunc()
	if len(urls) == 0 {
		is.logger.Error("No URLs provided for streaming")
		return
	}

	// Create request context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var (
		mu               sync.RWMutex
		downloadedImages = make([][]byte, len(urls))
		failedDownloads  atomic.Int32
		resultChan       = make(chan DownloadResult, len(urls))
		doneChan         = make(chan struct{})
	)

	// Start downloading images concurrently
	for i, url := range urls {
		go func(index int, url string) {
			// Attempt download with retries
			data, err := is.downloadImage(ctx, url)
			if err != nil {
				is.logger.Warn("Failed to download image after retries",
					zap.Error(err),
					zap.String("url", url))
				failedDownloads.Add(1)

				resultChan <- DownloadResult{index: index}

				return
			}

			resultChan <- DownloadResult{index: index, data: data}
		}(i, url)
	}

	// Collect downloaded images as they complete
	go func() {
		for range urls {
			select {
			case <-ctx.Done():
				return
			case result := <-resultChan:
				if result.data != nil {
					mu.Lock()

					downloadedImages[result.index] = result.data

					mu.Unlock()
				}
			}
		}

		close(doneChan)
	}()

	// Update display periodically until all images are downloaded
	ticker := time.NewTicker(1500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Clean up streaming state before returning
			is.createAndDisplayGrid(req, downloadedImages, &mu, "Image loading timed out")
			session.PaginationIsStreaming.Set(req.Session, false)

			return
		case <-doneChan:
			// Final update when all images are downloaded
			message := "Images loaded"
			if failed := failedDownloads.Load(); failed > 0 {
				message = fmt.Sprintf("Images loaded (%d failed)", failed)
			}

			is.createAndDisplayGrid(req, downloadedImages, &mu, message)
			session.PaginationIsStreaming.Set(req.Session, false)

			return
		case <-ticker.C:
			// Periodic update while images are still downloading
			message := "Loading images..."
			if failed := failedDownloads.Load(); failed > 0 {
				message = fmt.Sprintf("Loading images... (%d failed)", failed)
			}

			is.createAndDisplayGrid(req, downloadedImages, &mu, message)
		}
	}
}

// downloadImage attempts to download an image with retries.
func (is *ImageStreamer) downloadImage(ctx context.Context, url string) ([]byte, error) {
	// Skip if URL is invalid
	if url == "" || url == fetcher.ThumbnailPlaceholder {
		return nil, nil
	}

	// Try up to 3 times with 2 seconds timeout each
	for attempt := 1; attempt <= 3; attempt++ {
		// Create timeout context for this attempt
		downloadCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()

		// Attempt download
		resp, err := is.client.NewRequest().URL(url).Do(downloadCtx)
		if err != nil {
			is.logger.Debug("Download attempt failed",
				zap.Int("attempt", attempt),
				zap.Error(err),
				zap.String("url", url))

			continue
		}
		defer resp.Body.Close()

		// Read image data
		buf := new(bytes.Buffer)
		if _, err := buf.ReadFrom(resp.Body); err != nil {
			is.logger.Debug("Failed to read image data",
				zap.Int("attempt", attempt),
				zap.Error(err),
				zap.String("url", url))

			continue
		}

		return buf.Bytes(), nil
	}

	return nil, ErrImageDownloadFailed
}

// createAndDisplayGrid creates and displays the image grid.
func (is *ImageStreamer) createAndDisplayGrid(req StreamRequest, images [][]byte, mu *sync.RWMutex, message string) {
	mu.Lock()
	defer mu.Unlock()

	if len(images) == 0 {
		return
	}

	// Create image grid from downloaded images
	buf, err := is.mergeImageBytes(images, req.Columns, req.Rows, req.MaxItems)
	if err != nil {
		is.logger.Error("Failed to merge images", zap.Error(err))
		return
	}

	// Call success callback if provided
	if req.OnSuccess != nil {
		req.OnSuccess(buf)
	}

	// Update Discord message with progress or completion status
	is.manager.Display(req.Event, req.Session, req.Page, message)
}

// mergeImageBytes combines multiple images into a single grid layout.
func (is *ImageStreamer) mergeImageBytes(images [][]byte, columns, rows, maxItems int) (*bytes.Buffer, error) {
	// Define dimensions for the grid and individual images
	imgWidth := 150
	imgHeight := 150
	gridWidth := imgWidth * columns
	gridHeight := imgHeight * rows

	// Create destination image for the grid
	dst := image.NewRGBA(image.Rect(0, 0, gridWidth, gridHeight))

	// Place each image in its grid position
	for i, imgBytes := range images {
		if i >= maxItems {
			continue
		}

		// Decode source image
		img, err := webp.Decode(bytes.NewReader(imgBytes))
		if err != nil {
			img = is.placeholderImg
		}

		// Calculate position in grid
		x := (i % columns) * imgWidth
		y := (i / columns) * imgHeight

		// Draw image into grid
		draw.Draw(dst, image.Rect(x, y, x+imgWidth, y+imgHeight), img, image.Point{}, draw.Over)
	}

	// Encode final grid image to PNG
	buf := new(bytes.Buffer)
	if err := png.Encode(buf, dst); err != nil {
		return nil, fmt.Errorf("failed to encode PNG: %w", err)
	}

	return buf, nil
}
