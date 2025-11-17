package discord

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/diamondburned/arikawa/v3/discord"
	"github.com/diamondburned/arikawa/v3/session"
	"github.com/diamondburned/arikawa/v3/state"
	"github.com/google/uuid"
	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/redis/rueidis"
	"github.com/robalyx/rotector/internal/ai"
	"github.com/robalyx/rotector/internal/cloudflare"
	"github.com/robalyx/rotector/internal/database"
	"github.com/robalyx/rotector/internal/database/types"
	"github.com/robalyx/rotector/internal/database/types/enum"
	"github.com/robalyx/rotector/internal/discord/rate"
	"github.com/robalyx/rotector/pkg/utils"
	"github.com/sony/gobreaker"
	"go.uber.org/zap"
)

// UserProfile represents the user profile data from Discord.
type UserProfile struct {
	User struct {
		ID       string `json:"id"`
		Username string `json:"username"`
	} `json:"user"`
	ConnectedAccounts []struct {
		Type     string `json:"type"`
		ID       string `json:"id"`
		Name     string `json:"name"`
		Verified bool   `json:"verified"`
	} `json:"connected_accounts"` //nolint:tagliatelle // discord api response
	MutualGuilds []struct {
		ID   string `json:"id"`
		Nick string `json:"nick"`
	} `json:"mutual_guilds"` //nolint:tagliatelle // discord api response
}

// MessageSearchResponse represents the Discord message search API response.
type MessageSearchResponse struct {
	Messages [][]struct {
		ID      string `json:"id"`
		Content string `json:"content"`
		Author  struct {
			ID string `json:"id"`
		} `json:"author"`
	} `json:"messages"`
}

var (
	ErrUserNotVisible     = errors.New("user not visible to scanner")
	ErrCircuitBreakerOpen = errors.New("circuit breaker is open")
	ErrUserBanned         = errors.New("user is banned")
)

// Scanner handles full guild scanning for Discord users.
type Scanner struct {
	db              database.Client
	roAPI           *api.API
	cfClient        *cloudflare.Client
	ratelimit       rueidis.Client
	state           *state.State
	session         *session.Session
	messageAnalyzer *ai.MessageAnalyzer
	breaker         *gobreaker.CircuitBreaker
	rateLimiter     *rate.Limiter
	logger          *zap.Logger
	scannerID       string
}

// NewScanner creates a new full scan handler.
func NewScanner(
	db database.Client, roAPI *api.API, cfClient *cloudflare.Client, ratelimit rueidis.Client,
	state *state.State, session *session.Session,
	messageAnalyzer *ai.MessageAnalyzer, rateLimiter *rate.Limiter, scannerID string, logger *zap.Logger,
) *Scanner {
	scannerLogger := logger.Named("discord_scanner")

	// Create circuit breaker for Discord API calls
	breaker := gobreaker.NewCircuitBreaker(gobreaker.Settings{
		Name:        "discord_api",
		MaxRequests: 1,
		Timeout:     60 * time.Second,
		Interval:    0,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			failureRatio := float64(counts.TotalFailures) / float64(counts.Requests)
			return counts.Requests >= 10 && failureRatio >= 0.6
		},
		OnStateChange: func(_ string, from gobreaker.State, to gobreaker.State) {
			scannerLogger.Warn("Discord API circuit breaker state changed",
				zap.String("from", from.String()),
				zap.String("to", to.String()))
		},
	})

	return &Scanner{
		db:              db,
		roAPI:           roAPI,
		cfClient:        cfClient,
		ratelimit:       ratelimit,
		state:           state,
		session:         session,
		messageAnalyzer: messageAnalyzer,
		breaker:         breaker,
		rateLimiter:     rateLimiter,
		logger:          scannerLogger,
		scannerID:       scannerID,
	}
}

