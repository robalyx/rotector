package fetcher

import (
	"context"
	"strconv"

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
	thumbnailURLs := make(map[uint64]string)

	// Process users in batches of 100
	batchSize := 100
	for i := 0; i < len(users); i += batchSize {
		// Get the current batch of users
		end := i + batchSize
		if end > len(users) {
			end = len(users)
		}
		batch := users[i:end]

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

		// Send batch request to Roblox API
		thumbnailResponses, err := t.roAPI.Thumbnails().GetBatchThumbnails(context.Background(), requests.Build())
		if err != nil {
			t.logger.Error("Error fetching batch thumbnails", zap.Error(err))
			continue
		}

		// Process responses and store URLs
		for _, response := range thumbnailResponses {
			if response.State == types.ThumbnailStateCompleted && response.ImageURL != nil {
				thumbnailURLs[response.TargetID] = *response.ImageURL
			}
		}

		t.logger.Info("Fetched batch thumbnails",
			zap.Int("batchSize", len(batch)),
			zap.Int("fetchedThumbnails", len(thumbnailResponses)))
	}

	// Add thumbnail URLs to users
	for i, user := range users {
		if thumbnailURL, ok := thumbnailURLs[user.ID]; ok {
			users[i].ThumbnailURL = thumbnailURL
		}
	}

	return users
}

// AddGroupImageURLs fetches thumbnails for a batch of groups and adds them to the group records.
func (t *ThumbnailFetcher) AddGroupImageURLs(groups []*database.FlaggedGroup) []*database.FlaggedGroup {
	thumbnailURLs := make(map[uint64]string)

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

	// Send batch request to Roblox API
	thumbnailResponses, err := t.roAPI.Thumbnails().GetBatchThumbnails(context.Background(), requests.Build())
	if err != nil {
		t.logger.Error("Error fetching batch group thumbnails", zap.Error(err))
		return groups
	}

	// Process responses and store URLs
	for _, response := range thumbnailResponses {
		if response.State == types.ThumbnailStateCompleted && response.ImageURL != nil {
			thumbnailURLs[response.TargetID] = *response.ImageURL
		}
	}

	// Add thumbnail URLs to groups
	for i, group := range groups {
		if thumbnailURL, ok := thumbnailURLs[group.ID]; ok {
			groups[i].ThumbnailURL = thumbnailURL
		}
	}

	return groups
}
