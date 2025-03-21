package service

import (
	"context"
	"fmt"

	"github.com/robalyx/rotector/internal/common/storage/database/models"
	"go.uber.org/zap"
)

// SyncService handles user sync-related operations.
type SyncService struct {
	sync   *models.SyncModel
	logger *zap.Logger
}

// NewSync creates a new sync service.
func NewSync(sync *models.SyncModel, logger *zap.Logger) *SyncService {
	return &SyncService{
		sync:   sync,
		logger: logger.Named("sync_service"),
	}
}

// ShouldSkipUser checks if a user's data should be skipped due to privacy settings.
// Returns true if the user should be skipped (is redacted or whitelisted).
func (s *SyncService) ShouldSkipUser(
	ctx context.Context, userID uint64,
) (isRedacted, isWhitelisted bool, err error) {
	// Check if user has requested data deletion
	isRedacted, err = s.sync.IsUserDataRedacted(ctx, userID)
	if err != nil {
		return false, false, fmt.Errorf("failed to check data redaction status: %w", err)
	}

	// Check whitelist status
	isWhitelisted, err = s.sync.IsUserWhitelisted(ctx, userID)
	if err != nil {
		return false, false, fmt.Errorf("failed to check whitelist status: %w", err)
	}

	return isRedacted, isWhitelisted, err
}
