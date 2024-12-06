package handler

import (
	"context"
	"time"

	"github.com/rotector/rotector/internal/common/storage/database"
	"github.com/rotector/rotector/internal/common/storage/database/types"
	"github.com/rotector/rotector/rpc/user"
	"go.uber.org/zap"
)

// UserHandler handles user lookup requests.
type UserHandler struct {
	db     *database.Client
	logger *zap.Logger
}

// NewUserHandler creates a new user handler.
func NewUserHandler(db *database.Client, logger *zap.Logger) *UserHandler {
	return &UserHandler{
		db:     db,
		logger: logger,
	}
}

// GetUser handles the GetUser RPC method.
func (h *UserHandler) GetUser(ctx context.Context, req *user.GetUserRequest) (*user.GetUserResponse, error) {
	// Get full user information (includes type information)
	users, userTypes, err := h.db.Users().GetUsersByIDs(ctx, []uint64{req.GetUserId()}, types.DefaultUserFields())
	if err != nil {
		h.logger.Error("Failed to get user information", zap.Error(err))
		return nil, err
	}

	userData := users[req.GetUserId()]
	if userData == nil {
		return &user.GetUserResponse{
			Exists: false,
		}, nil
	}

	// Convert user status
	status := user.UserStatus_USER_STATUS_UNKNOWN
	switch userTypes[req.GetUserId()] {
	case types.UserTypeFlagged:
		status = user.UserStatus_USER_STATUS_FLAGGED
	case types.UserTypeConfirmed:
		status = user.UserStatus_USER_STATUS_CONFIRMED
	case types.UserTypeCleared:
		status = user.UserStatus_USER_STATUS_CLEARED
	case types.UserTypeBanned:
		status = user.UserStatus_USER_STATUS_BANNED
	}

	// Convert user data to protobuf message
	protoUser := &user.User{
		Id:             userData.ID,
		Name:           userData.Name,
		DisplayName:    userData.DisplayName,
		Description:    userData.Description,
		CreatedAt:      userData.CreatedAt.Format(time.RFC3339),
		Reason:         userData.Reason,
		FlaggedContent: userData.FlaggedContent,
		FlaggedGroups:  userData.FlaggedGroups,
		FollowerCount:  userData.FollowerCount,
		FollowingCount: userData.FollowingCount,
		Confidence:     userData.Confidence,
		LastScanned:    userData.LastScanned.Format(time.RFC3339),
		LastUpdated:    userData.LastUpdated.Format(time.RFC3339),
		LastViewed:     userData.LastViewed.Format(time.RFC3339),
		ThumbnailUrl:   userData.ThumbnailURL,
		Upvotes:        userData.Upvotes,
		Downvotes:      userData.Downvotes,
		Reputation:     userData.Reputation,
		Status:         status,
	}

	// Convert groups
	for _, g := range userData.Groups {
		protoUser.Groups = append(protoUser.Groups, &user.Group{
			Id:   g.Group.ID,
			Name: g.Group.Name,
			Role: g.Role.Name,
		})
	}

	// Convert friends
	for _, f := range userData.Friends {
		protoUser.Friends = append(protoUser.Friends, &user.Friend{
			Id:               f.ID,
			Name:             f.Name,
			DisplayName:      f.DisplayName,
			HasVerifiedBadge: f.HasVerifiedBadge,
		})
	}

	// Convert games
	for _, g := range userData.Games {
		protoUser.Games = append(protoUser.Games, &user.Game{
			Id:   g.ID,
			Name: g.Name,
		})
	}

	return &user.GetUserResponse{
		Exists: true,
		User:   protoUser,
	}, nil
}
