package manager

import (
	"context"
	"fmt"

	"github.com/bytedance/sonic"
	"github.com/robalyx/rotector/internal/cloudflare/api"
	"github.com/robalyx/rotector/internal/database/types"
	"go.uber.org/zap"
)

const (
	GroupFlagTypeMixed     = 0
	GroupFlagTypeFlagged   = 1
	GroupFlagTypeConfirmed = 2
)

// GroupFlags handles group flagging operations for D1 database.
type GroupFlags struct {
	api    *api.Cloudflare
	logger *zap.Logger
}

// NewGroupFlags creates a new group flags manager.
func NewGroupFlags(cloudflareAPI *api.Cloudflare, logger *zap.Logger) *GroupFlags {
	return &GroupFlags{
		api:    cloudflareAPI,
		logger: logger,
	}
}

// AddConfirmed inserts or updates a confirmed group in the group_flags table.
func (g *GroupFlags) AddConfirmed(ctx context.Context, group *types.ReviewGroup) error {
	return g.addGroup(ctx, group, GroupFlagTypeConfirmed)
}

// AddMixed inserts or updates a mixed group in the group_flags table.
func (g *GroupFlags) AddMixed(ctx context.Context, group *types.ReviewGroup) error {
	return g.addGroup(ctx, group, GroupFlagTypeMixed)
}

// AddFlagged inserts flagged groups into the group_flags table.
func (g *GroupFlags) AddFlagged(ctx context.Context, flaggedGroups map[int64]*types.ReviewGroup) error {
	if len(flaggedGroups) == 0 {
		return nil
	}

	for _, group := range flaggedGroups {
		if err := g.addGroup(ctx, group, GroupFlagTypeFlagged); err != nil {
			g.logger.Error("Failed to add flagged group to D1 database",
				zap.Error(err),
				zap.Int64("groupID", group.ID))

			continue
		}
	}

	g.logger.Debug("Added flagged groups to group_flags table",
		zap.Int("count", len(flaggedGroups)))

	return nil
}

// Remove removes a group from the group_flags table.
func (g *GroupFlags) Remove(ctx context.Context, groupID int64) error {
	sqlStmt := "DELETE FROM group_flags WHERE group_id = ?"

	_, err := g.api.ExecuteSQL(ctx, sqlStmt, []any{groupID})
	if err != nil {
		return fmt.Errorf("failed to remove group from group_flags: %w", err)
	}

	g.logger.Debug("Removed group from group_flags table",
		zap.Int64("groupID", groupID))

	return nil
}

// addGroup is a helper method that inserts or updates a group with the specified flag type.
func (g *GroupFlags) addGroup(ctx context.Context, group *types.ReviewGroup, flagType int) error {
	// Prepare reasons JSON
	reasonsJSON := "{}"

	if len(group.Reasons) > 0 {
		reasonsData := make(map[string]map[string]any)
		for reasonType, reason := range group.Reasons {
			reasonsData[reasonType.String()] = map[string]any{
				"message":    reason.Message,
				"confidence": reason.Confidence,
				"evidence":   reason.Evidence,
			}
		}

		jsonBytes, err := sonic.Marshal(reasonsData)
		if err != nil {
			return fmt.Errorf("failed to marshal group reasons: %w", err)
		}

		reasonsJSON = string(jsonBytes)
	}

	sqlStmt := `
		INSERT OR REPLACE INTO group_flags (
			group_id, 
			flag_type, 
			confidence, 
			reasons
		) VALUES (?, ?, ?, ?)`

	params := []any{
		group.ID,
		flagType,
		group.Confidence,
		reasonsJSON,
	}

	_, err := g.api.ExecuteSQL(ctx, sqlStmt, params)
	if err != nil {
		return fmt.Errorf("failed to insert/update group %d in group_flags: %w", group.ID, err)
	}

	g.logger.Debug("Added/updated group in group_flags table",
		zap.Int64("groupID", group.ID),
		zap.Int("flagType", flagType),
		zap.Float64("confidence", group.Confidence))

	return nil
}
