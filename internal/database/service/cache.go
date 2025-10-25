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

// FilterProcessedUsers filters out user IDs that have been processed within their dynamic processing interval.
func (s *CacheService) FilterProcessedUsers(ctx context.Context, userIDs []int64) ([]int64, error) {
	if len(userIDs) == 0 {
		return userIDs, nil
	}

	unprocessedIDs, err := dbretry.Operation(ctx, func(ctx context.Context) ([]int64, error) {
		// Get processing log entries
		processedEntries, err := s.model.GetProcessingLogs(ctx, userIDs)
		if err != nil {
			return nil, err
		}

		// If no users have been processed yet, return all as unprocessed
		if len(processedEntries) == 0 {
			return userIDs, nil
		}

		// Check which users are still within their processing cooldown
		now := time.Now()
		processedMap := make(map[int64]bool, len(processedEntries))

		for _, entry := range processedEntries {
			if now.Before(entry.NextScanTime) {
				processedMap[entry.UserID] = true
			}
		}

		// Filter out user IDs that are within their processing interval
		unprocessed := make([]int64, 0, len(userIDs))
		for _, userID := range userIDs {
			if !processedMap[userID] {
				unprocessed = append(unprocessed, userID)
			}
		}

		return unprocessed, nil
	})
	if err != nil {
		return nil, err
	}

	cacheHits := len(userIDs) - len(unprocessedIDs)

	s.logger.Info("Filtered processed user IDs with dynamic intervals",
		zap.Int("totalUserIDs", len(userIDs)),
		zap.Int("unprocessedUserIDs", len(unprocessedIDs)),
		zap.Int("cacheHits", cacheHits),
		zap.Float64("cacheHitRate", float64(cacheHits)/float64(len(userIDs))*100))

	return unprocessedIDs, nil
}

// MarkUsersProcessed marks users as processed with calculated times based on their account age.
func (s *CacheService) MarkUsersProcessed(ctx context.Context, userCreationDates map[int64]time.Time) error {
	if len(userCreationDates) == 0 {
		return nil
	}

	return dbretry.NoResult(ctx, func(ctx context.Context) error {
		now := time.Now()
		entries := make([]*types.UserProcessingLog, 0, len(userCreationDates))

		for userID, createdAt := range userCreationDates {
			interval := utils.CalculateProcessingInterval(createdAt)
			entries = append(entries, &types.UserProcessingLog{
				UserID:        userID,
				LastProcessed: now,
				NextScanTime:  now.Add(interval),
			})
		}

		return s.model.MarkUsersProcessed(ctx, entries)
	})
}
