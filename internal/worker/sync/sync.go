package sync

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/diamondburned/arikawa/v3/discord"
	"github.com/diamondburned/arikawa/v3/gateway"
	"github.com/robalyx/rotector/internal/database/types"
	"github.com/robalyx/rotector/internal/discord/memberstate"
	"go.uber.org/zap"
)

const (
	maxChannelAttempts           = 15
	maxConsecutiveNoProgressIter = 3
	syncTimeout                  = 5 * time.Minute
)

// syncCycle attempts to sync all servers.
func (w *Worker) syncCycle(ctx context.Context) error {
	// Get all guilds
	guilds, err := w.state.Guilds()
	if err != nil {
		return fmt.Errorf("failed to get initial guilds: %w", err)
	}

	// Track total member counts for reporting
	totalMembers := 0
	successfulGuilds := 0
	failedGuilds := 0
	now := time.Now()

	for i, guild := range guilds {
		// Check if context was cancelled
		select {
		case <-ctx.Done():
			w.logger.Info("Context cancelled during guild sync")
			return ctx.Err()
		default:
		}

		// Print progress
		progress := (i * 100) / len(guilds)

		guildName := guild.Name
		if len(guildName) > 15 {
			guildName = guildName[:15] + "..."
		}

		w.bar.SetStepMessage(fmt.Sprintf("Syncing %s (%d/%d) [%d OK, %d failed]",
			guildName, i+1, len(guilds), successfulGuilds, failedGuilds), int64(progress))
		w.reporter.UpdateStatus("Syncing guilds", progress)
		w.logger.Debug("Syncing guild", zap.String("name", guild.Name), zap.Uint64("id", uint64(guild.ID)))

		// Store server info for this guild
		serverInfo := &types.DiscordServerInfo{
			ServerID:  uint64(guild.ID),
			Name:      guild.Name,
			UpdatedAt: now,
		}

		if err := w.db.Model().Sync().UpsertServerInfo(ctx, serverInfo); err != nil {
			w.logger.Error("Failed to update server info",
				zap.String("name", guild.Name),
				zap.Uint64("id", uint64(guild.ID)),
				zap.Error(err))
			// Continue to next guild even if server info update fails
			failedGuilds++

			continue
		}

		// Request all members for this guild
		members, err := w.syncServerMembers(ctx, guild.ID)
		if err != nil {
			w.logger.Error("Failed to sync guild members",
				zap.String("name", guild.Name),
				zap.Uint64("id", uint64(guild.ID)),
				zap.Error(err))

			// We still continue to the next guild, but record this as a failure
			failedGuilds++

			// If we got partial results, we'll still try to save them
			if len(members) == 0 {
				continue
			}

			// Log that we're still adding partial results
			w.logger.Info("Adding partial member results despite sync error",
				zap.String("guild_name", guild.Name),
				zap.Uint64("guildID", uint64(guild.ID)),
				zap.Int("partial_member_count", len(members)))
		}

		w.logger.Debug("Adding members to database",
			zap.String("guild_name", guild.Name),
			zap.Uint64("guildID", uint64(guild.ID)),
			zap.Int("member_count", len(members)))

		// Batch update members for this guild
		if err := w.db.Model().Sync().UpsertServerMembers(ctx, members, false); err != nil {
			w.logger.Error("Failed to batch update members",
				zap.String("guild_name", guild.Name),
				zap.Uint64("guildID", uint64(guild.ID)),
				zap.Int("member_count", len(members)),
				zap.Error(err))

			failedGuilds++

			continue
		}

		totalMembers += len(members)
		successfulGuilds++
	}

	// Get total unique members in database for reporting
	uniqueUserCount, err := w.db.Model().Sync().GetUniqueUserCount(ctx)
	if err != nil {
		w.logger.Warn("Failed to get unique user count", zap.Error(err))
	} else {
		w.logger.Info("Member sync statistics",
			zap.Int("members_seen_this_cycle", totalMembers),
			zap.Int("total_unique_members_in_db", uniqueUserCount),
			zap.Int("guilds_processed", len(guilds)),
			zap.Int("guilds_successful", successfulGuilds),
			zap.Int("guilds_failed", failedGuilds))
	}

	w.bar.SetStepMessage(fmt.Sprintf("Synced %d servers (%d failed)", successfulGuilds, failedGuilds), 100)
	w.reporter.UpdateStatus(fmt.Sprintf("Sync complete: %d OK, %d failed", successfulGuilds, failedGuilds), 100)

	return nil
}

