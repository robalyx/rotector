package admin

import (
	"context"
	"strconv"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	builder "github.com/rotector/rotector/internal/bot/builder/admin"
	"github.com/rotector/rotector/internal/bot/constants"
	"github.com/rotector/rotector/internal/bot/core/pagination"
	"github.com/rotector/rotector/internal/bot/core/session"
	"github.com/rotector/rotector/internal/bot/interfaces"
	"github.com/rotector/rotector/internal/common/storage/database/types"
	"go.uber.org/zap"
)

// ConfirmMenu handles the confirmation interface for admin actions.
type ConfirmMenu struct {
	layout *Layout
	page   *pagination.Page
}

// NewConfirmMenu creates a ConfirmMenu and sets up its page.
func NewConfirmMenu(layout *Layout) *ConfirmMenu {
	m := &ConfirmMenu{layout: layout}
	m.page = &pagination.Page{
		Name: "Delete Confirmation",
		Message: func(s *session.Session) *discord.MessageUpdateBuilder {
			return builder.NewConfirmBuilder(s).Build()
		},
		ButtonHandlerFunc: m.handleButton,
	}
	return m
}

// Show prepares and displays the confirmation interface.
func (m *ConfirmMenu) Show(event interfaces.CommonEvent, s *session.Session, action string, content string) {
	s.Set(constants.SessionKeyDeleteAction, action)
	m.layout.paginationManager.NavigateTo(event, s, m.page, content)
}

// handleButton processes button interactions.
func (m *ConfirmMenu) handleButton(event *events.ComponentInteractionCreate, s *session.Session, customID string) {
	switch customID {
	case constants.BackButtonCustomID:
		m.layout.paginationManager.NavigateBack(event, s, "")
	case constants.DeleteConfirmButtonCustomID:
		m.handleConfirmDelete(event, s)
	}
}

// handleConfirmDelete processes the deletion confirmation.
func (m *ConfirmMenu) handleConfirmDelete(event *events.ComponentInteractionCreate, s *session.Session) {
	action := s.GetString(constants.SessionKeyDeleteAction)
	idStr := s.GetString(constants.SessionKeyDeleteID)
	reason := s.GetString(constants.SessionKeyDeleteReason)

	// Parse ID from modal
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		m.layout.paginationManager.RespondWithError(event, "Invalid ID format.")
		return
	}

	// Delete user or group
	var found bool
	if action == constants.DeleteUserAction {
		found, err = m.layout.db.Users().DeleteUser(context.Background(), id)
	} else {
		found, err = m.layout.db.Groups().DeleteGroup(context.Background(), id)
	}

	if err != nil {
		m.layout.logger.Error("Failed to delete",
			zap.Error(err),
			zap.String("action", action),
			zap.Uint64("id", id))
		m.layout.paginationManager.RespondWithError(event, "Failed to delete. Please try again.")
		return
	}

	// Check if the ID was found in the database
	if !found {
		m.layout.paginationManager.NavigateBack(event, s, "ID not found in the database.")
		return
	}

	// Log the deletion
	if action == constants.DeleteUserAction {
		go m.layout.db.Activity().Log(context.Background(), &types.ActivityLog{
			ActivityTarget: types.ActivityTarget{
				UserID: id,
			},
			ReviewerID:        uint64(event.User().ID),
			ActivityType:      types.ActivityTypeUserDeleted,
			ActivityTimestamp: time.Now(),
			Details: map[string]interface{}{
				"reason": reason,
			},
		})
	} else {
		go m.layout.db.Activity().Log(context.Background(), &types.ActivityLog{
			ActivityTarget: types.ActivityTarget{
				GroupID: id,
			},
			ReviewerID:        uint64(event.User().ID),
			ActivityType:      types.ActivityTypeGroupDeleted,
			ActivityTimestamp: time.Now(),
			Details: map[string]interface{}{
				"reason": reason,
			},
		})
	}

	m.layout.paginationManager.NavigateBack(event, s, "Successfully deleted ID "+idStr)
}
