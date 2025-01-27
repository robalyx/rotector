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

// GroupHandler handles group lookup requests.
type GroupHandler struct {
	db     database.Client
	logger *zap.Logger
}

// NewGroupHandler creates a new group handler.
func NewGroupHandler(db database.Client, logger *zap.Logger) *GroupHandler {
	return &GroupHandler{
		db:     db,
		logger: logger,
	}
}

// GetGroup handles the GetGroup RPC method.
func (h *GroupHandler) GetGroup(ctx context.Context, req *proto.GetGroupRequest) (*proto.GetGroupResponse, error) {
	// Get full group information from database
	reviewGroup, err := h.db.Models().Groups().GetGroupByID(ctx, req.GetGroupId(), types.GroupFields{})
	if err != nil && !errors.Is(err, types.ErrGroupNotFound) {
		h.logger.Error("Failed to get group information",
			zap.String("group_id", req.GetGroupId()),
			zap.Error(err))
		return nil, err
	}

	// If the group is not found, return unflagged status
	if errors.Is(err, types.ErrGroupNotFound) {
		return &proto.GetGroupResponse{
			Status: proto.GroupStatus_GROUP_STATUS_UNFLAGGED,
		}, nil
	}

	// Convert to RPC API type
	return &proto.GetGroupResponse{
		Status: convert.GroupStatus(reviewGroup.Status),
		Group:  convert.Group(reviewGroup),
	}, nil
}
