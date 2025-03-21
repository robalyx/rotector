package events

import (
	"context"
	"strings"
	"time"

	"github.com/diamondburned/arikawa/v3/discord"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/common/client/ai"
	"github.com/robalyx/rotector/internal/common/storage/database/types"
	"go.uber.org/zap"
)

const (
	// similarityThreshold is the threshold for considering messages too similar (spam).
	similarityThreshold = 0.85
	// maxRecentMessagesToCheck is the maximum number of recent messages to check against.
	maxRecentMessagesToCheck = 20
	// maxMessageLength is the maximum length of a message to process to avoid spam.
	maxMessageLength = 500
)

// addMessageToQueue adds a message to the processing queue if it passes checks.
func (h *Handler) addMessageToQueue(message *discord.Message) {
	serverID := uint64(message.GuildID)
	channelID := uint64(message.ChannelID)
	userID := uint64(message.Author.ID)
	messageID := message.ID.String()
	content := message.Content

	// Skip empty messages
	if strings.TrimSpace(content) == "" {
		return
	}

	// Skip messages that are too long (likely spam)
	if len(content) > maxMessageLength {
		h.logger.Debug("Skipping long message (likely spam)",
			zap.Uint64("server_id", serverID),
			zap.Uint64("channel_id", channelID),
			zap.Uint64("user_id", userID),
			zap.String("message_id", messageID),
			zap.Int("length", len(content)),
			zap.String("sample", content[:min(len(content), 50)]))
		return
	}

	// Check privacy status
	isRedacted, isWhitelisted, err := h.db.Service().Sync().ShouldSkipUser(context.Background(), userID)
	if err != nil {
		h.logger.Error("Failed to check user privacy status",
			zap.Uint64("user_id", userID),
			zap.Error(err))
		return
	}

	if isRedacted || isWhitelisted {
		h.logger.Debug("Skipping message from redacted/whitelisted user",
			zap.Uint64("user_id", userID))
		return
	}

	h.messageMu.Lock()
	defer h.messageMu.Unlock()

	// Initialize guild messages map if needed
	if h.guildMessages == nil {
		h.guildMessages = make(map[uint64][]*ai.MessageContent)
	}

	// Create a composite key for the channel within the server
	channelKey := (serverID << 32) | channelID

	// Check if the channel has a message queue
	if _, exists := h.guildMessages[channelKey]; !exists {
		h.guildMessages[channelKey] = []*ai.MessageContent{}
	}

	// Check if we already have this message in the queue
	for _, msg := range h.guildMessages[channelKey] {
		if msg.MessageID == messageID {
			return
		}

		// Check for very similar content to prevent spam
		similarity := calculateStringSimilarity(msg.Content, content)
		if similarity > similarityThreshold {
			h.logger.Debug("Skipping similar message",
				zap.Uint64("server_id", serverID),
				zap.Uint64("channel_id", channelID),
				zap.Uint64("user_id", userID),
				zap.String("message_id", messageID),
				zap.Float64("similarity", similarity),
				zap.String("sample", content[:min(len(content), 50)]))
			return
		}
	}

	// Check if similar message has been recently processed and stored in the database
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	hasSimilarMessage, err := h.checkRecentSimilarMessages(ctx, serverID, userID, content)
	if err != nil {
		h.logger.Error("Failed to check for similar messages",
			zap.Uint64("server_id", serverID),
			zap.Uint64("channel_id", channelID),
			zap.Uint64("user_id", userID),
			zap.Error(err))
	} else if hasSimilarMessage {
		h.logger.Debug("Skipping message with similar content already in database",
			zap.Uint64("server_id", serverID),
			zap.Uint64("channel_id", channelID),
			zap.Uint64("user_id", userID),
			zap.String("message_id", messageID))
		return
	}

	// Add the message to the queue
	h.guildMessages[channelKey] = append(h.guildMessages[channelKey], &ai.MessageContent{
		MessageID: messageID,
		UserID:    userID,
		Content:   content,
	})

	// Log queued message
	h.logger.Debug("Added message to queue",
		zap.Uint64("server_id", serverID),
		zap.Uint64("channel_id", channelID),
		zap.Uint64("user_id", userID),
		zap.String("message_id", messageID),
		zap.String("sample", content[:min(len(content), 50)]))

	// Process messages if we've reached the batch limit for this channel
	if len(h.guildMessages[channelKey]) >= h.channelThreshold {
		go h.processChannelMessages(serverID, channelID, channelKey)
	}
}

// checkRecentSimilarMessages checks if a similar message was recently processed from the same user in the same server.
func (h *Handler) checkRecentSimilarMessages(ctx context.Context, serverID, userID uint64, content string) (bool, error) {
	// Get recent messages from this user in this server
	messages, err := h.db.Model().Message().GetUserInappropriateMessages(ctx, serverID, userID, maxRecentMessagesToCheck)
	if err != nil {
		return false, err
	}

	// Look for messages with similar content
	for _, msg := range messages {
		similarity := calculateStringSimilarity(msg.Content, content)
		if similarity > similarityThreshold {
			return true, nil
		}
	}

	return false, nil
}