// PerformFullScan executes a full guild scan for a user and returns their username and discovered connections.
// If updateScanTime is true, the user's last scan timestamp will be updated.
func (s *Scanner) PerformFullScan(ctx context.Context, userID uint64, updateScanTime bool) (string, []*types.DiscordRobloxConnection, error) {
	// Fetch mutual guilds
	var profile UserProfile

	endpoint := fmt.Sprintf(
		"https://discord.com/api/v9/users/%d/profile?with_mutual_guilds=true&with_mutual_friends=false",
		userID,
	)

	// Wait for rate limit slot
	if err := s.rateLimiter.WaitForNextSlot(ctx); err != nil {
		return "", nil, err
	}

	// Execute API call
	userExists, err := s.breaker.Execute(func() (any, error) {
		err := s.session.WithContext(ctx).RequestJSON(&profile, "GET", endpoint)
		if err != nil {
			// Treat as a successful API call (user just doesn't exist)
			if strings.Contains(err.Error(), "Unknown User") {
				return false, nil
			}

			return false, err
		}

		return true, nil
	})
	if err != nil {
		// Check for circuit breaker open state
		if errors.Is(err, gobreaker.ErrOpenState) {
			s.logger.Warn("Discord API circuit breaker is open, skipping user scan",
				zap.Uint64("userID", userID))

			return "", nil, fmt.Errorf("%w: %w", ErrCircuitBreakerOpen, err)
		}

		return "", nil, fmt.Errorf("failed to fetch profile: %w", err)
	}

	// Handle cases where scanner cannot see the user
	if !userExists.(bool) {
		s.logger.Info("Scanner cannot see user",
			zap.Uint64("userID", userID),
			zap.String("scannerID", s.scannerID))

		return "", nil, fmt.Errorf("%w: userID=%d", ErrUserNotVisible, userID)
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
	if err := s.db.Model().Sync().UpsertServerMembers(ctx, members, updateScanTime); err != nil {
		return "", nil, fmt.Errorf("failed to upsert server members: %w", err)
	}

	// Extract Roblox connections from Discord profile
	var discoveredConnections []*types.DiscordRobloxConnection

	for _, account := range profile.ConnectedAccounts {
		if account.Type == "roblox" && account.Verified {
			robloxUserID, err := strconv.ParseInt(account.ID, 10, 64)
			if err != nil {
				s.logger.Warn("Failed to parse Roblox user ID",
					zap.String("id", account.ID),
					zap.Error(err))

				continue
			}

			discoveredConnections = append(discoveredConnections, &types.DiscordRobloxConnection{
				DiscordUserID:  userID,
				RobloxUserID:   robloxUserID,
				RobloxUsername: account.Name,
				Verified:       account.Verified,
				DetectedAt:     now,
				UpdatedAt:      now,
			})
		}
	}

	s.logger.Info("Full scan complete",
		zap.Uint64("userID", userID),
		zap.Int("guild_count", len(profile.MutualGuilds)),
		zap.Int("connections_found", len(discoveredConnections)))

	return profile.User.Username, discoveredConnections, nil
}

// HasGuildAccess checks if this scanner's account is in the specified guild.
func (s *Scanner) HasGuildAccess(guildID uint64) bool {
	guilds, err := s.state.Guilds()
	if err != nil {
		return false
	}

	return slices.ContainsFunc(guilds, func(guild discord.Guild) bool {
		return uint64(guild.ID) == guildID
	})
}

// FetchUserMessages fetches the first page of messages for a user in a specific guild.
func (s *Scanner) FetchUserMessages(ctx context.Context, guildID, userID uint64) ([]*ai.MessageContent, error) {
	var response MessageSearchResponse

	endpoint := fmt.Sprintf(
		"https://discord.com/api/v9/guilds/%d/messages/search?author_id=%d&sort_by=timestamp&sort_order=desc&offset=0&include_nsfw=true",
		guildID,
		userID,
	)

	// Wait for rate limit slot
	if err := s.rateLimiter.WaitForNextSlot(ctx); err != nil {
		return nil, err
	}

	// Fetch messages
	_, err := s.breaker.Execute(func() (any, error) {
		err := s.session.WithContext(ctx).RequestJSON(&response, "GET", endpoint)
		if err != nil {
			// Treat as a successful API call (guild no longer exists)
			if strings.Contains(err.Error(), "Unknown Guild") {
				s.logger.Warn("Cannot fetch messages from guild",
					zap.Uint64("guildID", guildID),
					zap.Uint64("userID", userID))

				return false, nil
			}

			return false, err
		}

		return true, nil
	})
	if err != nil {
		// Check for circuit breaker open state
		if errors.Is(err, gobreaker.ErrOpenState) {
			s.logger.Warn("Discord API circuit breaker is open, skipping message fetch",
				zap.Uint64("guildID", guildID),
				zap.Uint64("userID", userID))

			return nil, fmt.Errorf("%w: %w", ErrCircuitBreakerOpen, err)
		}

		return nil, fmt.Errorf("failed to fetch user messages: %w", err)
	}

	// Extract messages from the nested response structure
	messages := make([]*ai.MessageContent, 0)

	for _, messageGroup := range response.Messages {
		for _, msg := range messageGroup {
			messages = append(messages, &ai.MessageContent{
				MessageID: msg.ID,
				Content:   msg.Content,
			})
		}
	}

	s.logger.Info("Fetched user messages",
		zap.Uint64("guildID", guildID),
		zap.Uint64("userID", userID),
		zap.Int("message_count", len(messages)))

	return messages, nil
}

// AnalyzeAndFlagUser analyzes a Discord user's messages and flags their Roblox account if needed.
func (s *Scanner) AnalyzeAndFlagUser(
	ctx context.Context, userID uint64, guildIDs []uint64, robloxUserID int64, existingUser *types.ReviewUser, fetchedUserInfo *types.User,
) error {
	// Check if user meets condo server threshold (3+ mutual guilds)
	guildCount := len(guildIDs)
	meetsGuildThreshold := guildCount >= 3

	s.logger.Info("Evaluating user for flagging",
		zap.Uint64("discordUserID", userID),
		zap.Int64("robloxUserID", robloxUserID),
		zap.Int("guild_count", guildCount),
		zap.Bool("meets_guild_threshold", meetsGuildThreshold))

	// Skip message analysis if already meets guild threshold
	if meetsGuildThreshold {
		s.logger.Info("User meets guild threshold, skipping message analysis",
			zap.Uint64("discordUserID", userID),
			zap.Int64("robloxUserID", robloxUserID),
			zap.Int("guild_count", guildCount))

		// Calculate confidence based on guild count
		confidence := calculateCondoConfidence(guildCount)

		if err := s.flagRobloxAccount(ctx, userID, robloxUserID, guildCount, false, confidence, existingUser, fetchedUserInfo); err != nil {
			return fmt.Errorf("failed to flag roblox account: %w", err)
		}

		return nil
	}

	// Filter to only guilds this scanner can access
	accessibleGuilds := make([]uint64, 0, len(guildIDs))
	for _, guildID := range guildIDs {
		if s.HasGuildAccess(guildID) {
			accessibleGuilds = append(accessibleGuilds, guildID)
		}
	}

	// Track whether inappropriate messages were found
	foundInappropriateMessages := false

	// Analyze messages from accessible mutual guilds
	for _, guildID := range accessibleGuilds {
		// Fetch messages for this guild
		messages, err := s.FetchUserMessages(ctx, guildID, userID)
		if err != nil {
			s.logger.Warn("Failed to fetch messages for guild",
				zap.Uint64("guildID", guildID),
				zap.Uint64("userID", userID),
				zap.Error(err))

			continue
		}

		if len(messages) == 0 {
			continue
		}

		// Get guild name
		guildName := fmt.Sprintf("Guild_%d", guildID)

		serverInfos, err := s.db.Model().Sync().GetServerInfo(ctx, []uint64{guildID})
		if err == nil && len(serverInfos) > 0 {
			guildName = serverInfos[0].Name
		}

		// Analyze messages with AI
		flaggedUser, err := s.messageAnalyzer.ProcessMessages(ctx, guildID, guildID, guildName, userID, messages)
		if err != nil {
			s.logger.Error("Failed to analyze messages",
				zap.Uint64("guildID", guildID),
				zap.Uint64("userID", userID),
				zap.Error(err))

			continue
		}

		// Check if this user was flagged
		if flaggedUser != nil && len(flaggedUser.Messages) > 0 {
			s.logger.Info("User has inappropriate messages",
				zap.Uint64("discordUserID", userID),
				zap.Int64("robloxUserID", robloxUserID),
				zap.Uint64("guildID", guildID),
				zap.Int("flagged_message_count", len(flaggedUser.Messages)))

			// Store the inappropriate messages in the database
			now := time.Now()
			dbMessages := make([]*types.InappropriateMessage, 0, len(flaggedUser.Messages))

			for _, msg := range flaggedUser.Messages {
				dbMessages = append(dbMessages, &types.InappropriateMessage{
					ServerID:   guildID,
					UserID:     userID,
					MessageID:  msg.MessageID,
					Content:    msg.Content,
					Reason:     msg.Reason,
					Confidence: msg.Confidence,
					DetectedAt: now,
					UpdatedAt:  now,
				})
			}

			if err := s.db.Model().Message().BatchStoreInappropriateMessages(ctx, dbMessages); err != nil {
				s.logger.Error("Failed to store inappropriate messages",
					zap.Uint64("discordUserID", userID),
					zap.Error(err))
			}

			// Update user summary
			summary := &types.InappropriateUserSummary{
				UserID:       userID,
				Reason:       flaggedUser.Reason,
				MessageCount: len(flaggedUser.Messages),
				LastDetected: now,
				UpdatedAt:    now,
			}

			if err := s.db.Model().Message().BatchUpdateUserSummaries(ctx, []*types.InappropriateUserSummary{summary}); err != nil {
				s.logger.Error("Failed to update user summary",
					zap.Uint64("discordUserID", userID),
					zap.Error(err))
			}

			// Calculate user's confidence
			confidence := calculateMessageConfidence(flaggedUser.Messages)

			// Flag based on message content
			if err := s.flagRobloxAccount(ctx, userID, robloxUserID, guildCount, true, confidence, existingUser, fetchedUserInfo); err != nil {
				return fmt.Errorf("failed to flag roblox account: %w", err)
			}

			foundInappropriateMessages = true

			break // Stop analyzing once we find inappropriate messages
		}

		time.Sleep(1 * time.Second)
	}

	// If no inappropriate messages found, add user as mixed type
	if !foundInappropriateMessages {
		s.logger.Info("User passed message analysis, adding as mixed type",
			zap.Uint64("discordUserID", userID),
			zap.Int64("robloxUserID", robloxUserID),
			zap.Int("guild_count", guildCount))

		confidence := 0.4

		if err := s.flagRobloxAccount(ctx, userID, robloxUserID, guildCount, false, confidence, existingUser, fetchedUserInfo); err != nil {
			return fmt.Errorf("failed to add user as mixed type: %w", err)
		}
	}

	return nil
}

// flagRobloxAccount creates or updates a flagged user record for a Roblox account linked to a Discord user.
func (s *Scanner) flagRobloxAccount(
	ctx context.Context, discordUserID uint64, robloxUserID int64, guildCount int,
	hasInappropriateMessages bool, confidence float64, existingUser *types.ReviewUser, fetchedUserInfo *types.User,
) error {
	now := time.Now()

	// Determine status based on guild count and message content
	var status enum.UserType

	switch {
	case hasInappropriateMessages:
		status = enum.UserTypeFlagged
	case guildCount >= 3:
		status = enum.UserTypeFlagged
	default:
		status = enum.UserTypeMixed
	}

	// Create reason message based on what was detected
	var reasonMessage string
	if hasInappropriateMessages {
		reasonMessage = fmt.Sprintf(
			"Discord user with linked Roblox account detected in %d condo server(s) with inappropriate messages",
			guildCount,
		)
	} else {
		reasonMessage = fmt.Sprintf(
			"Discord user with linked Roblox account detected in %d+ condo servers",
			guildCount,
		)
	}

	// Create new condo reason
	condoReason := &types.Reason{
		Message:    reasonMessage,
		Confidence: confidence,
		Evidence: []string{
			fmt.Sprintf("Discord User ID: %d", discordUserID),
		},
	}

	var reviewUser *types.ReviewUser
	if existingUser != nil {
		// Merge condo reason with existing reasons
		reviewUser = existingUser

		reviewUser.Confidence = confidence
		reviewUser.LastUpdated = now

		if reviewUser.Reasons == nil {
			reviewUser.Reasons = make(types.Reasons[enum.UserReasonType])
		}

		reviewUser.Reasons.AddWithSource(enum.UserReasonTypeCondo, condoReason, "Discord")
	} else {
		// Create new user with condo reason and fetched user info
		newUser := &types.User{
			ID:            robloxUserID,
			UUID:          uuid.New(),
			Status:        status,
			Confidence:    confidence,
			EngineVersion: types.CurrentEngineVersion,
			LastScanned:   now,
			LastUpdated:   now,
			LastViewed:    now,
			LastBanCheck:  now,
		}

		// Populate user info from fetched data if available
		if fetchedUserInfo != nil {
			newUser.Name = fetchedUserInfo.Name
			newUser.DisplayName = fetchedUserInfo.DisplayName
			newUser.Description = fetchedUserInfo.Description
			newUser.CreatedAt = fetchedUserInfo.CreatedAt
		}

		reviewUser = &types.ReviewUser{
			User:    newUser,
			Reasons: make(types.Reasons[enum.UserReasonType]),
		}

		reviewUser.Reasons.AddWithSource(enum.UserReasonTypeCondo, condoReason, "Discord")
	}

	// Save the flagged user
	flaggedUsers := map[int64]*types.ReviewUser{
		robloxUserID: reviewUser,
	}

	if err := s.db.Service().User().SaveUsers(ctx, flaggedUsers); err != nil {
		return fmt.Errorf("failed to save flagged user: %w", err)
	}

	// Auto-confirm condo user
	if status == enum.UserTypeFlagged {
		if err := s.db.Service().User().ConfirmUsers(ctx, []*types.ReviewUser{reviewUser}, 0); err != nil {
			s.logger.Error("Failed to auto-confirm condo user",
				zap.Int64("robloxUserID", robloxUserID),
				zap.Error(err))
		}

		// Sync to D1 database as confirmed
		if err := s.cfClient.UserFlags.AddConfirmed(ctx, reviewUser, 0); err != nil {
			s.logger.Error("Failed to add confirmed user to D1",
				zap.Int64("robloxUserID", robloxUserID),
				zap.Error(err))
		}

		s.logger.Info("Successfully flagged and confirmed Roblox account",
			zap.Int64("robloxUserID", robloxUserID),
			zap.String("status", status.String()),
			zap.String("reasonType", enum.UserReasonTypeCondo.String()),
			zap.Float64("confidence", confidence))
	} else {
		// Sync to D1 database as mixed
		if err := s.cfClient.UserFlags.AddMixed(ctx, reviewUser); err != nil {
			s.logger.Error("Failed to add mixed user to D1",
				zap.Int64("robloxUserID", robloxUserID),
				zap.Error(err))
		}

		s.logger.Info("Successfully flagged Roblox account as mixed",
			zap.Int64("robloxUserID", robloxUserID),
			zap.String("status", status.String()),
			zap.String("reasonType", enum.UserReasonTypeCondo.String()),
			zap.Float64("confidence", confidence))
	}

	return nil
}

// processRobloxConnection stores and processes a discovered Roblox connection.
func (s *Scanner) processRobloxConnection(
	ctx context.Context, discordUserID uint64, connection *types.DiscordRobloxConnection, guildIDs []uint64,
) {
	// Store the connection
	if err := s.db.Model().Sync().UpsertDiscordRobloxConnection(ctx, connection); err != nil {
		s.logger.Error("Failed to store Roblox connection",
			zap.Uint64("discordUserID", discordUserID),
			zap.Int64("robloxUserID", connection.RobloxUserID),
			zap.Error(err))

		return
	}

	s.logger.Info("Stored Roblox connection",
		zap.Uint64("discordUserID", discordUserID),
		zap.Int64("robloxUserID", connection.RobloxUserID),
		zap.String("robloxUsername", connection.RobloxUsername))

	// Check if Roblox account already exists in the system
	existingUser, err := s.db.Service().User().GetUserByID(
		ctx,
		strconv.FormatInt(connection.RobloxUserID, 10),
		types.UserFieldBasic|types.UserFieldProfile|types.UserFieldStats|types.UserFieldReasons,
	)
	if err == nil {
		// Check if user already has condo reason
		if condoReason := existingUser.Reasons[enum.UserReasonTypeCondo]; condoReason != nil {
			guildCount := len(guildIDs)

			// Only update if guild count meets threshold
			if guildCount >= 3 {
				confidence := calculateCondoConfidence(guildCount)
				if err := s.flagRobloxAccount(ctx, discordUserID, connection.RobloxUserID, guildCount, false, confidence, existingUser, existingUser.User); err != nil {
					s.logger.Error("Failed to update condo reason",
						zap.Uint64("discordUserID", discordUserID),
						zap.Int64("robloxUserID", connection.RobloxUserID),
						zap.Error(err))
				}
			}

			return
		}

		// User exists but doesn't have condo reason, proceed with full analysis
		s.logger.Info("Roblox account exists without condo reason, performing analysis",
			zap.Uint64("discordUserID", discordUserID),
			zap.Int64("robloxUserID", connection.RobloxUserID))

		// Analyze with existing user info
		if err := s.AnalyzeAndFlagUser(ctx, discordUserID, guildIDs, connection.RobloxUserID, existingUser, existingUser.User); err != nil {
			s.logger.Error("Failed to analyze and flag user",
				zap.Uint64("discordUserID", discordUserID),
				zap.Int64("robloxUserID", connection.RobloxUserID),
				zap.Error(err))
		}

		return
	}

	// Fetch Roblox user info for new user
	fetchedUserInfo, err := s.fetchRobloxUserInfo(ctx, connection.RobloxUserID)
	if err != nil {
		s.logger.Error("Failed to fetch Roblox user info for new user",
			zap.Uint64("discordUserID", discordUserID),
			zap.Int64("robloxUserID", connection.RobloxUserID),
			zap.Error(err))

		return
	}

	// Analyze user's messages and flag Roblox account if needed
	if err := s.AnalyzeAndFlagUser(ctx, discordUserID, guildIDs, connection.RobloxUserID, nil, fetchedUserInfo); err != nil {
		s.logger.Error("Failed to analyze and flag user",
			zap.Uint64("discordUserID", discordUserID),
			zap.Int64("robloxUserID", connection.RobloxUserID),
			zap.Error(err))
	}
}

// calculateCondoConfidence calculates confidence score based on number of condo servers.
func calculateCondoConfidence(guildCount int) float64 {
	switch {
	case guildCount >= 5:
		return 0.95
	case guildCount == 4:
		return 0.90
	case guildCount == 3:
		return 0.85
	case guildCount == 2:
		return 0.40
	case guildCount == 1:
		return 0.30
	default:
		return 0.0
	}
}

// calculateMessageConfidence calculates average confidence from flagged messages.
func calculateMessageConfidence(messages []ai.FlaggedMessage) float64 {
	if len(messages) == 0 {
		return 0.0
	}

	var totalConfidence float64
	for _, msg := range messages {
		totalConfidence += msg.Confidence
	}

	return totalConfidence / float64(len(messages))
}

// fetchRobloxUserInfo retrieves basic Roblox user information from the API.
func (s *Scanner) fetchRobloxUserInfo(ctx context.Context, robloxUserID int64) (*types.User, error) {
	userInfo, err := s.roAPI.Users().GetUserByID(ctx, robloxUserID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch Roblox user info: %w", err)
	}

	// Skip banned or deleted users
	if userInfo.IsBanned {
		return nil, ErrUserBanned
	}

	normalizer := utils.NewTextNormalizer()
	now := time.Now()

	return &types.User{
		ID:           userInfo.ID,
		Name:         normalizer.Normalize(userInfo.Name),
		DisplayName:  normalizer.Normalize(userInfo.DisplayName),
		Description:  normalizer.Normalize(userInfo.Description),
		CreatedAt:    userInfo.Created,
		LastUpdated:  now,
		LastBanCheck: now,
	}, nil
}
