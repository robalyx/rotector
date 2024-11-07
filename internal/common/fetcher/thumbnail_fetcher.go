package fetcher

import (
	"context"
	"strconv"
	"sync"

	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/jaxron/roapi.go/pkg/api/resources/thumbnails"
	"github.com/jaxron/roapi.go/pkg/api/types"
	"github.com/rotector/rotector/internal/common/database"
	"go.uber.org/zap"
)

// ThumbnailFetchResult contains the result of fetching thumbnails.
type ThumbnailFetchResult struct {
	URLs  map[uint64]string
	Error error
}

// ThumbnailFetcher handles retrieval of user and group thumbnails from the Roblox API.
type ThumbnailFetcher struct {
	roAPI  *api.API
	logger *zap.Logger
}

// NewThumbnailFetcher creates a ThumbnailFetcher with the provided API client and logger.
func NewThumbnailFetcher(roAPI *api.API, logger *zap.Logger) *ThumbnailFetcher {
	return &ThumbnailFetcher{
		roAPI:  roAPI,
		logger: logger,
	}
}

// AddImageURLs fetches thumbnails for a batch of users and adds them to the user records.
func (t *ThumbnailFetcher) AddImageURLs(users []*database.User) []*database.User {
	var wg sync.WaitGroup
	resultsChan := make(chan struct {
		BatchStart int
		Result     *ThumbnailFetchResult
	}, (len(users)+99)/100) // Ceiling division for batch count

	// Process users in batches of 100
	batchSize := 100
	for i := 0; i < len(users); i += batchSize {
		wg.Add(1)
		go func(start int) {
			defer wg.Done()

			// Get the current batch of users
			end := start + batchSize
			if end > len(users) {
				end = len(users)
			}
			batch := users[start:end]

			// Create batch request for headshots
			requests := thumbnails.NewBatchThumbnailsBuilder()
			for _, user := range batch {
				requests.AddRequest(types.ThumbnailRequest{
					Type:      types.AvatarHeadShotType,
					TargetID:  user.ID,
					RequestID: strconv.FormatUint(user.ID, 10),
					Size:      types.Size420x420,
					Format:    types.PNG,
				})
			}

			// Process the batch
			thumbnailURLs, err := t.processBatchThumbnails(requests)
			resultsChan <- struct {
				BatchStart int
				Result     *ThumbnailFetchResult
			}{
				BatchStart: start,
				Result: &ThumbnailFetchResult{
					URLs:  thumbnailURLs,
					Error: err,
				},
			}
		}(i)
	}

	// Close channel when all goroutines complete
	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	// Collect results from the channel
	results := make(map[uint64]string)
	for result := range resultsChan {
		if result.Result.Error != nil {
			t.logger.Error("Error fetching batch thumbnails",
				zap.Error(result.Result.Error),
				zap.Int("batchStart", result.BatchStart))
			continue
		}

		// Merge URLs from successful batches
		for id, url := range result.Result.URLs {
			results[id] = url
		}
	}

	// Add thumbnail URLs to users
	updatedUsers := make([]*database.User, 0, len(users))
	for _, user := range users {
		if thumbnailURL, ok := results[user.ID]; ok {
			user.ThumbnailURL = thumbnailURL
			updatedUsers = append(updatedUsers, user)
		}
	}

	t.logger.Info("Finished fetching user thumbnails",
		zap.Int("totalUsers", len(users)),
		zap.Int("successfulFetches", len(updatedUsers)))

	return updatedUsers
}

// AddImageURLs fetches thumbnails for users in batches of 100 and adds them to the user records.
// Failed batches are logged and skipped.
func (t *ThumbnailFetcher) AddGroupImageURLs(groups []*database.FlaggedGroup) []*database.FlaggedGroup {
	var wg sync.WaitGroup
	resultsChan := make(chan struct {
		BatchStart int
		Result     *ThumbnailFetchResult
	}, (len(groups)+99)/100) // Ceiling division for batch count

	// Process groups in batches of 100
	batchSize := 100
	for i := 0; i < len(groups); i += batchSize {
		wg.Add(1)
		go func(start int) {
			defer wg.Done()

			// Get the current batch of groups
			end := start + batchSize
			if end > len(groups) {
				end = len(groups)
			}
			batch := groups[start:end]

			// Create batch request for group icons
			requests := thumbnails.NewBatchThumbnailsBuilder()
			for _, group := range batch {
				requests.AddRequest(types.ThumbnailRequest{
					Type:      types.GroupIconType,
					TargetID:  group.ID,
					RequestID: strconv.FormatUint(group.ID, 10),
					Size:      types.Size420x420,
					Format:    types.PNG,
				})
			}

			// Process the batch
			thumbnailURLs, err := t.processBatchThumbnails(requests)
			resultsChan <- struct {
				BatchStart int
				Result     *ThumbnailFetchResult
			}{
				BatchStart: start,
				Result: &ThumbnailFetchResult{
					URLs:  thumbnailURLs,
					Error: err,
				},
			}
		}(i)
	}

	// Close channel when all goroutines complete
	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	// Collect results from the channel
	results := make(map[uint64]string)
	for result := range resultsChan {
		if result.Result.Error != nil {
			t.logger.Error("Error fetching batch thumbnails",
				zap.Error(result.Result.Error),
				zap.Int("batchStart", result.BatchStart))
			continue
		}

		// Merge URLs from successful batches
		for id, url := range result.Result.URLs {
			results[id] = url
		}
	}

	// Add thumbnail URLs to groups
	updatedGroups := make([]*database.FlaggedGroup, 0, len(groups))
	for _, group := range groups {
		if thumbnailURL, ok := results[group.ID]; ok {
			group.ThumbnailURL = thumbnailURL
			updatedGroups = append(updatedGroups, group)
		}
	}

	t.logger.Info("Finished fetching group thumbnails",
		zap.Int("totalGroups", len(groups)),
		zap.Int("successfulFetches", len(updatedGroups)))

	return updatedGroups
}

// processBatchThumbnails handles a single batch of thumbnail requests.
func (t *ThumbnailFetcher) processBatchThumbnails(requests *thumbnails.BatchThumbnailsBuilder) (map[uint64]string, error) {
	thumbnailURLs := make(map[uint64]string)

	// Send batch request to Roblox API
	thumbnailResponses, err := t.roAPI.Thumbnails().GetBatchThumbnails(context.Background(), requests.Build())
	if err != nil {
		return nil, err
	}

	// Process responses and store URLs
	for _, response := range thumbnailResponses {
		if response.State == types.ThumbnailStateCompleted && response.ImageURL != nil {
			thumbnailURLs[response.TargetID] = *response.ImageURL
		}
	}

	return thumbnailURLs, nil
}
