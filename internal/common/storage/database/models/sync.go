package models

import (
	"context"
	"fmt"

	"github.com/uptrace/bun"
	"go.uber.org/zap"

	"github.com/robalyx/rotector/internal/common/storage/database/types"
)

// SyncModel handles database operations for Discord server syncing.
type SyncModel struct {
	db     *bun.DB
	logger *zap.Logger
}

// NewSync creates a new sync model instance.
func NewSync(db *bun.DB, logger *zap.Logger) *SyncModel {
	return &SyncModel{
		db:     db,
		logger: logger,
	}
}

// UpsertServerMember creates or updates a single server member record.
func (m *SyncModel) UpsertServerMember(ctx context.Context, member *types.DiscordServerMember) error {
	_, err := m.db.NewInsert().
		Model(member).
		On("CONFLICT (server_id, user_id) DO UPDATE").
		Set("updated_at = EXCLUDED.updated_at").
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to upsert member: %w", err)
	}
	return nil
}

// UpsertServerMembers creates or updates server member records.
func (m *SyncModel) UpsertServerMembers(ctx context.Context, members []*types.DiscordServerMember) error {
	_, err := m.db.NewInsert().
		Model(&members).
		On("CONFLICT (server_id, user_id) DO UPDATE").
		Set("updated_at = EXCLUDED.updated_at").
		Exec(ctx)
	return err
}

// RemoveServerMember removes a member from a server.
func (m *SyncModel) RemoveServerMember(ctx context.Context, serverID, userID uint64) error {
	_, err := m.db.NewDelete().
		Model((*types.DiscordServerMember)(nil)).
		Where("server_id = ? AND user_id = ?", serverID, userID).
		Exec(ctx)
	return err
}

// GetDiscordUserGuilds returns all guild memberships for a specific Discord user.
func (m *SyncModel) GetDiscordUserGuilds(ctx context.Context, discordUserID uint64) ([]*types.UserGuildInfo, error) {
	var members []*types.DiscordServerMember

	err := m.db.NewSelect().
		Model(&members).
		Where("user_id = ?", discordUserID).
		Order("joined_at DESC").
		Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get Discord user guild memberships: %w", err)
	}

	// Convert to UserGuildInfo format
	guilds := make([]*types.UserGuildInfo, len(members))
	for i, member := range members {
		guilds[i] = &types.UserGuildInfo{
			ServerID:  member.ServerID,
			JoinedAt:  member.JoinedAt,
			UpdatedAt: member.UpdatedAt,
		}
	}

	return guilds, nil
}

// UpsertServerInfo creates or updates a single server information record.
func (m *SyncModel) UpsertServerInfo(ctx context.Context, server *types.DiscordServerInfo) error {
	_, err := m.db.NewInsert().
		Model(server).
		On("CONFLICT (server_id) DO UPDATE").
		Set("name = EXCLUDED.name").
		Set("updated_at = EXCLUDED.updated_at").
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to upsert server info: %w", err)
	}
	return nil
}

// GetServerInfo returns server information for the given server IDs.
func (m *SyncModel) GetServerInfo(ctx context.Context, serverIDs []uint64) ([]*types.DiscordServerInfo, error) {
	var servers []*types.DiscordServerInfo
	err := m.db.NewSelect().
		Model(&servers).
		Where("server_id IN (?)", bun.In(serverIDs)).
		Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get server info: %w", err)
	}
	return servers, nil
}

// BatchUpsertServerMembers creates or updates multiple server member records for a specific guild.
func (m *SyncModel) BatchUpsertServerMembers(ctx context.Context, serverID uint64, members []*types.DiscordServerMember) error {
	if len(members) == 0 {
		return nil
	}

	// Direct upsert into the main table
	_, err := m.db.NewInsert().
		Model(&members).
		On("CONFLICT (server_id, user_id) DO UPDATE").
		Set("joined_at = EXCLUDED.joined_at").
		Set("updated_at = EXCLUDED.updated_at").
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to upsert members for server %d: %w", serverID, err)
	}

	// Log the batch update
	m.logger.Debug("Upserting batch of members",
		zap.Uint64("server_id", serverID),
		zap.Int("member_count", len(members)))

	return nil
}

// GetFlaggedServerMembers returns information about flagged users and their servers.
func (m *SyncModel) GetFlaggedServerMembers(ctx context.Context, memberIDs []uint64) (map[uint64][]*types.UserGuildInfo, error) {
	// Query to find which members exist and their server information
	var flaggedMembers []*types.DiscordServerMember

	err := m.db.NewSelect().
		Model((*types.DiscordServerMember)(nil)).
		Column("user_id", "server_id", "joined_at", "updated_at").
		Where("user_id IN (?)", bun.In(memberIDs)).
		Scan(ctx, &flaggedMembers)
	if err != nil {
		return nil, fmt.Errorf("failed to get flagged members: %w", err)
	}

	// Convert to map of user ID to their guild info
	result := make(map[uint64][]*types.UserGuildInfo)
	for _, member := range flaggedMembers {
		guildInfo := &types.UserGuildInfo{
			ServerID:  member.ServerID,
			JoinedAt:  member.JoinedAt,
			UpdatedAt: member.UpdatedAt,
		}
		result[member.UserID] = append(result[member.UserID], guildInfo)
	}

	return result, nil
}

// GetUniqueGuildCount returns the number of unique guilds in the database.
func (m *SyncModel) GetUniqueGuildCount(ctx context.Context) (int, error) {
	count, err := m.db.NewSelect().
		Model((*types.DiscordServerInfo)(nil)).
		Count(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to get unique guild count: %w", err)
	}
	return count, nil
}

// GetUniqueUserCount returns the number of unique user IDs in the server members table.
func (m *SyncModel) GetUniqueUserCount(ctx context.Context) (int, error) {
	var count int
	_, err := m.db.NewRaw(`
		SELECT COUNT(DISTINCT user_id) 
		FROM discord_server_members
	`).Exec(ctx, &count)
	if err != nil {
		return 0, fmt.Errorf("failed to get unique user count: %w", err)
	}
	return count, nil
}
