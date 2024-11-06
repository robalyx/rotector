package ai

import (
	"context"
	"time"

	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/jaxron/roapi.go/pkg/api/resources/groups"
	"github.com/openai/openai-go"
	"github.com/rotector/rotector/internal/common/checker"
	"github.com/rotector/rotector/internal/common/database"
	"github.com/rotector/rotector/internal/common/fetcher"
	"github.com/rotector/rotector/internal/common/progress"
	"go.uber.org/zap"
)

const (
	// GroupUsersToProcess sets how many users to process in each batch.
	GroupUsersToProcess = 100
)

// MemberWorker processes group member lists by checking each member's
// status and analyzing their profiles for inappropriate content.
type MemberWorker struct {
	db          *database.Database
	roAPI       *api.API
	bar         *progress.Bar
	userFetcher *fetcher.UserFetcher
	userChecker *checker.UserChecker
	logger      *zap.Logger
}

// NewMemberWorker creates a MemberWorker.
func NewMemberWorker(db *database.Database, openaiClient *openai.Client, roAPI *api.API, bar *progress.Bar, logger *zap.Logger) *MemberWorker {
	userFetcher := fetcher.NewUserFetcher(roAPI, logger)
	userChecker := checker.NewUserChecker(db, bar, roAPI, openaiClient, userFetcher, logger)

	return &MemberWorker{
		db:          db,
		roAPI:       roAPI,
		bar:         bar,
		userFetcher: userFetcher,
		userChecker: userChecker,
		logger:      logger,
	}
}

// Start begins the group worker's main loop:
// 1. Gets a confirmed group to process
// 2. Fetches member lists in batches
// 3. Checks members for inappropriate content
// 4. Repeats until stopped.
func (g *MemberWorker) Start() {
	g.logger.Info("Group Worker started")
	g.bar.SetTotal(100)

	var oldUserIDs []uint64
	for {
		g.bar.Reset()

		// Step 1: Get next confirmed group (10%)
		g.bar.SetStepMessage("Fetching next confirmed group")
		group, err := g.db.Groups().GetNextConfirmedGroup()
		if err != nil {
			g.logger.Error("Error getting next confirmed group", zap.Error(err))
			time.Sleep(5 * time.Minute) // Wait before trying again
			continue
		}
		g.bar.Increment(10)

		// Step 2: Get group users (15%)
		g.bar.SetStepMessage("Processing group users")
		userIDs, err := g.processGroup(group.ID, oldUserIDs)
		if err != nil {
			g.logger.Error("Error processing group", zap.Error(err), zap.Uint64("groupID", group.ID))
			time.Sleep(5 * time.Minute) // Wait before trying again
			continue
		}
		g.bar.Increment(15)

		// Step 3: Fetch user info (15%)
		g.bar.SetStepMessage("Fetching user info")
		userInfos := g.userFetcher.FetchInfos(userIDs[:GroupUsersToProcess])
		g.bar.Increment(15)

		// Step 4: Process users (60%)
		failedValidationIDs := g.userChecker.ProcessUsers(userInfos)

		// Step 5: Prepare for next batch
		oldUserIDs = userIDs[GroupUsersToProcess:]

		// Add failed validation IDs back to the queue for retry
		if len(failedValidationIDs) > 0 {
			oldUserIDs = append(oldUserIDs, failedValidationIDs...)
			g.logger.Info("Added failed validation IDs for retry",
				zap.Int("failedCount", len(failedValidationIDs)))
		}

		// Short pause before next iteration
		time.Sleep(1 * time.Second)
	}
}

// processGroup builds a list of member IDs to check by:
// 1. Fetching member lists in batches using cursor pagination
// 2. Filtering out already processed users
// 3. Collecting enough IDs to fill a batch.
func (g *MemberWorker) processGroup(groupID uint64, userIDs []uint64) ([]uint64, error) {
	g.logger.Info("Processing group", zap.Uint64("groupID", groupID))

	cursor := ""
	for len(userIDs) < GroupUsersToProcess {
		// Fetch group users with cursor pagination
		builder := groups.NewGroupUsersBuilder(groupID).WithLimit(100).WithCursor(cursor)
		groupUsers, err := g.roAPI.Groups().GetGroupUsers(context.Background(), builder.Build())
		if err != nil {
			g.logger.Error("Error fetching group members", zap.Error(err))
			return nil, err
		}

		// If the group has no users, skip it
		if len(groupUsers.Data) == 0 {
			break
		}

		// Extract user IDs from member list
		newUserIDs := make([]uint64, len(groupUsers.Data))
		for i, groupUser := range groupUsers.Data {
			newUserIDs[i] = groupUser.User.UserID
		}

		// Check which users already exist in the database
		existingUsers, err := g.db.Users().CheckExistingUsers(newUserIDs)
		if err != nil {
			g.logger.Error("Error checking existing users", zap.Error(err))
			continue
		}

		// Add only new users to the userIDs slice
		for _, userID := range newUserIDs {
			if _, exists := existingUsers[userID]; !exists {
				userIDs = append(userIDs, userID)
			}
		}

		g.logger.Info("Fetched group users",
			zap.Uint64("groupID", groupID),
			zap.String("cursor", cursor),
			zap.Int("totalUsers", len(groupUsers.Data)),
			zap.Int("newUsers", len(newUserIDs)-len(existingUsers)),
			zap.Int("userIDs", len(userIDs)))

		// Move to next page if available
		if groupUsers.NextPageCursor == nil {
			break
		}
		cursor = *groupUsers.NextPageCursor
	}

	return userIDs, nil
}