// syncServerMembers gets all members for a guild using the lazy member list approach.
func (w *Worker) syncServerMembers(ctx context.Context, guildID discord.GuildID) ([]*types.DiscordServerMember, error) {
	now := time.Now()

	attemptedChannels := make(map[discord.ChannelID]struct{})

	var (
		lastError  error
		allMembers []*types.DiscordServerMember
	)

	for attempt := range maxChannelAttempts {
		targetChannel, err := w.findTextChannel(guildID, attemptedChannels)
		if err != nil {
			lastError = err
			break
		}

		attemptedChannels[targetChannel] = struct{}{}

		w.logger.Debug("Trying member sync with channel",
			zap.String("guildID", guildID.String()),
			zap.String("channelID", targetChannel.String()),
			zap.Int("attempt", attempt+1),
			zap.Int("max_attempts", maxChannelAttempts))

		if attempt > 0 {
			thinkingDelay := w.getRandomDelay(2*time.Second, 5*time.Second)
			w.logger.Debug("Waiting before trying new channel",
				zap.Duration("delay", thinkingDelay))
			time.Sleep(thinkingDelay)
		}

		w.memberState.RequestMemberList(ctx, guildID, targetChannel, 0)

		time.Sleep(1 * time.Second)

		members, err := w.syncMemberChunks(ctx, guildID, targetChannel, now)

		if err == nil || !errors.Is(err, ErrListNotFoundRetry) {
			return members, err
		}

		lastError = err

		allMembers = append(allMembers, members...)

		w.logger.Info("Retrying member sync with different channel",
			zap.String("guildID", guildID.String()),
			zap.String("previous_channel", targetChannel.String()),
			zap.Int("members_so_far", len(allMembers)),
			zap.Int("attempt", attempt+1))

		if attempt < maxChannelAttempts-1 {
			channelSwitchDelay := w.getRandomDelay(5*time.Second, 10*time.Second)
			w.logger.Debug("Waiting before trying next channel",
				zap.Duration("delay", channelSwitchDelay))
			time.Sleep(channelSwitchDelay)
		}
	}

	if len(allMembers) > 0 {
		return allMembers, nil
	}

	return nil, fmt.Errorf("failed to sync guild members after %d channel attempts: %w", maxChannelAttempts, lastError)
}

