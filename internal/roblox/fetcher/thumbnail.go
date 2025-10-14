package fetcher

import (
	"context"
	"errors"
	"maps"
	"strconv"
	"sync"
	"time"

	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/jaxron/roapi.go/pkg/api/middleware/auth"
	"github.com/jaxron/roapi.go/pkg/api/resources/thumbnails"
	apiTypes "github.com/jaxron/roapi.go/pkg/api/types"
	"github.com/robalyx/rotector/internal/database/types"
	"github.com/robalyx/rotector/pkg/utils"
	"github.com/sourcegraph/conc/pool"
	"go.uber.org/zap"
)

// ThumbnailPlaceholder represents a placeholder value used when a thumbnail is unavailable or in an error state.
const ThumbnailPlaceholder = "-"

// ErrPendingThumbnails is an error returned when some thumbnails are still pending.
var ErrPendingThumbnails = errors.New("some thumbnails still pending")

// ThumbnailFetcher handles retrieval of user and group thumbnails from the Roblox API.
type ThumbnailFetcher struct {
	roAPI  *api.API
	logger *zap.Logger
}

// NewThumbnailFetcher creates a ThumbnailFetcher with the provided API client and logger.
func NewThumbnailFetcher(roAPI *api.API, logger *zap.Logger) *ThumbnailFetcher {
	return &ThumbnailFetcher{
		roAPI:  roAPI,
		logger: logger.Named("thumbnail_fetcher"),
	}
}

// GetImageURLs fetches thumbnails for a batch of users and returns a map of results.
func (t *ThumbnailFetcher) GetImageURLs(ctx context.Context, users map[int64]*types.User) map[int64]string {
	// Create batch request for headshots
	requests := thumbnails.NewBatchThumbnailsBuilder()
	for _, user := range users {
		requests.AddRequest(apiTypes.ThumbnailRequest{
			Type:      apiTypes.AvatarType,
			TargetID:  user.ID,
			RequestID: strconv.FormatInt(user.ID, 10),
			Size:      apiTypes.Size420x420,
			Format:    apiTypes.WEBP,
		})
	}

	// Process thumbnails
	results := t.ProcessBatchThumbnails(ctx, requests)

	t.logger.Debug("Finished fetching user thumbnails",
		zap.Int("totalUsers", len(users)),
		zap.Int("successfulFetches", len(results)))

	return results
}

// AddGroupImageURLs fetches thumbnails for groups and adds them to the group records.
func (t *ThumbnailFetcher) AddGroupImageURLs(
	ctx context.Context, groups map[int64]*types.ReviewGroup,
) map[int64]*types.ReviewGroup {
	// Create batch request for group icons
	requests := thumbnails.NewBatchThumbnailsBuilder()
	for _, group := range groups {
		requests.AddRequest(apiTypes.ThumbnailRequest{
			Type:      apiTypes.GroupIconType,
			TargetID:  group.ID,
			RequestID: strconv.FormatInt(group.ID, 10),
			Size:      apiTypes.Size420x420,
			Format:    apiTypes.WEBP,
		})
	}

	// Process thumbnails
	results := t.ProcessBatchThumbnails(ctx, requests)

	// Add thumbnail URLs to groups
	now := time.Now()

	updatedGroups := make(map[int64]*types.ReviewGroup, len(groups))
	for _, group := range groups {
		if thumbnailURL, ok := results[group.ID]; ok {
			group.ThumbnailURL = thumbnailURL
			group.LastThumbnailUpdate = now
			updatedGroups[group.ID] = group
		}
	}

	t.logger.Debug("Finished fetching group thumbnails",
		zap.Int("totalGroups", len(groups)),
		zap.Int("successfulFetches", len(updatedGroups)))

	return updatedGroups
}

// ProcessBatchThumbnails handles batched thumbnail requests, processing them in groups of 100.
// It returns a map of target IDs to their thumbnail URLs.
func (t *ThumbnailFetcher) ProcessBatchThumbnails(
	ctx context.Context, requests *thumbnails.BatchThumbnailsBuilder,
) map[int64]string {
	ctx = context.WithValue(ctx, auth.KeyAddCookie, true)

	var (
		requestList   = requests.Build()
		thumbnailURLs = make(map[int64]string)
		p             = pool.New().WithContext(ctx)
		mu            sync.Mutex
		batchSize     = 100
	)

	// Process batches concurrently
	for i := 0; i < len(requestList.Requests); i += batchSize {
		p.Go(func(ctx context.Context) error {
			end := min(i+batchSize, len(requestList.Requests))
			batchRequests := requestList.Requests[i:end]

			// Create batch request
			initialBatch := thumbnails.NewBatchThumbnailsBuilder()
			for _, request := range batchRequests {
				initialBatch.AddRequest(request)
			}

			currentBatch := initialBatch.Build()

			err := utils.WithRetry(ctx, func() error {
				// Fetch batch thumbnails
				resp, err := t.roAPI.Thumbnails().GetBatchThumbnails(ctx, currentBatch)
				if err != nil {
					return err
				}

				// Process batch response
				pendingRequests, results := t.processTargetBatchResponse(resp.Data, currentBatch.Requests)

				mu.Lock()
				maps.Copy(thumbnailURLs, results)
				mu.Unlock()

				// If there are still pending requests, return error to trigger retry
				// Note: we don't want to wait for 2 pending thumbnails to be fetched
				currentBatch = pendingRequests.Build()
				if len(currentBatch.Requests) > 2 {
					return ErrPendingThumbnails
				}

				return nil
			}, utils.GetThumbnailRetryOptions())
			if err != nil {
				t.logger.Warn("Failed to fetch thumbnails after retries",
					zap.Error(err),
					zap.Int("batchStart", i),
					zap.Int("pendingCount", len(currentBatch.Requests)))
			}

			return nil
		})
	}

	// Wait for all goroutines to complete
	if err := p.Wait(); err != nil {
		t.logger.Error("Error during thumbnail processing", zap.Error(err))
	}

	t.logger.Debug("Processed batch thumbnails",
		zap.Int("totalRequested", len(requestList.Requests)),
		zap.Int("successfulFetches", len(thumbnailURLs)))

	return thumbnailURLs
}

// processTargetBatchResponse processes thumbnail responses for target IDs and returns pending requests and results.
func (t *ThumbnailFetcher) processTargetBatchResponse(
	responses []apiTypes.ThumbnailData, requests []apiTypes.ThumbnailRequest,
) (*thumbnails.BatchThumbnailsBuilder, map[int64]string) {
	pendingRequests := thumbnails.NewBatchThumbnailsBuilder()
	results := make(map[int64]string)

	// Create map of targetID to request for O(1) lookup
	requestMap := make(map[int64]apiTypes.ThumbnailRequest, len(requests))
	for _, req := range requests {
		requestMap[req.TargetID] = req
	}

	for _, response := range responses {
		switch response.State {
		case apiTypes.ThumbnailStateCompleted:
			// If thumbnail is processed successfully, add it to the results
			if response.ImageURL != nil {
				results[response.TargetID] = *response.ImageURL
			}
		case apiTypes.ThumbnailStatePending:
			// Add to pending requests for retry if found in original requests
			if req, ok := requestMap[response.TargetID]; ok {
				pendingRequests.AddRequest(req)
			}
		case apiTypes.ThumbnailStateError,
			apiTypes.ThumbnailStateInReview,
			apiTypes.ThumbnailStateBlocked,
			apiTypes.ThumbnailStateUnavailable:
			// If thumbnail is in a bad state, add a placeholder to the results
			results[response.TargetID] = ThumbnailPlaceholder
		}
	}

	return pendingRequests, results
}
