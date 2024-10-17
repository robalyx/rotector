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

// ThumbnailFetcher handles fetching of user thumbnails.
type ThumbnailFetcher struct {
	roAPI  *api.API
	logger *zap.Logger
}

// NewThumbnailFetcher creates a new ThumbnailFetcher instance.
func NewThumbnailFetcher(roAPI *api.API, logger *zap.Logger) *ThumbnailFetcher {
	return &ThumbnailFetcher{
		roAPI:  roAPI,
		logger: logger,
	}
}

// AddImageURLs fetches thumbnails for a batch of users and adds them to the users.
func (t *ThumbnailFetcher) AddImageURLs(users []*database.User) []*database.User {
	thumbnailURLs := make(map[uint64]string)

	// Fetch thumbnails in batches of 100
	batchSize := 100
	for i := 0; i < len(users); i += batchSize {
		// Get the batch of users
		end := i + batchSize
		if end > len(users) {
			end = len(users)
		}

		batch := users[i:end]

		// Create a new batch request
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

		// Fetch the batch thumbnails
		thumbnailResponses, err := t.roAPI.Thumbnails().GetBatchThumbnails(context.Background(), requests.Build())
		if err != nil {
			t.logger.Error("Error fetching batch thumbnails", zap.Error(err))
			continue
		}

		// Process the thumbnail responses
		for _, response := range thumbnailResponses {
			if response.State == "Completed" && response.ImageURL != nil {
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
