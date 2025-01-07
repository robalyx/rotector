package handler

import (
	"errors"
	"net/http"

	"github.com/rotector/rotector/internal/common/storage/database"
	"github.com/rotector/rotector/internal/common/storage/database/types"
	"github.com/rotector/rotector/internal/rest/convert"
	restTypes "github.com/rotector/rotector/internal/rest/types"
	"github.com/uptrace/bunrouter"
	"go.uber.org/zap"
)

// UserHandler handles user-related REST endpoints.
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

// GetUser godoc
//
//	@Summary		Get user information
//	@Description	Retrieves detailed information about a user by their ID
//	@Tags			users
//	@Accept			json
//	@Produce		json
//	@Param			id	path		string	true	"User ID"
//	@Success		200	{object}	types.GetUserResponse
//	@Failure		429	{string}	string	"Rate limit exceeded"
//	@Failure		500	{string}	string	"Internal server error"
//	@Security		BearerAuth
//	@Router			/users/{id} [get]
func (h *UserHandler) GetUser(w http.ResponseWriter, req bunrouter.Request) error {
	// Get user from database
	reviewUser, err := h.db.Users().GetUserByID(req.Context(), req.Param("id"), types.UserFields{})
	if err != nil {
		if errors.Is(err, types.ErrUserNotFound) {
			response := restTypes.GetUserResponse{
				Status: restTypes.UserStatusUnflagged,
			}
			return bunrouter.JSON(w, response)
		}
		h.logger.Error("Failed to get user", zap.Error(err))
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return nil
	}

	// If the user is not found, return unflagged status
	if errors.Is(err, types.ErrUserNotFound) {
		return bunrouter.JSON(w, restTypes.GetUserResponse{
			Status: restTypes.UserStatusUnflagged,
		})
	}

	// Convert to REST API type and send response
	response := restTypes.GetUserResponse{
		Status: convert.UserStatus(reviewUser.Status),
		User:   convert.User(reviewUser),
	}

	return bunrouter.JSON(w, response)
}
