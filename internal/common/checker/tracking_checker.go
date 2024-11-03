package checker

import (
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

// TrackingChecker handles the analysis of user and group relationships by
// tracking affiliations between confirmed users.
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
	groups, err := c.db.Tracking().GetAndRemoveQualifiedGroupTrackings(MinConfirmedUsersForFlag)
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
	c.db.Groups().SaveFlaggedGroups(flaggedGroups)

	c.logger.Info("Checked group trackings", zap.Int("flagged_groups", len(flaggedGroups)))
	return nil
}

// CheckUserTrackings analyzes user friend networks to find users connected to
// many confirmed users. Users exceeding the threshold are flagged with a confidence
// score based on the ratio of confirmed friends.
func (c *TrackingChecker) CheckUserTrackings() error {
	// Find users with enough confirmed connections
	users, err := c.db.Tracking().GetAndRemoveQualifiedUserTrackings(MinConfirmedUsersForFlag)
	if err != nil {
		return fmt.Errorf("failed to query and remove qualified user network trackings: %w", err)
	}

	// Extract user IDs for batch lookup
	userIDs := make([]uint64, 0, len(users))
	for userID := range users {
		userIDs = append(userIDs, userID)
	}

	// Load user information from API
	userInfos := c.userFetcher.FetchInfos(userIDs)
	if len(userInfos) == 0 {
		return nil
	}

	// Create flagged user entries with confidence scores
	flaggedUsers := make([]*database.User, 0, len(userInfos))
	for _, userInfo := range userInfos {
		flaggedUsers = append(flaggedUsers, &database.User{
			ID:          userInfo.ID,
			Name:        userInfo.Name,
			DisplayName: userInfo.DisplayName,
			Description: userInfo.Description,
			CreatedAt:   userInfo.CreatedAt,
			Reason:      fmt.Sprintf("User has %d confirmed users in their network", users[userInfo.ID]),
			Confidence:  float64(users[userInfo.ID]) / float64(len(userInfo.Friends)),
			LastUpdated: time.Now(),
		})
	}

	// Add thumbnails and save to database
	flaggedUsers = c.thumbnailFetcher.AddImageURLs(flaggedUsers)
	c.db.Users().SaveFlaggedUsers(flaggedUsers)

	c.logger.Info("Checked user trackings", zap.Int("flagged_users", len(flaggedUsers)))
	return nil
}
