package handler

import (
	"context"
	"errors"

	"github.com/robalyx/rotector/internal/common/storage/database"
	"github.com/robalyx/rotector/internal/common/storage/database/types"
	"github.com/robalyx/rotector/internal/rpc/convert"
	"github.com/robalyx/rotector/internal/rpc/proto"
	"go.uber.org/zap"
)

// UserHandler handles user lookup requests.
type UserHandler struct {
	db     database.Client
	logger *zap.Logger
}

// NewUserHandler creates a new user handler.
func NewUserHandler(db database.Client, logger *zap.Logger) *UserHandler {
	return &UserHandler{
		db:     db,
		logger: logger,
	}
}

// GetUser handles the GetUser RPC method.
func (h *UserHandler) GetUser(ctx context.Context, req *proto.GetUserRequest) (*proto.GetUserResponse, error) {
	// Get full user information from database
	reviewUser, err := h.db.Models().Users().GetUserByID(ctx, req.GetUserId(), types.UserFieldAll)
	if err != nil && !errors.Is(err, types.ErrUserNotFound) {
		h.logger.Error("Failed to get user information",
			zap.String("user_id", req.GetUserId()),
			zap.Error(err))
		return nil, err
	}

	// If the user is not found, return unflagged status
	if errors.Is(err, types.ErrUserNotFound) {
		return &proto.GetUserResponse{
			Status: proto.UserStatus_USER_STATUS_UNFLAGGED,
		}, nil
	}

	// Convert to RPC API type
	return &proto.GetUserResponse{
		Status: convert.UserStatus(reviewUser.Status),
		User:   convert.User(reviewUser),
	}, nil
}
