package discord

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/diamondburned/arikawa/v3/session"
	"github.com/google/uuid"
	"github.com/redis/rueidis"
	"github.com/robalyx/rotector/internal/ai"
	"github.com/robalyx/rotector/internal/cloudflare"
	"github.com/robalyx/rotector/internal/database"
	"github.com/robalyx/rotector/internal/database/types"
	"github.com/robalyx/rotector/internal/database/types/enum"
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

// Scanner handles full guild scanning for Discord users.
type Scanner struct {
	db              database.Client
	cfClient        *cloudflare.Client
	ratelimit       rueidis.Client
	session         *session.Session
	messageAnalyzer *ai.MessageAnalyzer
	logger          *zap.Logger
}

// NewScanner creates a new full scan handler.
func NewScanner(
	db database.Client,
	cfClient *cloudflare.Client,
	ratelimit rueidis.Client,
	session *session.Session,
	messageAnalyzer *ai.MessageAnalyzer,
	logger *zap.Logger,
) *Scanner {
	return &Scanner{
		db:              db,
		cfClient:        cfClient,
		ratelimit:       ratelimit,
		session:         session,
		messageAnalyzer: messageAnalyzer,
		logger:          logger.Named("discord_scanner"),
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
		errStr := err.Error()
		if strings.Contains(errStr, "Unknown User") {
			if deleteErr := s.db.Model().Sync().DeleteUserGuildMemberships(ctx, userID); deleteErr != nil {
				s.logger.Error("Failed to delete user records",
					zap.Uint64("userID", userID),
					zap.Error(deleteErr))

				return "", fmt.Errorf("failed to delete user records: %w", deleteErr)
			}

			s.logger.Info("Successfully cleaned up deleted Discord user",
				zap.Uint64("userID", userID))

			return "", nil
		}

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

	// Extract and store Roblox connection if present
	for _, account := range profile.ConnectedAccounts {
		if account.Type == "roblox" {
			// Only process verified connections
			if !account.Verified {
				continue
			}

			robloxUserID, err := strconv.ParseInt(account.ID, 10, 64)
			if err != nil {
				s.logger.Warn("Failed to parse Roblox user ID",
					zap.String("id", account.ID),
					zap.Error(err))

				continue
			}

			connection := &types.DiscordRobloxConnection{
				DiscordUserID:  userID,
				RobloxUserID:   robloxUserID,
				RobloxUsername: account.Name,
				Verified:       account.Verified,
				DetectedAt:     now,
				UpdatedAt:      now,
			}

			if err := s.db.Model().Sync().UpsertDiscordRobloxConnection(ctx, connection); err != nil {
				s.logger.Error("Failed to store Roblox connection",
					zap.Uint64("discordUserID", userID),
					zap.Int64("robloxUserID", robloxUserID),
					zap.Error(err))
			} else {
				s.logger.Info("Stored Roblox connection",
					zap.Uint64("discordUserID", userID),
					zap.Int64("robloxUserID", robloxUserID),
					zap.String("robloxUsername", account.Name))

				// Check if Roblox account already exists in the system
				existingUser, err := s.db.Service().User().GetUserByID(
					ctx,
					strconv.FormatInt(robloxUserID, 10),
					types.UserFieldID|types.UserFieldStatus|types.UserFieldReasons,
				)
				if err == nil {
					// Check if user already has condo reason
					if condoReason := existingUser.Reasons[enum.UserReasonTypeCondo]; condoReason != nil {
						guildCount := len(profile.MutualGuilds)

						// Only update if guild count meets threshold
						if guildCount >= 3 {
							confidence := calculateCondoConfidence(guildCount)
							if err := s.flagRobloxAccount(ctx, userID, robloxUserID, guildCount, false, confidence, existingUser); err != nil {
								s.logger.Error("Failed to update condo reason",
									zap.Uint64("discordUserID", userID),
									zap.Int64("robloxUserID", robloxUserID),
									zap.Error(err))
							}
						}

						continue
					}

					// User exists but doesn't have condo reason, proceed with full analysis
					s.logger.Info("Roblox account exists without condo reason, performing analysis",
						zap.Uint64("discordUserID", userID),
						zap.Int64("robloxUserID", robloxUserID))
				}

				// Analyze user's messages and flag Roblox account if needed
				if err := s.AnalyzeAndFlagUser(ctx, userID, &profile, robloxUserID, existingUser); err != nil {
					s.logger.Error("Failed to analyze and flag user",
						zap.Uint64("discordUserID", userID),
						zap.Int64("robloxUserID", robloxUserID),
						zap.Error(err))
				}
			}
		}
	}

	s.logger.Info("Full scan complete",
		zap.Uint64("userID", userID),
		zap.Int("guild_count", len(profile.MutualGuilds)))

	return profile.User.Username, nil
}

// ShouldScan checks if a user is eligible for scanning based on rate limits.
func (s *Scanner) ShouldScan(ctx context.Context, userID uint64) bool {
	exists, err := s.ratelimit.Do(ctx, s.ratelimit.B().Exists().Key("mutual_guilds_lookup").Build()).ToInt64()
	if err != nil {
		s.logger.Error("Failed to check rate limit",
			zap.Error(err),
			zap.Uint64("userID", userID))

		return false
	}

	return exists == 0
}

// FetchUserMessages fetches the first page of messages for a user in a specific guild.
func (s *Scanner) FetchUserMessages(guildID, userID uint64) ([]*ai.MessageContent, error) {
	var response MessageSearchResponse

	endpoint := fmt.Sprintf(
		"https://discord.com/api/v9/guilds/%d/messages/search?author_id=%d&sort_by=timestamp&sort_order=desc&offset=0&include_nsfw=true",
		guildID,
		userID,
	)

	err := s.session.RequestJSON(&response, "GET", endpoint)
	if err != nil {
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
	ctx context.Context, userID uint64, profile *UserProfile, robloxUserID int64, existingUser *types.ReviewUser,
) error {
	// Check if user meets condo server threshold (3+ mutual guilds)
	guildCount := len(profile.MutualGuilds)
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

		if err := s.flagRobloxAccount(ctx, userID, robloxUserID, guildCount, false, confidence, existingUser); err != nil {
			return fmt.Errorf("failed to flag roblox account: %w", err)
		}

		return nil
	}

	// Analyze messages from each mutual guild
	for _, guild := range profile.MutualGuilds {
		guildID, err := strconv.ParseUint(guild.ID, 10, 64)
		if err != nil {
			s.logger.Warn("Failed to parse guild ID",
				zap.String("guildID", guild.ID),
				zap.Error(err))

			continue
		}

		// Fetch messages for this guild
		messages, err := s.FetchUserMessages(guildID, userID)
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
			if err := s.flagRobloxAccount(ctx, userID, robloxUserID, guildCount, true, confidence, existingUser); err != nil {
				return fmt.Errorf("failed to flag roblox account: %w", err)
			}

			break // Stop analyzing once we find inappropriate messages
		}

		time.Sleep(1 * time.Second)
	}

	return nil
}

