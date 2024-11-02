package checker

import (
	"fmt"
	"time"

	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/rotector/rotector/internal/common/database"
	"github.com/rotector/rotector/internal/common/fetcher"
	"go.uber.org/zap"
)

const (
	MinConfirmedUsersForFlag = 20
)

// TrackingChecker handles checking of tracking data for flagging groups and users.
type TrackingChecker struct {
	db               *database.Database
	roAPI            *api.API
	userFetcher      *fetcher.UserFetcher
	groupFetcher     *fetcher.GroupFetcher
	thumbnailFetcher *fetcher.ThumbnailFetcher
	logger           *zap.Logger
}

// NewTrackingChecker creates a new TrackingChecker instance.
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

// CheckGroupTrackings checks group member trackings and flags groups with sufficient confirmed users.
func (c *TrackingChecker) CheckGroupTrackings() error {
	// Find and remove groups with sufficient confirmed users
	groups, err := c.db.Tracking().GetAndRemoveQualifiedGroupTrackings(MinConfirmedUsersForFlag)
	if err != nil {
		c.logger.Error("Error getting and removing qualified groups", zap.Error(err))
		return err
	}

	// Extract group IDs
	groupIDs := make([]uint64, 0, len(groups))
	for groupID := range groups {
		groupIDs = append(groupIDs, groupID)
	}

	// Fetch group information
	groupInfos := c.groupFetcher.FetchGroupInfos(groupIDs)
	if len(groupInfos) == 0 {
		return nil
	}

	// Process each group
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

	// Add thumbnail URLs
	flaggedGroups = c.thumbnailFetcher.AddGroupImageURLs(flaggedGroups)

	// Save flagged groups
	c.db.Groups().SaveFlaggedGroups(flaggedGroups)

	c.logger.Info("Checked group trackings", zap.Int("flagged_groups", len(flaggedGroups)))
	return nil
}

// CheckUserTrackings checks user network trackings and flags users with sufficient confirmed users.
func (c *TrackingChecker) CheckUserTrackings() error {
	// Find and remove users with sufficient confirmed users
	users, err := c.db.Tracking().GetAndRemoveQualifiedUserTrackings(MinConfirmedUsersForFlag)
	if err != nil {
		return fmt.Errorf("failed to query and remove qualified user network trackings: %w", err)
	}

	// Extract user IDs
	userIDs := make([]uint64, 0, len(users))
	for userID := range users {
		userIDs = append(userIDs, userID)
	}

	// Fetch user information
	userInfos := c.userFetcher.FetchInfos(userIDs)
	if len(userInfos) == 0 {
		c.logger.Info("No user infos found", zap.Int("user_ids", len(userIDs)))
		return nil
	}

	// Process each user
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

	// Add thumbnail URLs
	flaggedUsers = c.thumbnailFetcher.AddImageURLs(flaggedUsers)

	// Save flagged users
	c.db.Users().SaveFlaggedUsers(flaggedUsers)

	c.logger.Info("Checked user trackings", zap.Int("flagged_users", len(flaggedUsers)))
	return nil
}
