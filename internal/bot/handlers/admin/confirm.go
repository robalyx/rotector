package admin

import (
	"fmt"
	"strconv"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/interaction"
	"github.com/robalyx/rotector/internal/bot/core/session"
	view "github.com/robalyx/rotector/internal/bot/views/admin"
	"github.com/robalyx/rotector/internal/database/types"
	"github.com/robalyx/rotector/internal/database/types/enum"
	"go.uber.org/zap"
)

// ConfirmMenu handles the confirmation interface for admin actions.
type ConfirmMenu struct {
	layout *Layout
	page   *interaction.Page
}

// NewConfirmMenu creates a ConfirmMenu and sets up its page.
func NewConfirmMenu(layout *Layout) *ConfirmMenu {
	m := &ConfirmMenu{layout: layout}
	m.page = &interaction.Page{
		Name: constants.AdminActionConfirmPageName,
		Message: func(s *session.Session) *discord.MessageUpdateBuilder {
			return view.NewConfirmBuilder(s).Build()
		},
		ButtonHandlerFunc: m.handleButton,
	}

	return m
}

// handleButton processes button interactions.
func (m *ConfirmMenu) handleButton(ctx *interaction.Context, s *session.Session, customID string) {
	switch customID {
	case constants.BackButtonCustomID:
		ctx.NavigateBack("")
	case constants.ActionButtonCustomID:
		m.handleConfirm(ctx, s)
	}
}

// handleConfirm processes the confirmation action.
func (m *ConfirmMenu) handleConfirm(ctx *interaction.Context, s *session.Session) {
	action := session.AdminAction.Get(s)
	id := session.AdminActionID.Get(s)
	reason := session.AdminReason.Get(s)

	switch action {
	case constants.DeleteUserAction:
		m.handleDeleteUser(ctx, id, reason)
	case constants.DeleteGroupAction:
		m.handleDeleteGroup(ctx, id, reason)
	}
}

// handleDeleteUser processes the user deletion action.
func (m *ConfirmMenu) handleDeleteUser(ctx *interaction.Context, idStr string, reason string) {
	// Parse ID from modal
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		ctx.Cancel("Invalid ID format.")
		return
	}

	if id <= 0 {
		ctx.Cancel("Invalid ID format.")
		return
	}

	// Delete user
	found, err := m.layout.db.Service().User().DeleteUser(ctx.Context(), id)
	if err != nil {
		m.layout.logger.Error("Failed to delete user",
			zap.Error(err),
			zap.Int64("id", id))
		ctx.Error("Failed to delete user. Please try again.")

		return
	}

	// Check if the ID was found in the database
	if !found {
		ctx.NavigateBack("User ID not found in the database.")
		return
	}

	// Remove from D1 database
	if err := m.layout.cfClient.UserFlags.Remove(ctx.Context(), id); err != nil {
		m.layout.logger.Warn("Failed to remove user from D1 database",
			zap.Error(err),
			zap.Int64("userID", id))
	}

	ctx.NavigateBack(fmt.Sprintf("Successfully deleted user %d.", id))

	// Log the deletion
	m.layout.db.Model().Activity().Log(ctx.Context(), &types.ActivityLog{
		ActivityTarget: types.ActivityTarget{
			UserID: id,
		},
		ReviewerID:        uint64(ctx.Event().User().ID),
		ActivityType:      enum.ActivityTypeUserDeleted,
		ActivityTimestamp: time.Now(),
		Details: map[string]any{
			"reason": reason,
		},
	})
}

// handleDeleteGroup processes the group deletion action.
func (m *ConfirmMenu) handleDeleteGroup(ctx *interaction.Context, idStr string, reason string) {
	// Parse ID from modal
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		ctx.Cancel("Invalid ID format.")
		return
	}

	if id <= 0 {
		ctx.Cancel("Invalid ID format.")
		return
	}

	// Delete group
	found, err := m.layout.db.Model().Group().DeleteGroup(ctx.Context(), id)
	if err != nil {
		m.layout.logger.Error("Failed to delete group",
			zap.Error(err),
			zap.Int64("id", id))
		ctx.Error("Failed to delete group. Please try again.")

		return
	}

	// Check if the ID was found in the database
	if !found {
		ctx.NavigateBack("Group ID not found in the database.")
		return
	}

	// Remove from D1 database
	if err := m.layout.cfClient.GroupFlags.Remove(ctx.Context(), id); err != nil {
		m.layout.logger.Warn("Failed to remove group from D1 database",
			zap.Error(err),
			zap.Int64("groupID", id))
	}

	ctx.NavigateBack(fmt.Sprintf("Successfully deleted group %d.", id))

	// Log the deletion
	m.layout.db.Model().Activity().Log(ctx.Context(), &types.ActivityLog{
		ActivityTarget: types.ActivityTarget{
			GroupID: id,
		},
		ReviewerID:        uint64(ctx.Event().User().ID),
		ActivityType:      enum.ActivityTypeGroupDeleted,
		ActivityTimestamp: time.Now(),
		Details: map[string]any{
			"reason": reason,
		},
	})
}