// flagRobloxAccount creates or updates a flagged user record for a Roblox account linked to a Discord user.
func (s *Scanner) flagRobloxAccount(
	ctx context.Context, discordUserID uint64, robloxUserID int64, guildCount int,
	hasInappropriateMessages bool, confidence float64, existingUser *types.ReviewUser,
) error {
	now := time.Now()

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

		reviewUser.Reasons[enum.UserReasonTypeCondo] = condoReason
	} else {
		// Create new user with only condo reason
		reviewUser = &types.ReviewUser{
			User: &types.User{
				ID:            robloxUserID,
				UUID:          uuid.New(),
				Status:        enum.UserTypeFlagged,
				Confidence:    confidence,
				EngineVersion: types.CurrentEngineVersion,
				LastScanned:   now,
				LastUpdated:   now,
				LastViewed:    now,
				LastBanCheck:  now,
			},
			Reasons: types.Reasons[enum.UserReasonType]{
				enum.UserReasonTypeCondo: condoReason,
			},
		}
	}

	// Save the flagged user
	flaggedUsers := map[int64]*types.ReviewUser{
		robloxUserID: reviewUser,
	}

	if err := s.db.Service().User().SaveUsers(ctx, flaggedUsers); err != nil {
		return fmt.Errorf("failed to save flagged user: %w", err)
	}

	// Auto-confirm condo user
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
		zap.String("reasonType", enum.UserReasonTypeCondo.String()),
		zap.Float64("confidence", confidence))

	return nil
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
	default: // Below minimum threshold
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
