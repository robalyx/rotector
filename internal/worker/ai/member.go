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
	GroupUsersToProcess = 200
)

// MemberWorker represents a group worker that processes group members.
type MemberWorker struct {
	db          *database.Database
	roAPI       *api.API
	bar         *progress.Bar
	userFetcher *fetcher.UserFetcher
	userChecker *checker.UserChecker
	logger      *zap.Logger
}

// NewMemberWorker creates a new group worker instance.
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

// Start begins the group worker's main loop.
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
		g.userChecker.ProcessUsers(userInfos)

		// Step 5: Prepare for next batch
		oldUserIDs = userIDs[GroupUsersToProcess:]

		// Short pause before next iteration
		time.Sleep(1 * time.Second)
	}
}

// processGroup handles the processing of a single group.
func (g *MemberWorker) processGroup(groupID uint64, userIDs []uint64) ([]uint64, error) {
	g.logger.Info("Processing group", zap.Uint64("groupID", groupID))

	cursor := ""
	for len(userIDs) < GroupUsersToProcess {
		builder := groups.NewGroupUsersBuilder(groupID).
			WithLimit(100).
			WithCursor(cursor)

		groupUsers, err := g.roAPI.Groups().GetGroupUsers(context.Background(), builder.Build())
		if err != nil {
			g.logger.Error("Error fetching group members", zap.Error(err))
			return nil, err
		}

		// Extract user IDs
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

		if groupUsers.NextPageCursor == nil {
			break
		}
		cursor = *groupUsers.NextPageCursor
	}

	return userIDs, nil
}
