package handler

import (
	"errors"
	"net/http"

	"github.com/robalyx/rotector/internal/common/storage/database"
	"github.com/robalyx/rotector/internal/common/storage/database/types"
	"github.com/robalyx/rotector/internal/rest/convert"
	restTypes "github.com/robalyx/rotector/internal/rest/types"
	"github.com/uptrace/bunrouter"
	"go.uber.org/zap"
)

// GroupHandler handles group-related REST endpoints.
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

// GetGroup godoc
//
//	@Summary		Get group information
//	@Description	Retrieves detailed information about a group by their ID
//	@Tags			groups
//	@Accept			json
//	@Produce		json
//	@Param			id	path		string	true	"Group ID"
//	@Success		200	{object}	types.GetGroupResponse
//	@Failure		429	{string}	string	"Rate limit exceeded"
//	@Failure		500	{string}	string	"Internal server error"
//	@Security		BearerAuth
//	@Router			/groups/{id} [get]
func (h *GroupHandler) GetGroup(w http.ResponseWriter, req bunrouter.Request) error {
	// Get group from database
	reviewGroup, err := h.db.Models().Groups().GetGroupByID(req.Context(), req.Param("id"), types.GroupFields{})
	if err != nil {
		if errors.Is(err, types.ErrGroupNotFound) {
			response := restTypes.GetGroupResponse{
				Status: restTypes.GroupStatusUnflagged,
			}
			return bunrouter.JSON(w, response)
		}
		h.logger.Error("Failed to get group", zap.Error(err))
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return nil
	}

	// If the group is not found, return unflagged status
	if errors.Is(err, types.ErrGroupNotFound) {
		return bunrouter.JSON(w, restTypes.GetGroupResponse{
			Status: restTypes.GroupStatusUnflagged,
		})
	}

	// Convert to REST API type and send response
	response := restTypes.GetGroupResponse{
		Status: convert.GroupStatus(reviewGroup.Status),
		Group:  convert.Group(reviewGroup),
	}

	return bunrouter.JSON(w, response)
}
