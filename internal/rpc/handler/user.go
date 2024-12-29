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
	// Get full user information
	reviewUser, err := h.db.Users().GetUserByID(ctx, req.GetUserId(), types.UserFields{}, true)
	if err != nil {
		h.logger.Error("Failed to get user information", zap.Error(err))
		return nil, err
	}

	// Convert user status
	var status user.UserStatus
	switch reviewUser.Status {
	case types.UserTypeFlagged:
		status = user.UserStatus_USER_STATUS_FLAGGED
	case types.UserTypeConfirmed:
		status = user.UserStatus_USER_STATUS_CONFIRMED
	case types.UserTypeCleared:
		status = user.UserStatus_USER_STATUS_CLEARED
	case types.UserTypeBanned:
		status = user.UserStatus_USER_STATUS_BANNED
	case types.UserTypeUnflagged:
		status = user.UserStatus_USER_STATUS_UNFLAGGED
	}

	// If the user is unflagged, return immediately
	if status == user.UserStatus_USER_STATUS_UNFLAGGED {
		return &user.GetUserResponse{
			Status: user.UserStatus_USER_STATUS_UNFLAGGED,
		}, nil
	}

	// Convert user data to protobuf message
	protoUser := &user.User{
		Id:             reviewUser.ID,
		Name:           reviewUser.Name,
		DisplayName:    reviewUser.DisplayName,
		Description:    reviewUser.Description,
		CreatedAt:      reviewUser.CreatedAt.Format(time.RFC3339),
		Reason:         reviewUser.Reason,
		FlaggedContent: reviewUser.FlaggedContent,
		FollowerCount:  reviewUser.FollowerCount,
		FollowingCount: reviewUser.FollowingCount,
		Confidence:     reviewUser.Confidence,
		LastScanned:    reviewUser.LastScanned.Format(time.RFC3339),
		LastUpdated:    reviewUser.LastUpdated.Format(time.RFC3339),
		LastViewed:     reviewUser.LastViewed.Format(time.RFC3339),
		ThumbnailUrl:   reviewUser.ThumbnailURL,
		Upvotes:        reviewUser.Reputation.Upvotes,
		Downvotes:      reviewUser.Reputation.Downvotes,
		Reputation:     reviewUser.Reputation.Score,
	}

	// Convert groups
	for _, g := range reviewUser.Groups {
		protoUser.Groups = append(protoUser.Groups, &user.Group{
			Id:   g.Group.ID,
			Name: g.Group.Name,
			Role: g.Role.Name,
		})
	}

	// Convert friends
	for _, f := range reviewUser.Friends {
		protoUser.Friends = append(protoUser.Friends, &user.Friend{
			Id:               f.ID,
			Name:             f.Name,
			DisplayName:      f.DisplayName,
			HasVerifiedBadge: f.HasVerifiedBadge,
		})
	}

	// Convert games
	for _, g := range reviewUser.Games {
		protoUser.Games = append(protoUser.Games, &user.Game{
			Id:   g.ID,
			Name: g.Name,
		})
	}

	return &user.GetUserResponse{
		Status: status,
		User:   protoUser,
	}, nil
}