// findTextChannel locates a suitable text channel in the guild for member list requests.
// The attemptedChannels map tracks channels that have already been tried to avoid repetition.
func (w *Worker) findTextChannel(
	guildID discord.GuildID, attemptedChannels map[discord.ChannelID]struct{},
) (discord.ChannelID, error) {
	channels, err := w.state.Channels(guildID)
	if err != nil {
		return 0, fmt.Errorf("failed to get guild channels: %w", err)
	}

	// Get bot's user ID for permission checks
	botUserID := w.state.Ready().User.ID

	// Filter for text channels that the bot can view
	textChannels := make([]discord.Channel, 0, len(channels))
	for _, channel := range channels {
		if channel.Type != discord.GuildText {
			continue
		}

		// Skip channels that were already attempted
		if _, attempted := attemptedChannels[channel.ID]; attempted {
			continue
		}

		// Check if bot has VIEW_CHANNEL permission
		perms, err := w.state.Permissions(channel.ID, botUserID)
		if err != nil {
			w.logger.Debug("Failed to check permissions for channel",
				zap.String("channelID", channel.ID.String()),
				zap.Error(err))
			attemptedChannels[channel.ID] = struct{}{}

			continue
		}

		if !perms.Has(discord.PermissionViewChannel) {
			attemptedChannels[channel.ID] = struct{}{}
			continue
		}

		textChannels = append(textChannels, channel)
	}

	channels = textChannels

	if len(channels) == 0 {
		return 0, ErrNoTextChannel
	}

	// Priority channel names that typically contain full member lists
	priorityNames := []string{
		"general",
		"main",
		"announce",
		"welcome",
		"lobby",
		"chat",
		"lounge",
		"hangout",
		"discuss",
		"community",
	}

	// First pass: check for priority channels by name
	for _, channel := range channels {
		// Skip channels that are already attempted
		if _, attempted := attemptedChannels[channel.ID]; attempted {
			continue
		}

		// Check if the channel name contains any priority name
		channelName := strings.ToLower(channel.Name)
		for _, priorityName := range priorityNames {
			if strings.Contains(channelName, priorityName) {
				w.logger.Debug("Selected priority channel for member list",
					zap.String("guildID", guildID.String()),
					zap.String("channelID", channel.ID.String()),
					zap.String("channel_name", channel.Name))

				return channel.ID, nil
			}
		}
	}

	// Second pass: find the most suitable channel based on recent activity
	var (
		mostSuitableChannel discord.ChannelID
		highestMessageID    discord.MessageID
	)

	for _, channel := range channels {
		// Skip channels that are already attempted
		if _, attempted := attemptedChannels[channel.ID]; attempted {
			continue
		}

		// If this is the first valid channel or it has a more recent message
		if mostSuitableChannel == 0 || channel.LastMessageID > highestMessageID {
			mostSuitableChannel = channel.ID
			highestMessageID = channel.LastMessageID
		}
	}

	// If we found a suitable channel
	if mostSuitableChannel != 0 {
		w.logger.Debug("Selected active channel for member list",
			zap.String("guildID", guildID.String()),
			zap.String("channelID", mostSuitableChannel.String()),
			zap.String("messageID", highestMessageID.String()))

		return mostSuitableChannel, nil
	}

	// If all channels were attempted or unsuitable
	if len(attemptedChannels) == len(channels) {
		return 0, ErrAllChannelsAttempted
	}

	// Last resort: just pick the first non-attempted channel
	for _, channel := range channels {
		if _, attempted := attemptedChannels[channel.ID]; !attempted {
			w.logger.Warn("Falling back to first available channel for member list",
				zap.String("guildID", guildID.String()),
				zap.String("channelID", channel.ID.String()))

			return channel.ID, nil
		}
	}

	return 0, ErrNoTextChannel
}

