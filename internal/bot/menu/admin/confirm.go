package admin

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/snowflake/v2"
	builder "github.com/robalyx/rotector/internal/bot/builder/admin"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/pagination"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/bot/interfaces"
	"github.com/robalyx/rotector/internal/common/storage/database/types"
	"github.com/robalyx/rotector/internal/common/storage/database/types/enum"
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
		Name: "Action Confirmation",
		Message: func(s *session.Session) *discord.MessageUpdateBuilder {
			return builder.NewConfirmBuilder(s).Build()
		},
		ButtonHandlerFunc: m.handleButton,
	}
	return m
}

// Show prepares and displays the confirmation interface.
func (m *ConfirmMenu) Show(event interfaces.CommonEvent, s *session.Session, action string, content string) {
	session.AdminAction.Set(s, action)
	m.layout.paginationManager.NavigateTo(event, s, m.page, content)
}

// handleButton processes button interactions.
func (m *ConfirmMenu) handleButton(event *events.ComponentInteractionCreate, s *session.Session, customID string) {
	switch customID {
	case constants.BackButtonCustomID:
		m.layout.paginationManager.NavigateBack(event, s, "")
	case constants.ActionButtonCustomID:
		m.handleConfirm(event, s)
	}
}

// handleConfirm processes the confirmation action.
func (m *ConfirmMenu) handleConfirm(event *events.ComponentInteractionCreate, s *session.Session) {
	action := session.AdminAction.Get(s)
	id := session.AdminActionID.Get(s)
	reason := session.AdminReason.Get(s)

	switch action {
	case constants.BanUserAction:
		m.handleBanUser(event, s, id, reason)
	case constants.UnbanUserAction:
		m.handleUnbanUser(event, s, id, reason)
	case constants.DeleteUserAction:
		m.handleDeleteUser(event, s, id, reason)
	case constants.DeleteGroupAction:
		m.handleDeleteGroup(event, s, id, reason)
	}
}

// handleBanUser processes the user ban action.
func (m *ConfirmMenu) handleBanUser(event *events.ComponentInteractionCreate, s *session.Session, userID string, notes string) {
	// Parse user ID
	id, err := strconv.ParseUint(userID, 10, 64)
	if err != nil {
		m.layout.paginationManager.RespondWithError(event, "Invalid user ID format.")
		return
	}

	banReason := session.BanReason.Get(s)

	// Ban the user
	now := time.Now()
	expiresAt := session.BanExpiry.Get(s)
	if err := m.layout.db.Bans().BanUser(context.Background(), &types.DiscordBan{
		ID:        snowflake.ID(id),
		Reason:    banReason,
		Source:    enum.BanSourceAdmin,
		Notes:     notes,
		BannedBy:  uint64(event.User().ID),
		BannedAt:  now,
		ExpiresAt: expiresAt,
		UpdatedAt: now,
	}); err != nil {
		m.layout.logger.Error("Failed to ban user",
			zap.Error(err),
			zap.Uint64("user_id", id),
			zap.Uint64("admin_id", uint64(event.User().ID)),
		)
		m.layout.paginationManager.RespondWithError(event, "Failed to ban user. Please try again.")
		return
	}

	// Log the ban action
	go m.layout.db.Activity().Log(context.Background(), &types.ActivityLog{
		ActivityTarget: types.ActivityTarget{
			DiscordID: id,
		},
		ReviewerID:        uint64(event.User().ID),
		ActivityType:      enum.ActivityTypeDiscordUserBanned,
		ActivityTimestamp: time.Now(),
		Details: map[string]interface{}{
			"notes":      notes,
			"ban_reason": banReason.String(),
			"expires_at": expiresAt,
		},
	})

	m.layout.paginationManager.NavigateBack(event, s, fmt.Sprintf("Successfully banned user %d.", id))
}

