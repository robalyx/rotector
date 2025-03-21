package models

import (
	"context"
	"fmt"
	"time"

	"github.com/robalyx/rotector/internal/common/storage/database/types"
	"github.com/uptrace/bun"
	"go.uber.org/zap"
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
		logger: logger.Named("db_sync"),
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

// UpsertServerMembers creates or updates multiple server member records.
func (m *SyncModel) UpsertServerMembers(
	ctx context.Context, members []*types.DiscordServerMember, updateScanTime bool,
) error {
	if len(members) == 0 {
		return nil
	}

	now := time.Now()
	return m.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Upsert server members
		_, err := tx.NewInsert().
			Model(&members).
			On("CONFLICT (server_id, user_id) DO UPDATE").
			Set("updated_at = EXCLUDED.updated_at").
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to upsert server members: %w", err)
		}

		// Create full scan records for each unique user
		uniqueUsers := make(map[uint64]struct{})
		for _, member := range members {
			uniqueUsers[member.UserID] = struct{}{}
		}

		// Convert unique users to full scan records
		scans := make([]*types.DiscordUserFullScan, 0, len(uniqueUsers))
		for userID := range uniqueUsers {
			scans = append(scans, &types.DiscordUserFullScan{
				UserID:   userID,
				LastScan: now.Add(-time.Hour * 24),
			})
		}

		// Insert full scan records, but only update timestamp if requested
		if len(scans) > 0 {
			query := tx.NewInsert().
				Model(&scans)

			if updateScanTime {
				query = query.On("CONFLICT (user_id) DO UPDATE").
					Set("last_scan = EXCLUDED.last_scan")
			} else {
				query = query.On("CONFLICT (user_id) DO NOTHING")
			}

			_, err = query.Exec(ctx)
			if err != nil {
				return fmt.Errorf("failed to insert user full scans: %w", err)
			}
		}

		m.logger.Debug("Upserted batch of server members",
			zap.Int("member_count", len(members)))

		return nil
	})
}

// RemoveServerMember removes a member from a server.
func (m *SyncModel) RemoveServerMember(ctx context.Context, serverID, userID uint64) error {
	_, err := m.db.NewDelete().
		Model((*types.DiscordServerMember)(nil)).
		Where("server_id = ? AND user_id = ?", serverID, userID).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to remove server member: %w", err)
	}
	return nil
}

// GetDiscordUserGuildsByCursor returns paginated guild memberships for a specific Discord user.
func (m *SyncModel) GetDiscordUserGuildsByCursor(
	ctx context.Context,
	discordUserID uint64,
	cursor *types.GuildCursor,
	limit int,
) ([]*types.UserGuildInfo, *types.GuildCursor, error) {
	var members []*types.DiscordServerMember

	// Build base query
	query := m.db.NewSelect().
		Model(&members).
		Where("user_id = ?", discordUserID).
		Limit(limit + 1) // Get one extra to determine if there's a next page

	// Apply cursor conditions if provided
	if cursor != nil {
		query = query.Where("(joined_at, server_id) < (?, ?)",
			cursor.JoinedAt,
			cursor.ServerID,
		)
	}

	// Order by join time and server ID for stable pagination
	query = query.Order("joined_at DESC", "server_id DESC")

	// Execute query
	err := query.Scan(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get Discord user guild memberships: %w", err)
	}

	// Convert to UserGuildInfo slice
	guilds := make([]*types.UserGuildInfo, len(members))
	for i, member := range members {
		guilds[i] = &types.UserGuildInfo{
			ServerID:  member.ServerID,
			JoinedAt:  member.JoinedAt,
			UpdatedAt: member.UpdatedAt,
		}
	}

	// Check if we have a next page
	var nextCursor *types.GuildCursor
	if len(guilds) > limit {
		lastGuild := guilds[limit] // Get the extra guild we fetched
		nextCursor = &types.GuildCursor{
			JoinedAt: lastGuild.JoinedAt,
			ServerID: lastGuild.ServerID,
		}
		guilds = guilds[:limit] // Remove the extra guild from results
	}

	return guilds, nextCursor, nil
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

// GetFlaggedServerMembers returns information about flagged users and their servers.
func (m *SyncModel) GetFlaggedServerMembers(
	ctx context.Context, memberIDs []uint64,
) (map[uint64][]*types.UserGuildInfo, error) {
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

// DeleteUserGuildMemberships deletes all guild memberships for a specific user.
func (m *SyncModel) DeleteUserGuildMemberships(ctx context.Context, userID uint64) error {
	return m.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Delete from server members
		_, err := tx.NewDelete().
			Model((*types.DiscordServerMember)(nil)).
			Where("user_id = ?", userID).
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to delete user guild memberships: %w", err)
		}

		// Delete from full scan
		_, err = tx.NewDelete().
			Model((*types.DiscordUserFullScan)(nil)).
			Where("user_id = ?", userID).
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to delete user full scan record: %w", err)
		}

		m.logger.Debug("Deleted user data",
			zap.Uint64("user_id", userID))

		return nil
	})
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

