package checker

import (
	"context"
	"fmt"
	"time"

	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/rotector/rotector/internal/common/database"
	"github.com/rotector/rotector/internal/common/fetcher"
	"go.uber.org/zap"
)

// MinConfirmedUsersForFlag sets the threshold for how many confirmed users
// must be found before flagging a group or user.
const MinConfirmedUsersForFlag = 20

// TrackingChecker handles the analysis of group affiliations.
type TrackingChecker struct {
	db               *database.Database
	roAPI            *api.API
	userFetcher      *fetcher.UserFetcher
	groupFetcher     *fetcher.GroupFetcher
	thumbnailFetcher *fetcher.ThumbnailFetcher
	logger           *zap.Logger
}

// NewTrackingChecker creates a TrackingChecker with all required fetchers
// for loading user and group information.
func NewTrackingChecker(db *database.Database, roAPI *api.API, thumbnailFetcher *fetcher.ThumbnailFetcher, logger *zap.Logger) *TrackingChecker {
	return &TrackingChecker{
		db:               db,
		roAPI:            roAPI,
		userFetcher:      fetcher.NewUserFetcher(roAPI, logger),
		groupFetcher:     fetcher.NewGroupFetcher(roAPI, logger),
		thumbnailFetcher: thumbnailFetcher,
		logger:           logger,
	}
}

// CheckGroupTrackings analyzes group member lists to find groups with many
// confirmed users. Groups exceeding the threshold are flagged with a confidence
// score based on the ratio of confirmed members.
func (c *TrackingChecker) CheckGroupTrackings() error {
	// Find groups with enough confirmed members
	groups, err := c.db.Tracking().GetAndRemoveQualifiedGroupTrackings(context.Background(), MinConfirmedUsersForFlag)
	if err != nil {
		c.logger.Error("Error getting and removing qualified groups", zap.Error(err))
		return err
	}

	// Extract group IDs for batch lookup
	groupIDs := make([]uint64, 0, len(groups))
	for groupID := range groups {
		groupIDs = append(groupIDs, groupID)
	}

	// Load group information from API
	groupInfos := c.groupFetcher.FetchGroupInfos(groupIDs)
	if len(groupInfos) == 0 {
		return nil
	}

	// Create flagged group entries with confidence scores
	flaggedGroups := make([]*database.FlaggedGroup, 0, len(groupInfos))
	for _, groupInfo := range groupInfos {
		flaggedGroups = append(flaggedGroups, &database.FlaggedGroup{
			ID:          groupInfo.ID,
			Name:        groupInfo.Name,
			Description: groupInfo.Description,
			Owner:       groupInfo.Owner.UserID,
			Reason:      fmt.Sprintf("Group has %d confirmed users", groups[groupInfo.ID]),
			Confidence:  float64(groups[groupInfo.ID]) / float64(groupInfo.MemberCount),
			LastUpdated: time.Now(),
		})
	}

	// Add thumbnails and save to database
	flaggedGroups = c.thumbnailFetcher.AddGroupImageURLs(flaggedGroups)
	c.db.Groups().SaveFlaggedGroups(context.Background(), flaggedGroups)

	c.logger.Info("Checked group trackings", zap.Int("flagged_groups", len(flaggedGroups)))
	return nil
}