// handleUnbanUser processes the user unban action.
func (m *ConfirmMenu) handleUnbanUser(event *events.ComponentInteractionCreate, s *session.Session, userID string, notes string) {
	// Parse user ID
	id, err := strconv.ParseUint(userID, 10, 64)
	if err != nil {
		m.layout.paginationManager.RespondWithError(event, "Invalid user ID format.")
		return
	}

	// Unban the user
	unbanned, err := m.layout.db.Bans().UnbanUser(context.Background(), id)
	if err != nil {
		m.layout.logger.Error("Failed to unban user",
			zap.Error(err),
			zap.Uint64("user_id", id),
			zap.Uint64("admin_id", uint64(event.User().ID)),
		)
		m.layout.paginationManager.RespondWithError(event, "Failed to unban user. Please try again.")
		return
	}

	// Check if the user was actually banned
	if !unbanned {
		m.layout.paginationManager.NavigateBack(event, s, "User is not currently banned.")
		return
	}

	// Log the unban action
	go m.layout.db.Activity().Log(context.Background(), &types.ActivityLog{
		ActivityTarget: types.ActivityTarget{
			DiscordID: id,
		},
		ReviewerID:        uint64(event.User().ID),
		ActivityType:      enum.ActivityTypeDiscordUserUnbanned,
		ActivityTimestamp: time.Now(),
		Details: map[string]interface{}{
			"notes": notes,
		},
	})

	m.layout.paginationManager.NavigateBack(event, s, fmt.Sprintf("Successfully unbanned user %d.", id))
}

// handleDeleteUser processes the user deletion action.
func (m *ConfirmMenu) handleDeleteUser(event *events.ComponentInteractionCreate, s *session.Session, idStr string, reason string) {
	// Parse ID from modal
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		m.layout.paginationManager.RespondWithError(event, "Invalid ID format.")
		return
	}

	// Delete user
	found, err := m.layout.db.Users().DeleteUser(context.Background(), id)
	if err != nil {
		m.layout.logger.Error("Failed to delete user",
			zap.Error(err),
			zap.Uint64("id", id))
		m.layout.paginationManager.RespondWithError(event, "Failed to delete user. Please try again.")
		return
	}

	// Check if the ID was found in the database
	if !found {
		m.layout.paginationManager.NavigateBack(event, s, "User ID not found in the database.")
		return
	}

	// Log the deletion
	go m.layout.db.Activity().Log(context.Background(), &types.ActivityLog{
		ActivityTarget: types.ActivityTarget{
			UserID: id,
		},
		ReviewerID:        uint64(event.User().ID),
		ActivityType:      enum.ActivityTypeUserDeleted,
		ActivityTimestamp: time.Now(),
		Details: map[string]interface{}{
			"reason": reason,
		},
	})

	m.layout.paginationManager.NavigateBack(event, s, fmt.Sprintf("Successfully deleted user %d.", id))
}

// handleDeleteGroup processes the group deletion action.
func (m *ConfirmMenu) handleDeleteGroup(event *events.ComponentInteractionCreate, s *session.Session, idStr string, reason string) {
	// Parse ID from modal
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		m.layout.paginationManager.RespondWithError(event, "Invalid ID format.")
		return
	}

	// Delete group
	found, err := m.layout.db.Groups().DeleteGroup(context.Background(), id)
	if err != nil {
		m.layout.logger.Error("Failed to delete group",
			zap.Error(err),
			zap.Uint64("id", id))
		m.layout.paginationManager.RespondWithError(event, "Failed to delete group. Please try again.")
		return
	}

	// Check if the ID was found in the database
	if !found {
		m.layout.paginationManager.NavigateBack(event, s, "Group ID not found in the database.")
		return
	}

	// Log the deletion
	go m.layout.db.Activity().Log(context.Background(), &types.ActivityLog{
		ActivityTarget: types.ActivityTarget{
			GroupID: id,
		},
		ReviewerID:        uint64(event.User().ID),
		ActivityType:      enum.ActivityTypeGroupDeleted,
		ActivityTimestamp: time.Now(),
		Details: map[string]interface{}{
			"reason": reason,
		},
	})

	m.layout.paginationManager.NavigateBack(event, s, fmt.Sprintf("Successfully deleted group %d.", id))
}