// processChannelMessages processes a channel's queued messages.
func (h *Handler) processChannelMessages(serverID, channelID, channelKey uint64) {
	h.messageMu.Lock()

	// Get the channel's messages and clear the queue
	messages := h.guildMessages[channelKey]
	if len(messages) == 0 {
		h.messageMu.Unlock()
		return
	}
	delete(h.guildMessages, channelKey)
	h.messageMu.Unlock()

	// Get server info
	ctx := context.Background()
	serverInfo, err := h.getOrCreateServerInfo(ctx, serverID)
	if err != nil {
		h.logger.Error("Failed to get server info",
			zap.Uint64("server_id", serverID),
			zap.Uint64("channel_id", channelID),
			zap.Error(err))
		return
	}

	// Create batch of server members to upsert
	now := time.Now()
	members := make([]*types.DiscordServerMember, 0, len(messages))
	uniqueUsers := make(map[uint64]struct{})

	for _, message := range messages {
		if _, exists := uniqueUsers[message.UserID]; exists {
			continue
		}

		members = append(members, &types.DiscordServerMember{
			ServerID:  serverID,
			UserID:    message.UserID,
			UpdatedAt: now,
		})
		uniqueUsers[message.UserID] = struct{}{}
	}

	// Batch upsert server members
	if err := h.db.Model().Sync().UpsertServerMembers(ctx, members, false); err != nil {
		h.logger.Error("Failed to upsert server members",
			zap.Uint64("server_id", serverID),
			zap.Error(err))
	}

	// Create condo bans for all users
	userIDs := make([]uint64, 0, len(uniqueUsers))
	for userID := range uniqueUsers {
		userIDs = append(userIDs, userID)
	}
	h.db.Service().Ban().CreateCondoBans(ctx, userIDs)

	// Process the messages with AI
	flaggedUsers, err := h.messageAnalyzer.ProcessMessages(ctx, serverID, channelID, serverInfo.Name, messages)
	if err != nil {
		h.logger.Error("Failed to process messages with AI",
			zap.Uint64("server_id", serverID),
			zap.Uint64("channel_id", channelID),
			zap.Error(err))
		return
	}

	// If we have flagged users, store them in the database
	if len(flaggedUsers) > 0 {
		if err := h.storeInappropriateMessages(ctx, serverID, channelID, flaggedUsers); err != nil {
			h.logger.Error("Failed to store inappropriate messages",
				zap.Uint64("server_id", serverID),
				zap.Uint64("channel_id", channelID),
				zap.Error(err))
		}
	}
}

// getOrCreateServerInfo retrieves or creates server info for a given server ID.
func (h *Handler) getOrCreateServerInfo(ctx context.Context, serverID uint64) (*types.DiscordServerInfo, error) {
	// Try to get existing server info
	serverInfos, err := h.db.Model().Sync().GetServerInfo(ctx, []uint64{serverID})
	if err != nil {
		return nil, err
	}

	// If server info exists, return it
	if len(serverInfos) > 0 {
		return serverInfos[0], nil
	}

	// Server info doesn't exist, try to get it from Discord
	guild, err := h.state.Guild(discord.GuildID(serverID))
	if err != nil {
		// If we can't get the guild info, create a temporary record with placeholder name
		serverInfo := &types.DiscordServerInfo{
			ServerID:  serverID,
			Name:      constants.UnknownServer,
			UpdatedAt: time.Now(),
		}

		// Store the server info
		if err := h.db.Model().Sync().UpsertServerInfo(ctx, serverInfo); err != nil {
			return nil, err
		}

		h.logger.Warn("Created server info with placeholder name",
			zap.Uint64("server_id", serverID))
		return serverInfo, nil
	}

	// Create server info with actual guild name
	serverInfo := &types.DiscordServerInfo{
		ServerID:  serverID,
		Name:      guild.Name,
		UpdatedAt: time.Now(),
	}

	// Store the server info
	if err := h.db.Model().Sync().UpsertServerInfo(ctx, serverInfo); err != nil {
		return nil, err
	}

	h.logger.Debug("Created server info",
		zap.Uint64("server_id", serverID),
		zap.String("name", guild.Name))
	return serverInfo, nil
}

// storeInappropriateMessages stores flagged messages in the database and updates user summaries.
func (h *Handler) storeInappropriateMessages(
	ctx context.Context, serverID uint64, channelID uint64, flaggedUsers map[uint64]*ai.FlaggedMessageUser,
) error {
	var messages []*types.InappropriateMessage
	summaries := make([]*types.InappropriateUserSummary, 0, len(flaggedUsers))
	now := time.Now()

	// Process each flagged user
	for userID, flaggedUser := range flaggedUsers {
		// Create a summary for this user
		summary := &types.InappropriateUserSummary{
			UserID:       userID,
			Reason:       flaggedUser.Reason,
			MessageCount: len(flaggedUser.Messages),
			LastDetected: now,
			UpdatedAt:    now,
		}
		summaries = append(summaries, summary)

		// Create a database record for each flagged message
		for _, message := range flaggedUser.Messages {
			messages = append(messages, &types.InappropriateMessage{
				ServerID:   serverID,
				ChannelID:  channelID,
				UserID:     userID,
				MessageID:  message.MessageID,
				Content:    message.Content,
				Reason:     message.Reason,
				Confidence: message.Confidence,
				DetectedAt: now,
				UpdatedAt:  now,
			})
		}
	}

	// Store the messages in the database
	if err := h.db.Model().Message().BatchStoreInappropriateMessages(ctx, messages); err != nil {
		return err
	}

	// Update user summaries
	if err := h.db.Model().Message().BatchUpdateUserSummaries(ctx, summaries); err != nil {
		return err
	}

	h.logger.Info("Stored inappropriate messages",
		zap.Uint64("server_id", serverID),
		zap.Int("user_count", len(flaggedUsers)),
		zap.Int("message_count", len(messages)))
	return nil
}
