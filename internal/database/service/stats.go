package service

import (
	"context"
	"fmt"
	"time"

	"github.com/robalyx/rotector/internal/database/models"
	"github.com/robalyx/rotector/internal/database/types"
	"go.uber.org/zap"
)

// StatsService handles statistics-related business logic.
type StatsService struct {
	model  *models.StatsModel
	user   *models.UserModel
	group  *models.GroupModel
	logger *zap.Logger
}

// NewStats creates a new stats service.
func NewStats(
	model *models.StatsModel,
	user *models.UserModel,
	group *models.GroupModel,
	logger *zap.Logger,
) *StatsService {
	return &StatsService{
		model:  model,
		user:   user,
		group:  group,
		logger: logger.Named("stats_service"),
	}
}

// GetCurrentStats retrieves the current statistics by counting directly from relevant tables.
func (s *StatsService) GetCurrentStats(ctx context.Context) (*types.HourlyStats, error) {
	var stats types.HourlyStats

	stats.Timestamp = time.Now().UTC().Truncate(time.Hour)

	// Get user counts
	userCounts, err := s.user.GetUserCounts(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get user counts: %w", err)
	}

	stats.UsersConfirmed = int64(userCounts.Confirmed)
	stats.UsersFlagged = int64(userCounts.Flagged)
	stats.UsersCleared = int64(userCounts.Cleared)
	stats.UsersBanned = int64(userCounts.Banned)

	// Get group counts
	groupCounts, err := s.group.GetGroupCounts(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get group counts: %w", err)
	}

	stats.GroupsConfirmed = int64(groupCounts.Confirmed)
	stats.GroupsFlagged = int64(groupCounts.Flagged)
	stats.GroupsMixed = int64(groupCounts.Mixed)
	stats.GroupsLocked = int64(groupCounts.Locked)

	return &stats, nil
}

// GetCurrentCounts retrieves all current user and group counts.
func (s *StatsService) GetCurrentCounts(ctx context.Context) (*types.UserCounts, *types.GroupCounts, error) {
	userCounts, err := s.user.GetUserCounts(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get user counts: %w", err)
	}

	groupCounts, err := s.group.GetGroupCounts(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get group counts: %w", err)
	}

	return userCounts, groupCounts, nil
}

// SaveHourlyStats saves the current statistics snapshot.
func (s *StatsService) SaveHourlyStats(ctx context.Context) error {
	// Get current stats
	stats, err := s.GetCurrentStats(ctx)
	if err != nil {
		return fmt.Errorf("failed to get current stats: %w", err)
	}

	// Save stats to database
	if err := s.model.SaveHourlyStats(ctx, stats); err != nil {
		return fmt.Errorf("failed to save hourly stats: %w", err)
	}

	return nil
}
