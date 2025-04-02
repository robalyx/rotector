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
func (t *ThumbnailFetcher) GetImageURLs(ctx context.Context, users map[uint64]*types.User) map[uint64]string {
	// Create batch request for headshots
	requests := thumbnails.NewBatchThumbnailsBuilder()
	for _, user := range users {
		requests.AddRequest(apiTypes.ThumbnailRequest{
			Type:      apiTypes.AvatarType,
			TargetID:  user.ID,
			RequestID: strconv.FormatUint(user.ID, 10),
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
	ctx context.Context, groups map[uint64]*types.Group,
) map[uint64]*types.Group {
	// Create batch request for group icons
	requests := thumbnails.NewBatchThumbnailsBuilder()
	for _, group := range groups {
		requests.AddRequest(apiTypes.ThumbnailRequest{
			Type:      apiTypes.GroupIconType,
			TargetID:  group.ID,
			RequestID: strconv.FormatUint(group.ID, 10),
			Size:      apiTypes.Size420x420,
			Format:    apiTypes.WEBP,
		})
	}

	// Process thumbnails
	results := t.ProcessBatchThumbnails(ctx, requests)

	// Add thumbnail URLs to groups
	now := time.Now()
	updatedGroups := make(map[uint64]*types.Group, len(groups))
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
) map[uint64]string {
	ctx = context.WithValue(ctx, auth.KeyAddCookie, true)

	var (
		requestList   = requests.Build()
		thumbnailURLs = make(map[uint64]string)
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
			_, err := utils.WithRetry(ctx, func() (map[uint64]string, error) {
				// Fetch batch thumbnails
				resp, err := t.roAPI.Thumbnails().GetBatchThumbnails(ctx, currentBatch)
				if err != nil {
					return nil, err
				}

				// Process batch response
				pendingRequests, results := t.processTargetBatchResponse(resp.Data, currentBatch.Requests)
				mu.Lock()
				maps.Copy(thumbnailURLs, results)
				mu.Unlock()

				// If there are still pending requests, return error to trigger retry
				currentBatch = pendingRequests.Build()
				if len(currentBatch.Requests) > 0 {
					return nil, ErrPendingThumbnails
				}

				return results, nil
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

// ProcessPlayerTokens processes thumbnail requests for player tokens and returns a slice of URLs.
func (t *ThumbnailFetcher) ProcessPlayerTokens(ctx context.Context, tokens []string) []string {
	if len(tokens) == 0 {
		return nil
	}

	// Process thumbnails in batches of 100
	var (
		urls      []string
		mu        sync.Mutex
		p         = pool.New().WithContext(ctx)
		batchSize = 100
	)

	for i := 0; i < len(tokens); i += batchSize {
		p.Go(func(ctx context.Context) error {
			end := min(i+batchSize, len(tokens))
			batchTokens := tokens[i:end]

			// Create batch request
			batchRequests := thumbnails.NewBatchThumbnailsBuilder()
			for _, token := range batchTokens {
				batchRequests.AddRequest(apiTypes.ThumbnailRequest{
					Type:      apiTypes.AvatarType,
					TargetID:  0,
					RequestID: token,
					Token:     token,
					Size:      apiTypes.Size420x420,
					Format:    apiTypes.WEBP,
				})
			}
			currentBatch := batchRequests.Build()

			_, err := utils.WithRetry(ctx, func() (map[string]string, error) {
				// Fetch batch thumbnails
				resp, err := t.roAPI.Thumbnails().GetBatchThumbnails(ctx, currentBatch)
				if err != nil {
					return nil, err
				}

				// Process batch response
				pendingRequests, results := t.processTokenBatchResponse(resp.Data, currentBatch.Requests)

				// Extract URLs from successful responses
				var batchURLs []string
				for _, url := range results {
					if url != ThumbnailPlaceholder {
						batchURLs = append(batchURLs, url)
					}
				}

				mu.Lock()
				urls = append(urls, batchURLs...)
				mu.Unlock()

				// If there are still pending requests, return error to trigger retry
				currentBatch = pendingRequests.Build()
				if len(currentBatch.Requests) > 0 {
					return nil, ErrPendingThumbnails
				}

				return results, nil
			}, utils.GetThumbnailRetryOptions())
			if err != nil {
				t.logger.Warn("Failed to fetch player token thumbnails after retries",
					zap.Error(err),
					zap.Int("batchStart", i),
					zap.Int("pendingCount", len(currentBatch.Requests)))
			}

			return nil
		})
	}

	if err := p.Wait(); err != nil {
		t.logger.Error("Error processing player tokens", zap.Error(err))
	}

	t.logger.Debug("Processed player tokens",
		zap.Int("totalRequested", len(tokens)),
		zap.Int("successfulFetches", len(urls)))

	return urls
}

// processTargetBatchResponse processes thumbnail responses for target IDs and returns pending requests and results.
func (t *ThumbnailFetcher) processTargetBatchResponse(
	responses []apiTypes.ThumbnailData, requests []apiTypes.ThumbnailRequest,
) (*thumbnails.BatchThumbnailsBuilder, map[uint64]string) {
	pendingRequests := thumbnails.NewBatchThumbnailsBuilder()
	results := make(map[uint64]string)

	// Create map of targetID to request for O(1) lookup
	requestMap := make(map[uint64]apiTypes.ThumbnailRequest, len(requests))
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

// processTokenBatchResponse processes thumbnail responses for tokens and returns pending requests and results.
func (t *ThumbnailFetcher) processTokenBatchResponse(
	responses []apiTypes.ThumbnailData, requests []apiTypes.ThumbnailRequest,
) (*thumbnails.BatchThumbnailsBuilder, map[string]string) {
	pendingRequests := thumbnails.NewBatchThumbnailsBuilder()
	results := make(map[string]string)

	// Create map of token to request for O(1) lookup
	requestMap := make(map[string]apiTypes.ThumbnailRequest, len(requests))
	for _, req := range requests {
		requestMap[req.Token] = req
	}

	for _, response := range responses {
		// Find the original request to get the token
		req, ok := requestMap[response.RequestID]
		if !ok {
			continue
		}

		switch response.State {
		case apiTypes.ThumbnailStateCompleted:
			// If thumbnail is processed successfully, add it to the results
			if response.ImageURL != nil {
				results[req.Token] = *response.ImageURL
			}
		case apiTypes.ThumbnailStatePending:
			// Add to pending requests for retry if found in original requests
			pendingRequests.AddRequest(req)
		case apiTypes.ThumbnailStateError,
			apiTypes.ThumbnailStateInReview,
			apiTypes.ThumbnailStateBlocked,
			apiTypes.ThumbnailStateUnavailable:
			// If thumbnail is in a bad state, add a placeholder to the results
			results[req.Token] = ThumbnailPlaceholder
		}
	}

	return pendingRequests, results
}
