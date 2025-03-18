package discord

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/diamondburned/arikawa/v3/session"
	"github.com/redis/rueidis"
	"github.com/robalyx/rotector/internal/common/storage/database"
	"github.com/robalyx/rotector/internal/common/storage/database/types"
	"go.uber.org/zap"
)

// UserProfile represents the user profile data from Discord.
type UserProfile struct {
	User struct {
		ID       string `json:"id"`
		Username string `json:"username"`
	} `json:"user"`
	MutualGuilds []struct {
		ID   string `json:"id"`
		Nick string `json:"nick"`
	} `json:"mutual_guilds"` //nolint:tagliatelle // discord api response
}

// Scanner handles full guild scanning for Discord users.
type Scanner struct {
	db        database.Client
	ratelimit rueidis.Client
	session   *session.Session
	logger    *zap.Logger
}

// NewScanner creates a new full scan handler.
func NewScanner(db database.Client, ratelimit rueidis.Client, session *session.Session, logger *zap.Logger) *Scanner {
	return &Scanner{
		db:        db,
		ratelimit: ratelimit,
		session:   session,
		logger:    logger.Named("discord_scanner"),
	}
}

// PerformFullScan executes a full guild scan for a user and returns their username if found.
func (s *Scanner) PerformFullScan(ctx context.Context, userID uint64) (string, error) {
	// Set rate limit key in Redis
	err := s.ratelimit.Do(ctx, s.ratelimit.B().Set().
		Key("mutual_guilds_lookup").
		Value("1").
		Ex(2*time.Second).
		Build()).Error()
	if err != nil {
		return "", fmt.Errorf("failed to set rate limit: %w", err)
	}

	// Fetch mutual guilds
	var profile UserProfile
	endpoint := fmt.Sprintf(
		"https://discord.com/api/v9/users/%d/profile?with_mutual_guilds=true&with_mutual_friends=false",
		userID,
	)

	err = s.session.RequestJSON(&profile, "GET", endpoint)
	if err != nil {
		return "", fmt.Errorf("failed to fetch profile: %w", err)
	}

	// Process mutual guilds
	members := make([]*types.DiscordServerMember, 0, len(profile.MutualGuilds))
	now := time.Now()

	for _, guild := range profile.MutualGuilds {
		guildID, err := strconv.ParseUint(guild.ID, 10, 64)
		if err != nil {
			continue
		}

		members = append(members, &types.DiscordServerMember{
			ServerID:  guildID,
			UserID:    userID,
			UpdatedAt: now,
		})
	}

	// Batch upsert members
	if err := s.db.Model().Sync().UpsertServerMembers(ctx, members, true); err != nil {
		return "", fmt.Errorf("failed to upsert server members: %w", err)
	}

	// Ban user from system
	s.db.Service().Ban().CreateCondoBans(ctx, []uint64{userID})

	s.logger.Info("Full scan complete",
		zap.Uint64("user_id", userID),
		zap.Int("guild_count", len(profile.MutualGuilds)))

	return profile.User.Username, nil
}

// ShouldScan checks if a user is eligible for scanning based on rate limits.
func (s *Scanner) ShouldScan(ctx context.Context, userID uint64) bool {
	exists, err := s.ratelimit.Do(ctx, s.ratelimit.B().Exists().Key("mutual_guilds_lookup").Build()).ToInt64()
	if err != nil {
		s.logger.Error("Failed to check rate limit",
			zap.Error(err),
			zap.Uint64("user_id", userID))
		return false
	}
	return exists == 0
}
