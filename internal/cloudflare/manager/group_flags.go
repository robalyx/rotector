package manager

import (
	"context"
	"fmt"
	"strconv"
	"strings"

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
	d1     *api.D1Client
	logger *zap.Logger
}

// NewGroupFlags creates a new group flags manager.
func NewGroupFlags(d1Client *api.D1Client, logger *zap.Logger) *GroupFlags {
	return &GroupFlags{
		d1:     d1Client,
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

	_, err := g.d1.ExecuteSQL(ctx, sqlStmt, []any{groupID})
	if err != nil {
		return fmt.Errorf("failed to remove group from group_flags: %w", err)
	}

	g.logger.Debug("Removed group from group_flags table",
		zap.Int64("groupID", groupID))

	return nil
}

// UpdateBanStatus updates the is_banned field for groups in the group_flags table.
func (g *GroupFlags) UpdateBanStatus(ctx context.Context, groupIDs []int64, isBanned bool) error {
	if len(groupIDs) == 0 {
		return nil
	}

	// Build query to update is_banned
	query := "UPDATE group_flags SET is_banned = ? WHERE group_id IN ("
	params := make([]any, 0, len(groupIDs)+1)

	// Add the banned status as first parameter
	if isBanned {
		params = append(params, 1)
	} else {
		params = append(params, 0)
	}

	// Add placeholders for WHERE clause
	var whereInPlaceholders strings.Builder

	for i, groupID := range groupIDs {
		if i > 0 {
			whereInPlaceholders.WriteString(",")
		}

		whereInPlaceholders.WriteString("?")

		params = append(params, groupID)
	}

	query += whereInPlaceholders.String()

	query += ")"

	result, err := g.d1.ExecuteSQL(ctx, query, params)
	if err != nil {
		return fmt.Errorf("failed to update group ban status: %w", err)
	}

	g.logger.Debug("Updated group ban status in group_flags table",
		zap.Int("groups_processed", len(groupIDs)),
		zap.Bool("is_banned", isBanned),
		zap.Int("rows_affected", len(result)))

	return nil
}

// addGroup is a helper method that inserts or updates a group with the specified flag type.
func (g *GroupFlags) addGroup(ctx context.Context, group *types.ReviewGroup, flagType int) error {
	// Prepare reasons JSON
	reasonsJSON := "{}"

	if len(group.Reasons) > 0 {
		reasonsData := make(map[string]map[string]any)
		for reasonType, reason := range group.Reasons {
			reasonsData[strconv.Itoa(int(reasonType))] = map[string]any{
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
			reasons,
			is_banned
		) VALUES (?, ?, ?, ?, ?)`

	isBanned := 0
	if group.IsLocked {
		isBanned = 1
	}

	params := []any{
		group.ID,
		flagType,
		group.Confidence,
		reasonsJSON,
		isBanned,
	}

	_, err := g.d1.ExecuteSQL(ctx, sqlStmt, params)
	if err != nil {
		return fmt.Errorf("failed to insert/update group %d in group_flags: %w", group.ID, err)
	}

	g.logger.Debug("Added/updated group in group_flags table",
		zap.Int64("groupID", group.ID),
		zap.Int("flagType", flagType),
		zap.Float64("confidence", group.Confidence))

	return nil
}