// syncMemberChunks handles the main sync loop, retrieving member chunks and processing them.
func (w *Worker) syncMemberChunks(
	ctx context.Context, guildID discord.GuildID, channelID discord.ChannelID, now time.Time,
) ([]*types.DiscordServerMember, error) {
	var (
		allMembers   []*types.DiscordServerMember
		membersMutex sync.Mutex
		wg           sync.WaitGroup
	)

	processed := make(map[uint64]struct{})
	processedMutex := sync.Mutex{}

	timeout := time.After(syncTimeout)
	lastMaxChunk := -1
	consecutiveNoProgress := 0

	consecutiveListNotFound := 0
	maxConsecutiveListNotFound := w.getRandomRetryCount()

	w.logger.Debug("Starting member chunk sync",
		zap.String("guildID", guildID.String()),
		zap.String("channelID", channelID.String()))

	defer wg.Wait()

	for {
		select {
		case <-timeout:
			return allMembers, fmt.Errorf("timeout while syncing member list: %w", ErrTimeout)
		case <-ctx.Done():
			w.logger.Info("Context cancelled during member sync")
			return allMembers, ctx.Err()
		default:
			// Get current member list state
			list, err := w.memberState.GetMemberList(guildID, channelID)
			if err != nil {
				if errors.Is(err, memberstate.ErrListNotFound) {
					shouldContinue, handleErr := w.handleMemberListNotFound(
						ctx, guildID, channelID, &consecutiveListNotFound, maxConsecutiveListNotFound)
					if !shouldContinue {
						return allMembers, handleErr
					}

					continue
				}

				w.logger.Error("Failed to get member list",
					zap.String("guildID", guildID.String()),
					zap.Error(err))

				return allMembers, fmt.Errorf("failed to get member list: %w", err)
			}

			// Reset the consecutive failure counter since we got a successful response
			consecutiveListNotFound = 0

			currentMaxChunk := list.MaxChunk()

			if currentMaxChunk != lastMaxChunk {
				wg.Add(1)

				// Process chunk asynchronously to avoid blocking the main loop.
				go func(memberList *memberstate.List) {
					defer wg.Done()

					// Collect potential members with deduplication
					processedMutex.Lock()

					potentialMembers := w.collectPotentialMembers(memberList, processed, guildID)

					processedMutex.Unlock()

					if len(potentialMembers) == 0 {
						return
					}

					// Check database and apply filters
					newMembers := w.filterAndCheckMembers(ctx, potentialMembers, guildID, now)

					if len(newMembers) > 0 {
						membersMutex.Lock()

						allMembers = append(allMembers, newMembers...)

						membersMutex.Unlock()
					}
				}(list)

				consecutiveNoProgress = 0
				lastMaxChunk = currentMaxChunk
			} else {
				consecutiveNoProgress++
			}

			membersMutex.Lock()

			currentMemberCount := len(allMembers)

			membersMutex.Unlock()

			w.logger.Debug("Member list status",
				zap.Int("max_chunk", currentMaxChunk),
				zap.Int("total_visible", list.TotalVisible()),
				zap.Int("processed_members", currentMemberCount))

			time.Sleep(50 * time.Millisecond)

			if consecutiveNoProgress > 0 {
				w.logger.Debug("No progress in current iteration",
					zap.Int("consecutive_no_progress", consecutiveNoProgress),
					zap.Int("max_consecutive_allowed", maxConsecutiveNoProgressIter))
			}

			if consecutiveNoProgress >= maxConsecutiveNoProgressIter {
				// Wait for all in-flight goroutines to complete before checking final count
				w.logger.Debug("No progress detected, waiting for in-flight goroutines to complete",
					zap.Int("consecutive_iterations", consecutiveNoProgress))
				wg.Wait()

				// Get the final member count after all goroutines have finished
				membersMutex.Lock()

				finalMemberCount := len(allMembers)

				membersMutex.Unlock()

				w.logger.Debug("All goroutines complete, considering sync complete",
					zap.Int("consecutive_iterations", consecutiveNoProgress),
					zap.Int("final_member_count", finalMemberCount))

				return allMembers, nil
			}

			if list.TotalVisible() > 0 && currentMemberCount >= list.TotalVisible() {
				// Wait for all in-flight goroutines to complete before returning
				wg.Wait()

				// Get the final member count after all goroutines have finished
				membersMutex.Lock()

				finalMemberCount := len(allMembers)

				membersMutex.Unlock()

				w.logger.Debug("Reached all visible members",
					zap.Int("total_visible", list.TotalVisible()),
					zap.Int("processed", finalMemberCount))

				return allMembers, nil
			}

			// Request the next chunk if we have data arriving and the list isn't empty
			if currentMaxChunk >= 0 && list.TotalVisible() > 100 && list.TotalVisible() > currentMemberCount {
				nextChunk := currentMaxChunk + 1

				// Wait for rate limit before making request
				if err := w.discordRateLimiter.waitForNextSlot(ctx); err != nil {
					w.logger.Debug("Context cancelled during rate limit wait",
						zap.String("guildID", guildID.String()))

					return allMembers, ctx.Err()
				}

				w.memberState.RequestMemberList(ctx, guildID, channelID, nextChunk)
			} else {
				// Wait before checking status again to prevent tight loop
				time.Sleep(100 * time.Millisecond)
			}
		}
	}
}

// handleMemberListNotFound handles the case when the member list is not found.
// It retries the request up to maxConsecutiveListNotFound times before returning an error.
// Returns true to continue the loop, or false with an error to exit.
func (w *Worker) handleMemberListNotFound(
	ctx context.Context,
	guildID discord.GuildID,
	channelID discord.ChannelID,
	consecutiveListNotFound *int,
	maxConsecutiveListNotFound int,
) (bool, error) {
	*consecutiveListNotFound++
	w.logger.Debug("Member list not found, will retry",
		zap.String("guildID", guildID.String()),
		zap.Int("attempt", *consecutiveListNotFound),
		zap.Int("max_attempts", maxConsecutiveListNotFound))

	// Break out after too many consecutive failures
	if *consecutiveListNotFound >= maxConsecutiveListNotFound {
		w.logger.Debug("Too many consecutive list not found errors, will try a different channel",
			zap.String("guildID", guildID.String()),
			zap.Int("max_attempts", maxConsecutiveListNotFound))

		return false, ErrListNotFoundRetry
	}

	// Wait for rate limit before making request
	if err := w.discordRateLimiter.waitForNextSlot(ctx); err != nil {
		w.logger.Debug("Context cancelled during rate limit wait",
			zap.String("guildID", guildID.String()))

		return false, ctx.Err()
	}

	// Try again with a new request
	w.memberState.RequestMemberList(ctx, guildID, channelID, 0)

	return true, nil
}

