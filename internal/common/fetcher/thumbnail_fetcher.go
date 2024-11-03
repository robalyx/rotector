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
	var mu sync.Mutex
	thumbnailURLs := make(map[uint64]string)

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
			batchURLs := t.processBatchThumbnails(requests)

			// Merge results with thread-safety
			mu.Lock()
			for id, url := range batchURLs {
				thumbnailURLs[id] = url
			}
			mu.Unlock()
		}(i)
	}

	wg.Wait()

	// Add thumbnail URLs to users
	for i, user := range users {
		if thumbnailURL, ok := thumbnailURLs[user.ID]; ok {
			users[i].ThumbnailURL = thumbnailURL
		}
	}

	t.logger.Info("Finished fetching user thumbnails",
		zap.Int("totalUsers", len(users)),
		zap.Int("successfulFetches", len(thumbnailURLs)))

	return users
}

// AddGroupImageURLs fetches thumbnails for a batch of groups and adds them to the group records.
func (t *ThumbnailFetcher) AddGroupImageURLs(groups []*database.FlaggedGroup) []*database.FlaggedGroup {
	var wg sync.WaitGroup
	var mu sync.Mutex
	thumbnailURLs := make(map[uint64]string)

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
			batchURLs := t.processBatchThumbnails(requests)

			// Merge results with thread-safety
			mu.Lock()
			for id, url := range batchURLs {
				thumbnailURLs[id] = url
			}
			mu.Unlock()
		}(i)
	}

	wg.Wait()

	// Add thumbnail URLs to groups
	for i, group := range groups {
		if thumbnailURL, ok := thumbnailURLs[group.ID]; ok {
			groups[i].ThumbnailURL = thumbnailURL
		}
	}

	t.logger.Info("Finished fetching group thumbnails",
		zap.Int("totalGroups", len(groups)),
		zap.Int("successfulFetches", len(thumbnailURLs)))

	return groups
}

// processBatchThumbnails handles a single batch of thumbnail requests.
func (t *ThumbnailFetcher) processBatchThumbnails(requests *thumbnails.BatchThumbnailsBuilder) map[uint64]string {
	thumbnailURLs := make(map[uint64]string)

	// Send batch request to Roblox API
	thumbnailResponses, err := t.roAPI.Thumbnails().GetBatchThumbnails(context.Background(), requests.Build())
	if err != nil {
		t.logger.Error("Error fetching batch thumbnails", zap.Error(err))
		return thumbnailURLs
	}

	// Process responses and store URLs
	for _, response := range thumbnailResponses {
		if response.State == types.ThumbnailStateCompleted && response.ImageURL != nil {
			thumbnailURLs[response.TargetID] = *response.ImageURL
		}
	}

	return thumbnailURLs
}
