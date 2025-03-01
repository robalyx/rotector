package admin

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	builder "github.com/robalyx/rotector/internal/bot/builder/admin"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/pagination"
	"github.com/robalyx/rotector/internal/bot/core/session"
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
		Name: constants.AdminActionConfirmPageName,
		Message: func(s *session.Session) *discord.MessageUpdateBuilder {
			return builder.NewConfirmBuilder(s).Build()
		},
		ButtonHandlerFunc: m.handleButton,
	}
	return m
}

// handleButton processes button interactions.
func (m *ConfirmMenu) handleButton(event *events.ComponentInteractionCreate, s *session.Session, r *pagination.Respond, customID string) {
	switch customID {
	case constants.BackButtonCustomID:
		r.NavigateBack(event, s, "")
	case constants.ActionButtonCustomID:
		m.handleConfirm(event, s, r)
	}
}

// handleConfirm processes the confirmation action.
func (m *ConfirmMenu) handleConfirm(event *events.ComponentInteractionCreate, s *session.Session, r *pagination.Respond) {
	action := session.AdminAction.Get(s)
	id := session.AdminActionID.Get(s)
	reason := session.AdminReason.Get(s)

	switch action {
	case constants.BanUserAction:
		m.handleBanUser(event, s, r, id, reason)
	case constants.UnbanUserAction:
		m.handleUnbanUser(event, s, r, id, reason)
	case constants.DeleteUserAction:
		m.handleDeleteUser(event, s, r, id, reason)
	case constants.DeleteGroupAction:
		m.handleDeleteGroup(event, s, r, id, reason)
	}
}

// handleBanUser processes the user ban action.
func (m *ConfirmMenu) handleBanUser(event *events.ComponentInteractionCreate, s *session.Session, r *pagination.Respond, userID string, notes string) {
	// Parse user ID
	id, err := strconv.ParseUint(userID, 10, 64)
	if err != nil {
		r.Error(event, "Invalid user ID format.")
		return
	}

	banReason := session.AdminBanReason.Get(s)

	// Ban the user
	now := time.Now()
	expiresAt := session.AdminBanExpiry.Get(s)
	if err := m.layout.db.Models().Bans().BanUser(context.Background(), &types.DiscordBan{
		ID:        id,
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
		r.Error(event, "Failed to ban user. Please try again.")
		return
	}

	// Log the ban action
	go m.layout.db.Models().Activities().Log(context.Background(), &types.ActivityLog{
		ActivityTarget: types.ActivityTarget{
			DiscordID: id,
		},
		ReviewerID:        uint64(event.User().ID),
		ActivityType:      enum.ActivityTypeDiscordUserBanned,
		ActivityTimestamp: time.Now(),
		Details: map[string]any{
			"notes":      notes,
			"ban_reason": banReason.String(),
			"expires_at": expiresAt,
		},
	})

	r.NavigateBack(event, s, fmt.Sprintf("Successfully banned user %d.", id))
}

// handleUnbanUser processes the user unban action.
func (m *ConfirmMenu) handleUnbanUser(event *events.ComponentInteractionCreate, s *session.Session, r *pagination.Respond, userID string, notes string) {
	// Parse user ID
	id, err := strconv.ParseUint(userID, 10, 64)
	if err != nil {
		r.Error(event, "Invalid user ID format.")
		return
	}

	// Unban the user
	unbanned, err := m.layout.db.Models().Bans().UnbanUser(context.Background(), id)
	if err != nil {
		m.layout.logger.Error("Failed to unban user",
			zap.Error(err),
			zap.Uint64("user_id", id),
			zap.Uint64("admin_id", uint64(event.User().ID)),
		)
		r.Error(event, "Failed to unban user. Please try again.")
		return
	}

	// Check if the user was actually banned
	if !unbanned {
		r.NavigateBack(event, s, "User is not currently banned.")
		return
	}

	// Log the unban action
	go m.layout.db.Models().Activities().Log(context.Background(), &types.ActivityLog{
		ActivityTarget: types.ActivityTarget{
			DiscordID: id,
		},
		ReviewerID:        uint64(event.User().ID),
		ActivityType:      enum.ActivityTypeDiscordUserUnbanned,
		ActivityTimestamp: time.Now(),
		Details: map[string]any{
			"notes": notes,
		},
	})

	r.NavigateBack(event, s, fmt.Sprintf("Successfully unbanned user %d.", id))
}

// handleDeleteUser processes the user deletion action.
func (m *ConfirmMenu) handleDeleteUser(event *events.ComponentInteractionCreate, s *session.Session, r *pagination.Respond, idStr string, reason string) {
	// Parse ID from modal
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		r.Error(event, "Invalid ID format.")
		return
	}

	// Delete user
	found, err := m.layout.db.Models().Users().DeleteUser(context.Background(), id)
	if err != nil {
		m.layout.logger.Error("Failed to delete user",
			zap.Error(err),
			zap.Uint64("id", id))
		r.Error(event, "Failed to delete user. Please try again.")
		return
	}

	// Check if the ID was found in the database
	if !found {
		r.NavigateBack(event, s, "User ID not found in the database.")
		return
	}

	// Log the deletion
	go m.layout.db.Models().Activities().Log(context.Background(), &types.ActivityLog{
		ActivityTarget: types.ActivityTarget{
			UserID: id,
		},
		ReviewerID:        uint64(event.User().ID),
		ActivityType:      enum.ActivityTypeUserDeleted,
		ActivityTimestamp: time.Now(),
		Details: map[string]any{
			"reason": reason,
		},
	})

	r.NavigateBack(event, s, fmt.Sprintf("Successfully deleted user %d.", id))
}

// handleDeleteGroup processes the group deletion action.
func (m *ConfirmMenu) handleDeleteGroup(event *events.ComponentInteractionCreate, s *session.Session, r *pagination.Respond, idStr string, reason string) {
	// Parse ID from modal
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		r.Error(event, "Invalid ID format.")
		return
	}

	// Delete group
	found, err := m.layout.db.Models().Groups().DeleteGroup(context.Background(), id)
	if err != nil {
		m.layout.logger.Error("Failed to delete group",
			zap.Error(err),
			zap.Uint64("id", id))
		r.Error(event, "Failed to delete group. Please try again.")
		return
	}

	// Check if the ID was found in the database
	if !found {
		r.NavigateBack(event, s, "Group ID not found in the database.")
		return
	}

	// Log the deletion
	go m.layout.db.Models().Activities().Log(context.Background(), &types.ActivityLog{
		ActivityTarget: types.ActivityTarget{
			GroupID: id,
		},
		ReviewerID:        uint64(event.User().ID),
		ActivityType:      enum.ActivityTypeGroupDeleted,
		ActivityTimestamp: time.Now(),
		Details: map[string]any{
			"reason": reason,
		},
	})

	r.NavigateBack(event, s, fmt.Sprintf("Successfully deleted group %d.", id))
}