// collectPotentialMembers extracts members from the member list that haven't been processed yet.
// Returns a map of userID -> joinedAt time.
func (w *Worker) collectPotentialMembers(
	list *memberstate.List, processed map[uint64]struct{}, guildID discord.GuildID,
) map[uint64]time.Time {
	potentialMembers := make(map[uint64]time.Time)

	var totalItems, nilItems, botItems, alreadyProcessed int

	list.ViewItems(func(items []gateway.GuildMemberListOpItem) {
		totalItems = len(items)

		for _, item := range items {
			// Skip invalid items
			if item.Member == nil || item.Member.User.ID == 0 {
				nilItems++
				continue
			}

			userID := uint64(item.Member.User.ID)

			// Skip if already processed in this sync cycle
			if _, ok := processed[userID]; ok {
				alreadyProcessed++
				continue
			}

			// Skip if bot user
			if item.Member.User.Bot {
				botItems++
				continue
			}

			// Store the member for processing
			potentialMembers[userID] = item.Member.Joined.Time()
			processed[userID] = struct{}{}
		}
	})

	w.logger.Debug("Collected potential members",
		zap.String("guildID", guildID.String()),
		zap.Int("total_items", totalItems),
		zap.Int("nil_items", nilItems),
		zap.Int("bot_items", botItems),
		zap.Int("already_processed", alreadyProcessed),
		zap.Int("potential_members", len(potentialMembers)))

	return potentialMembers
}

// filterAndCheckMembers checks potential members against the database and applies grace period filtering.
// Returns a slice of new members to add.
func (w *Worker) filterAndCheckMembers(
	ctx context.Context, potentialMembers map[uint64]time.Time, guildID discord.GuildID, now time.Time,
) []*types.DiscordServerMember {
	if len(potentialMembers) == 0 {
		return nil
	}

	// Build list of user IDs to check
	userIDsToCheck := make([]uint64, 0, len(potentialMembers))
	for userID := range potentialMembers {
		userIDsToCheck = append(userIDsToCheck, userID)
	}

	// Check which users already exist in our database
	existingUsers := make(map[uint64]bool)

	if len(userIDsToCheck) > 0 {
		existingMembersMap, err := w.db.Model().Sync().GetFlaggedServerMembers(ctx, userIDsToCheck)
		if err != nil {
			w.logger.Error("Failed to check existing members",
				zap.Error(err),
				zap.Int("user_count", len(userIDsToCheck)))
		} else {
			for userID := range existingMembersMap {
				existingUsers[userID] = true
			}
		}
	}

	// Create the final list of new members based on grace period checks
	newMembers := make([]*types.DiscordServerMember, 0, len(potentialMembers))
	twelveHoursAgo := now.Add(-12 * time.Hour)

	for userID, joinedAt := range potentialMembers {
		// If user doesn't already exist in our database, apply grace period
		if !existingUsers[userID] && joinedAt.After(twelveHoursAgo) {
			w.logger.Debug("Skipping recently joined member (grace period)",
				zap.Uint64("serverID", uint64(guildID)),
				zap.Uint64("userID", userID),
				zap.Time("joined_at", joinedAt))

			continue
		}

		// Add to new members list
		newMembers = append(newMembers, &types.DiscordServerMember{
			ServerID:  uint64(guildID),
			UserID:    userID,
			JoinedAt:  joinedAt,
			UpdatedAt: now,
		})
	}

	return newMembers
}

// getRandomRetryCount returns a random retry count between 2 and 4.
func (w *Worker) getRandomRetryCount() int {
	return w.rng.Intn(3) + 2
}

// getRandomDelay returns a random delay between minDelay and maxDelay duration.
func (w *Worker) getRandomDelay(minDelay, maxDelay time.Duration) time.Duration {
	if maxDelay <= minDelay {
		return minDelay
	}

	delta := maxDelay - minDelay

	return minDelay + time.Duration(w.rng.Int63n(int64(delta)))
}
