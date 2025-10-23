package service

import (
	"context"
	"time"

	"github.com/robalyx/rotector/internal/database/dbretry"
	"github.com/robalyx/rotector/internal/database/models"
	"github.com/robalyx/rotector/internal/database/types"
	"github.com/robalyx/rotector/pkg/utils"
	"github.com/uptrace/bun"
	"go.uber.org/zap"
)

// CacheService handles cache-related business logic.
type CacheService struct {
	db     *bun.DB
	model  *models.CacheModel
	logger *zap.Logger
}

// NewCache creates a new cache service.
func NewCache(
	db *bun.DB,
	model *models.CacheModel,
	logger *zap.Logger,
) *CacheService {
	return &CacheService{
		db:     db,
		model:  model,
		logger: logger.Named("cache_service"),
	}
}

// FilterProcessedUsers filters out users that have been processed within their dynamic processing interval.
func (s *CacheService) FilterProcessedUsers(ctx context.Context, users []*types.ReviewUser) ([]*types.ReviewUser, error) {
	if len(users) == 0 {
		return users, nil
	}

	unprocessedUsers, err := dbretry.Operation(ctx, func(ctx context.Context) ([]*types.ReviewUser, error) {
		// Extract user IDs for query
		userIDs := make([]int64, len(users))

		userMap := make(map[int64]*types.ReviewUser, len(users))
		for i, user := range users {
			userIDs[i] = user.ID
			userMap[user.ID] = user
		}

		// Get processing log entries
		processedEntries, err := s.model.GetProcessingLogs(ctx, userIDs)
		if err != nil {
			s.logger.Warn("Failed to query processed users, returning all as unprocessed",
				zap.Error(err))

			return users, nil
		}

		// If no users have been processed yet, return all as unprocessed
		if len(processedEntries) == 0 {
			return users, nil
		}

		// Calculate which users are still within their processing interval
		now := time.Now()
		processedMap := make(map[int64]bool, len(processedEntries))

		for _, entry := range processedEntries {
			user, exists := userMap[entry.UserID]
			if !exists {
				s.logger.Warn("Processing log entry found for user not in provided list",
					zap.Int64("userID", entry.UserID))

				continue
			}

			// Calculate dynamic interval based on account age
			interval := utils.CalculateProcessingInterval(user.CreatedAt)

			// Check if user is still within their processing interval
			nextAllowedTime := entry.LastProcessed.Add(interval)
			if now.Before(nextAllowedTime) {
				// User was processed recently and is still within cooldown
				processedMap[entry.UserID] = true
			}
		}

		// Filter out users that are within their processing interval
		unprocessed := make([]*types.ReviewUser, 0, len(users))
		for _, user := range users {
			if !processedMap[user.ID] {
				unprocessed = append(unprocessed, user)
			}
		}

		return unprocessed, nil
	})
	if err != nil {
		return nil, err
	}

	cacheHits := len(users) - len(unprocessedUsers)

	s.logger.Info("Filtered processed users with dynamic intervals",
		zap.Int("totalUsers", len(users)),
		zap.Int("unprocessedUsers", len(unprocessedUsers)),
		zap.Int("cacheHits", cacheHits),
		zap.Float64("cacheHitRate", float64(cacheHits)/float64(len(users))*100))

	return unprocessedUsers, nil
}
