package pagination

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"sync"
	"sync/atomic"
	"time"

	"github.com/disgoorg/disgo/events"
	"github.com/jaxron/axonet/pkg/client"
	"github.com/robalyx/rotector/assets"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"go.uber.org/zap"
	"golang.org/x/image/webp"
)

// StreamRequest contains all the parameters needed for streaming images.
type StreamRequest struct {
	Event     *events.ComponentInteractionCreate
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
	paginationManager *Manager
	logger            *zap.Logger
	client            *client.Client
	placeholderImg    image.Image
}

// NewImageStreamer creates a new ImageStreamer instance.
func NewImageStreamer(paginationManager *Manager, logger *zap.Logger, client *client.Client) *ImageStreamer {
	// Load placeholder image for missing or failed thumbnails
	imageFile, err := assets.Images.Open("images/content_deleted.png")
	if err != nil {
		logger.Fatal("Failed to load placeholder image", zap.Error(err))
	}
	defer func() { _ = imageFile.Close() }()

	placeholderImg, _, err := image.Decode(imageFile)
	if err != nil {
		logger.Fatal("Failed to decode placeholder image", zap.Error(err))
	}

	return &ImageStreamer{
		paginationManager: paginationManager,
		logger:            logger,
		client:            client,
		placeholderImg:    placeholderImg,
	}
}

// Stream starts the image streaming process.
func (is *ImageStreamer) Stream(req StreamRequest) {
	// Show initial loading message
	session.ImageBuffer.Set(req.Session, new(bytes.Buffer))
	session.IsStreaming.Set(req.Session, true)
	is.paginationManager.NavigateTo(req.Event, req.Session, req.Page, "Loading images...")

	// Get URLs through URLFunc
	urls := req.URLFunc()
	if len(urls) == 0 {
		is.logger.Error("No URLs provided for streaming")
		return
	}

	// Track downloaded images and failed downloads
	var (
		mu               sync.RWMutex
		downloadedImages = make([][]byte, len(urls))
		failedDownloads  atomic.Int32
		resultChan       = make(chan DownloadResult, len(urls))
		doneChan         = make(chan struct{})
	)

	// Create request context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()

	// Start downloading images concurrently
	for i, url := range urls {
		go func(index int, url string) {
			// Skip if the URL is empty
			if url == "" {
				resultChan <- DownloadResult{index: index}
				return
			}

			// Download image
			resp, err := is.client.NewRequest().URL(url).Do(ctx)
			if err != nil {
				is.logger.Warn("Failed to download image", zap.Error(err), zap.String("url", url))
				failedDownloads.Add(1)
				resultChan <- DownloadResult{index: index}
				return
			}
			defer resp.Body.Close()

			// Read image data into buffer
			buf := new(bytes.Buffer)
			if _, err := buf.ReadFrom(resp.Body); err != nil {
				is.logger.Warn("Failed to read image", zap.Error(err), zap.String("url", url))
				failedDownloads.Add(1)
				resultChan <- DownloadResult{index: index}
				return
			}

			resultChan <- DownloadResult{index: index, data: buf.Bytes()}
		}(i, url)
	}

	// Collect downloaded images as they complete
	go func() {
		for range urls {
			result := <-resultChan
			if result.data != nil {
				mu.Lock()
				downloadedImages[result.index] = result.data
				mu.Unlock()
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
			session.IsStreaming.Set(req.Session, false)
			is.createAndDisplayGrid(req, downloadedImages, &mu, "Images took too long to load")
			return
		case <-doneChan:
			// Final update when all images are downloaded
			message := "Images loaded"
			if failed := failedDownloads.Load(); failed > 0 {
				message = fmt.Sprintf("Images loaded (%d failed)", failed)
			}

			session.IsStreaming.Set(req.Session, false)
			is.createAndDisplayGrid(req, downloadedImages, &mu, message)
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
	is.paginationManager.NavigateTo(req.Event, req.Session, req.Page, message)
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
