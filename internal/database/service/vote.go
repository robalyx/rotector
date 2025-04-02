package service

import (
	"context"
	"fmt"
	"time"

	"github.com/robalyx/rotector/internal/database/models"
	"github.com/robalyx/rotector/internal/database/types"
	"github.com/robalyx/rotector/internal/database/types/enum"
	"go.uber.org/zap"
)

// VoteService handles vote-related business logic.
type VoteService struct {
	model    *models.VoteModel
	activity *models.ActivityModel
	views    *ViewService
	ban      *models.BanModel
	logger   *zap.Logger
}

// NewVote creates a new vote service.
func NewVote(
	model *models.VoteModel,
	activity *models.ActivityModel,
	views *ViewService,
	ban *models.BanModel,
	logger *zap.Logger,
) *VoteService {
	return &VoteService{
		model:    model,
		activity: activity,
		views:    views,
		ban:      ban,
		logger:   logger.Named("vote_service"),
	}
}

// GetUserVoteStats retrieves vote statistics for a Discord user.
func (s *VoteService) GetUserVoteStats(
	ctx context.Context, discordUserID uint64, period enum.LeaderboardPeriod,
) (*types.VoteAccuracy, error) {
	// Try to refresh the materialized view if stale
	err := s.views.RefreshLeaderboardView(ctx, period)
	if err != nil {
		s.logger.Warn("Failed to refresh materialized view",
			zap.Error(err),
			zap.String("period", period.String()))
		// Continue anyway - we'll use slightly stale data
	}

	return s.model.GetUserVoteStats(ctx, discordUserID, period)
}

// GetLeaderboard retrieves the top voters for a given time period.
func (s *VoteService) GetLeaderboard(
	ctx context.Context, period enum.LeaderboardPeriod, cursor *types.LeaderboardCursor, limit int,
) ([]*types.VoteAccuracy, *types.LeaderboardCursor, error) {
	// Try to refresh the materialized view if stale
	err := s.views.RefreshLeaderboardView(ctx, period)
	if err != nil {
		s.logger.Warn("Failed to refresh materialized view",
			zap.Error(err),
			zap.String("period", period.String()))
		// Continue anyway - we'll use slightly stale data
	}

	return s.model.GetLeaderboard(ctx, period, cursor, limit)
}

// CheckVoteAccuracy checks if a user should be banned based on their voting accuracy.
// Returns true if the user is banned, false otherwise.
func (s *VoteService) CheckVoteAccuracy(ctx context.Context, discordUserID uint64) (bool, error) {
	// Get user's vote stats for all time
	stats, err := s.GetUserVoteStats(ctx, discordUserID, enum.LeaderboardPeriodAllTime)
	if err != nil {
		return false, fmt.Errorf("failed to get vote stats: %w", err)
	}

	// Check if user has enough votes to be evaluated
	const minVotesRequired = 10 // Minimum votes before checking accuracy
	if stats.TotalVotes < minVotesRequired {
		return false, nil
	}

	// Check if accuracy is below threshold
	const minAccuracyRequired = 0.40 // 40% minimum accuracy required
	shouldBan := stats.Accuracy < minAccuracyRequired
	if !shouldBan {
		return false, nil
	}

	// Create ban record
	ban := &types.DiscordBan{
		ID:       discordUserID,
		Reason:   enum.BanReasonAbuse,
		Source:   enum.BanSourceSystem,
		Notes:    "Automated system detection - suspicious voting patterns",
		BannedBy: 0, // System ban
		BannedAt: time.Now(),
	}

	err = s.ban.BanUser(ctx, ban)
	if err != nil {
		return false, fmt.Errorf("failed to create ban record: %w", err)
	}

	// Log the ban action
	go s.activity.Log(ctx, &types.ActivityLog{
		ActivityTarget: types.ActivityTarget{
			DiscordID: discordUserID,
		},
		ReviewerID:        0,
		ActivityType:      enum.ActivityTypeDiscordUserBanned,
		ActivityTimestamp: time.Now(),
		Details: map[string]any{
			"notes":      "Automated system detection - suspicious voting patterns",
			"accuracy":   stats.Accuracy,
			"totalVotes": stats.TotalVotes,
		},
	})

	s.logger.Info("User banned for low vote accuracy",
		zap.Uint64("discordUserID", discordUserID),
		zap.Float64("accuracy", stats.Accuracy),
		zap.Int64("totalVotes", stats.TotalVotes))

	return true, nil
}
