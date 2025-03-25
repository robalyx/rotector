package admin

import (
	"fmt"
	"strconv"
	"time"

	"github.com/disgoorg/disgo/discord"
	builder "github.com/robalyx/rotector/internal/bot/builder/admin"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/interaction"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/common/storage/database/types"
	"github.com/robalyx/rotector/internal/common/storage/database/types/enum"
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
			return builder.NewConfirmBuilder(s).Build()
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
	case constants.BanUserAction:
		m.handleBanUser(ctx, s, id, reason)
	case constants.UnbanUserAction:
		m.handleUnbanUser(ctx, id, reason)
	case constants.DeleteUserAction:
		m.handleDeleteUser(ctx, id, reason)
	case constants.DeleteGroupAction:
		m.handleDeleteGroup(ctx, id, reason)
	}
}

// handleBanUser processes the user ban action.
func (m *ConfirmMenu) handleBanUser(ctx *interaction.Context, s *session.Session, userID string, notes string) {
	// Parse user ID
	id, err := strconv.ParseUint(userID, 10, 64)
	if err != nil {
		ctx.Cancel("Invalid user ID format.")
		return
	}

	// Get ban reason and expiry
	banReason := session.AdminBanReason.Get(s)
	expiresAt := session.AdminBanExpiry.Get(s)

	// Ban the user
	now := time.Now()
	if err := m.layout.db.Model().Ban().BanUser(ctx.Context(), &types.DiscordBan{
		ID:        id,
		Reason:    banReason,
		Source:    enum.BanSourceAdmin,
		Notes:     notes,
		BannedBy:  uint64(ctx.Event().User().ID),
		BannedAt:  now,
		ExpiresAt: expiresAt,
		UpdatedAt: now,
	}); err != nil {
		m.layout.logger.Error("Failed to ban user",
			zap.Error(err),
			zap.Uint64("user_id", id),
			zap.Uint64("admin_id", uint64(ctx.Event().User().ID)),
		)
		ctx.Error("Failed to ban user. Please try again.")
		return
	}

	// Log the ban action
	go m.layout.db.Model().Activity().Log(ctx.Context(), &types.ActivityLog{
		ActivityTarget: types.ActivityTarget{
			DiscordID: id,
		},
		ReviewerID:        uint64(ctx.Event().User().ID),
		ActivityType:      enum.ActivityTypeDiscordUserBanned,
		ActivityTimestamp: time.Now(),
		Details: map[string]any{
			"notes":      notes,
			"ban_reason": banReason.String(),
			"expires_at": expiresAt,
		},
	})

	ctx.NavigateBack(fmt.Sprintf("Successfully banned user %d.", id))
}

// handleUnbanUser processes the user unban action.
func (m *ConfirmMenu) handleUnbanUser(ctx *interaction.Context, userID string, notes string) {
	// Parse user ID
	id, err := strconv.ParseUint(userID, 10, 64)
	if err != nil {
		ctx.Cancel("Invalid user ID format.")
		return
	}

	// Unban the user
	unbanned, err := m.layout.db.Model().Ban().UnbanUser(ctx.Context(), id)
	if err != nil {
		m.layout.logger.Error("Failed to unban user",
			zap.Error(err),
			zap.Uint64("user_id", id),
			zap.Uint64("admin_id", uint64(ctx.Event().User().ID)),
		)
		ctx.Error("Failed to unban user. Please try again.")
		return
	}

	// Check if the user was actually banned
	if !unbanned {
		ctx.NavigateBack("User is not currently banned.")
		return
	}

	// Log the unban action
	go m.layout.db.Model().Activity().Log(ctx.Context(), &types.ActivityLog{
		ActivityTarget: types.ActivityTarget{
			DiscordID: id,
		},
		ReviewerID:        uint64(ctx.Event().User().ID),
		ActivityType:      enum.ActivityTypeDiscordUserUnbanned,
		ActivityTimestamp: time.Now(),
		Details: map[string]any{
			"notes": notes,
		},
	})

	ctx.NavigateBack(fmt.Sprintf("Successfully unbanned user %d.", id))
}

// handleDeleteUser processes the user deletion action.
func (m *ConfirmMenu) handleDeleteUser(ctx *interaction.Context, idStr string, reason string) {
	// Parse ID from modal
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		ctx.Cancel("Invalid ID format.")
		return
	}

	// Delete user
	found, err := m.layout.db.Model().User().DeleteUser(ctx.Context(), id)
	if err != nil {
		m.layout.logger.Error("Failed to delete user",
			zap.Error(err),
			zap.Uint64("id", id))
		ctx.Error("Failed to delete user. Please try again.")
		return
	}

	// Check if the ID was found in the database
	if !found {
		ctx.NavigateBack("User ID not found in the database.")
		return
	}

	// Log the deletion
	go m.layout.db.Model().Activity().Log(ctx.Context(), &types.ActivityLog{
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

	ctx.NavigateBack(fmt.Sprintf("Successfully deleted user %d.", id))
}

// handleDeleteGroup processes the group deletion action.
func (m *ConfirmMenu) handleDeleteGroup(ctx *interaction.Context, idStr string, reason string) {
	// Parse ID from modal
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		ctx.Cancel("Invalid ID format.")
		return
	}

	// Delete group
	found, err := m.layout.db.Model().Group().DeleteGroup(ctx.Context(), id)
	if err != nil {
		m.layout.logger.Error("Failed to delete group",
			zap.Error(err),
			zap.Uint64("id", id))
		ctx.Error("Failed to delete group. Please try again.")
		return
	}

	// Check if the ID was found in the database
	if !found {
		ctx.NavigateBack("Group ID not found in the database.")
		return
	}

	// Log the deletion
	go m.layout.db.Model().Activity().Log(ctx.Context(), &types.ActivityLog{
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

	ctx.NavigateBack(fmt.Sprintf("Successfully deleted group %d.", id))
}
