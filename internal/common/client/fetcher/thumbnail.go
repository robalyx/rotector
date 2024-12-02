package fetcher

import (
	"context"
	"strconv"

	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/jaxron/roapi.go/pkg/api/resources/thumbnails"
	"github.com/jaxron/roapi.go/pkg/api/types"
	"github.com/rotector/rotector/internal/common/storage/database/models"
	"go.uber.org/zap"
)

const ThumbnailPlaceholder = "-"

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
func (t *ThumbnailFetcher) AddImageURLs(users map[uint64]*models.User) map[uint64]*models.User {
	// Create batch request for headshots
	requests := thumbnails.NewBatchThumbnailsBuilder()
	for _, user := range users {
		requests.AddRequest(types.ThumbnailRequest{
			Type:      types.AvatarHeadShotType,
			TargetID:  user.ID,
			RequestID: strconv.FormatUint(user.ID, 10),
			Size:      types.Size420x420,
			Format:    types.PNG,
		})
	}

	// Process thumbnails
	results := t.ProcessBatchThumbnails(requests)

	// Add thumbnail URLs to users
	updatedUsers := make(map[uint64]*models.User, len(users))
	for _, user := range users {
		if thumbnailURL, ok := results[user.ID]; ok {
			user.ThumbnailURL = thumbnailURL
			updatedUsers[user.ID] = user
		}
	}

	t.logger.Debug("Finished fetching user thumbnails",
		zap.Int("totalUsers", len(users)),
		zap.Int("successfulFetches", len(updatedUsers)))

	return updatedUsers
}

// AddGroupImageURLs fetches thumbnails for groups and adds them to the group records.
func (t *ThumbnailFetcher) AddGroupImageURLs(groups []*models.FlaggedGroup) []*models.FlaggedGroup {
	// Create batch request for group icons
	requests := thumbnails.NewBatchThumbnailsBuilder()
	for _, group := range groups {
		requests.AddRequest(types.ThumbnailRequest{
			Type:      types.GroupIconType,
			TargetID:  group.ID,
			RequestID: strconv.FormatUint(group.ID, 10),
			Size:      types.Size420x420,
			Format:    types.PNG,
		})
	}

	// Process thumbnails
	results := t.ProcessBatchThumbnails(requests)

	// Add thumbnail URLs to groups
	updatedGroups := make([]*models.FlaggedGroup, 0, len(groups))
	for _, group := range groups {
		if thumbnailURL, ok := results[group.ID]; ok {
			group.ThumbnailURL = thumbnailURL
			updatedGroups = append(updatedGroups, group)
		}
	}

	t.logger.Debug("Finished fetching group thumbnails",
		zap.Int("totalGroups", len(groups)),
		zap.Int("successfulFetches", len(updatedGroups)))

	return updatedGroups
}

// ProcessBatchThumbnails handles batched thumbnail requests, processing them in groups of 100.
// It returns a map of target IDs to their thumbnail URLs.
func (t *ThumbnailFetcher) ProcessBatchThumbnails(requests *thumbnails.BatchThumbnailsBuilder) map[uint64]string {
	thumbnailURLs := make(map[uint64]string)
	requestList := requests.Build()

	// Process in batches of 100
	batchSize := 100
	for i := 0; i < len(requestList.Requests); i += batchSize {
		end := i + batchSize
		if end > len(requestList.Requests) {
			end = len(requestList.Requests)
		}

		// Create new batch request
		batchRequests := thumbnails.NewBatchThumbnailsBuilder()
		for _, request := range requestList.Requests[i:end] {
			batchRequests.AddRequest(request)
		}

		// Send batch request to Roblox API
		thumbnailResponses, err := t.roAPI.Thumbnails().GetBatchThumbnails(context.Background(), batchRequests.Build())
		if err != nil {
			t.logger.Error("Error fetching batch thumbnails",
				zap.Error(err),
				zap.Int("batchStart", i))
			continue
		}

		// Process responses and store URLs
		for _, response := range thumbnailResponses.Data {
			if response.State == types.ThumbnailStateCompleted && response.ImageURL != nil {
				thumbnailURLs[response.TargetID] = *response.ImageURL
			} else {
				thumbnailURLs[response.TargetID] = ThumbnailPlaceholder
			}
		}
	}

	t.logger.Debug("Processed batch thumbnails",
		zap.Int("totalRequested", len(requestList.Requests)),
		zap.Int("successfulFetches", len(thumbnailURLs)))

	return thumbnailURLs
}