// GetDiscordUserGuildCount returns the total number of flagged guilds for a specific Discord user.
func (m *SyncModel) GetDiscordUserGuildCount(ctx context.Context, discordUserID uint64) (int, error) {
	count, err := m.db.NewSelect().
		Model((*types.DiscordServerMember)(nil)).
		Where("user_id = ?", discordUserID).
		Count(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to get Discord user guild count: %w", err)
	}
	return count, nil
}

// PurgeOldServerMembers removes Discord server member records older than the specified cutoff date.
func (m *SyncModel) PurgeOldServerMembers(ctx context.Context, cutoffDate time.Time) (int, error) {
	var affected int64
	err := m.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Get users to be purged
		var usersToPurge []uint64
		err := tx.NewSelect().
			Model((*types.DiscordServerMember)(nil)).
			Column("user_id").
			Where("updated_at < ?", cutoffDate).
			Group("user_id").
			Scan(ctx, &usersToPurge)
		if err != nil {
			return fmt.Errorf("failed to get users to purge: %w", err)
		}

		// Delete old server members
		result, err := tx.NewDelete().
			Model((*types.DiscordServerMember)(nil)).
			Where("updated_at < ?", cutoffDate).
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to purge old server members: %w", err)
		}

		// Delete corresponding full scan records
		if len(usersToPurge) > 0 {
			_, err = tx.NewDelete().
				Model((*types.DiscordUserFullScan)(nil)).
				Where("user_id IN (?)", bun.In(usersToPurge)).
				Exec(ctx)
			if err != nil {
				return fmt.Errorf("failed to purge full scan records: %w", err)
			}
		}

		affected, err = result.RowsAffected()
		if err != nil {
			return fmt.Errorf("failed to get affected rows: %w", err)
		}

		m.logger.Debug("Purged old server members and full scan records",
			zap.Int64("rowsAffected", affected),
			zap.Time("cutoffDate", cutoffDate),
			zap.Int("usersRemoved", len(usersToPurge)))

		return nil
	})
	if err != nil {
		return 0, err
	}

	return int(affected), nil
}

// MarkUserDataRedacted marks a user's data as redacted.
func (m *SyncModel) MarkUserDataRedacted(ctx context.Context, userID uint64) error {
	now := time.Now()
	redaction := &types.DiscordUserRedaction{
		UserID:     userID,
		RedactedAt: now,
		UpdatedAt:  now,
	}

	_, err := m.db.NewInsert().
		Model(redaction).
		On("CONFLICT (user_id) DO UPDATE").
		Set("updated_at = EXCLUDED.updated_at").
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to mark user data as redacted: %w", err)
	}

	m.logger.Debug("Marked user data as redacted",
		zap.Uint64("user_id", userID))

	return nil
}

// IsUserDataRedacted checks if a user's data has been redacted.
func (m *SyncModel) IsUserDataRedacted(ctx context.Context, userID uint64) (bool, error) {
	exists, err := m.db.NewSelect().
		Model((*types.DiscordUserRedaction)(nil)).
		Where("user_id = ?", userID).
		Exists(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to check if user data is redacted: %w", err)
	}
	return exists, nil
}

// GetUsersForFullScan returns users that haven't been scanned recently.
func (m *SyncModel) GetUsersForFullScan(ctx context.Context, before time.Time, limit int) ([]uint64, error) {
	var scans []types.DiscordUserFullScan
	err := m.db.NewSelect().
		Model(&scans).
		Where("last_scan < ?", before).
		Order("last_scan ASC").
		Limit(limit).
		Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get users for full scan: %w", err)
	}

	userIDs := make([]uint64, len(scans))
	for i, scan := range scans {
		userIDs[i] = scan.UserID
	}
	return userIDs, nil
}

// WhitelistDiscordUser adds a Discord user to the whitelist.
func (m *SyncModel) WhitelistDiscordUser(ctx context.Context, whitelist *types.DiscordUserWhitelist) error {
	_, err := m.db.NewInsert().
		Model(whitelist).
		On("CONFLICT (user_id) DO UPDATE").
		Set("whitelisted_at = EXCLUDED.whitelisted_at").
		Set("reason = EXCLUDED.reason").
		Set("reviewer_id = EXCLUDED.reviewer_id").
		Set("appeal_id = EXCLUDED.appeal_id").
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to whitelist Discord user: %w", err)
	}

	m.logger.Debug("Added Discord user to whitelist",
		zap.Uint64("user_id", whitelist.UserID),
		zap.Int64("appeal_id", whitelist.AppealID))

	return nil
}

// IsUserWhitelisted checks if a user is whitelisted.
func (m *SyncModel) IsUserWhitelisted(ctx context.Context, userID uint64) (bool, error) {
	exists, err := m.db.NewSelect().
		Model((*types.DiscordUserWhitelist)(nil)).
		Where("user_id = ?", userID).
		Exists(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to check if user is whitelisted: %w", err)
	}
	return exists, nil
}
