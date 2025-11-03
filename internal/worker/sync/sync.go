package sync

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/diamondburned/arikawa/v3/discord"
	"github.com/diamondburned/arikawa/v3/gateway"
	"github.com/diamondburned/arikawa/v3/state"
	"github.com/robalyx/rotector/internal/database/types"
	"github.com/robalyx/rotector/internal/discord/memberstate"
	"github.com/robalyx/rotector/pkg/utils"
	"go.uber.org/zap"
)

const (
	maxChannelAttempts           = 5
	maxConsecutiveNoProgressIter = 3
	maxConsecutiveListNotFound   = 3
	syncTimeout                  = 5 * time.Minute
)

// accountSyncResult holds the results from syncing a single account.
type accountSyncResult struct {
	accountIndex     int
	totalMembers     int
	successfulGuilds int
	failedGuilds     int
	totalGuilds      int
	err              error
}

// syncProgress tracks real-time progress across all parallel account syncs.
type syncProgress struct {
	totalGuilds      atomic.Int64
	processedGuilds  atomic.Int64
	successfulGuilds atomic.Int64
	failedGuilds     atomic.Int64
	skippedGuilds    atomic.Int64
}

// syncCycle attempts to sync all servers across all accounts in parallel.
func (w *Worker) syncCycle(ctx context.Context) {
	// Clear seen servers map at the start of each cycle
	w.seenServersMutex.Lock()
	w.seenServers = make(map[uint64]int)
	w.seenServersMutex.Unlock()

	// Create shared progress tracker for all accounts
	progress := &syncProgress{}

	// Create channel to receive results from each account
	resultsChan := make(chan accountSyncResult, len(w.states))

	var wg sync.WaitGroup

	// Launch a goroutine for each account
	for i := range w.states {
		wg.Add(1)

		go func(accountIndex int) {
			defer wg.Done()

			result := accountSyncResult{
				accountIndex: accountIndex,
			}

			// Sync this account's servers
			totalMembers, successfulGuilds, failedGuilds, totalGuilds, err := w.syncAccountServers(
				ctx, accountIndex, w.states[accountIndex], w.memberStates[accountIndex], progress)

			result.totalMembers = totalMembers
			result.successfulGuilds = successfulGuilds
			result.failedGuilds = failedGuilds
			result.totalGuilds = totalGuilds
			result.err = err

			resultsChan <- result
		}(i)
	}

	// Wait for all accounts to finish
	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	// Monitor and display aggregate progress across all accounts
	progressCtx, progressCancel := context.WithCancel(ctx)
	defer progressCancel()

	go func() {
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-progressCtx.Done():
				return
			case <-ticker.C:
				total := progress.totalGuilds.Load()
				processed := progress.processedGuilds.Load()
				successful := progress.successfulGuilds.Load()
				failed := progress.failedGuilds.Load()
				skipped := progress.skippedGuilds.Load()

				if total > 0 {
					progressPercent := (processed * 100) / total
					w.bar.SetStepMessage(fmt.Sprintf("Processing: %d/%d guilds across %d accounts [%d OK, %d fail, %d skip]",
						processed, total, len(w.states), successful, failed, skipped), progressPercent)
				}
			}
		}
	}()

	// Aggregate results
	totalMembers := 0
	totalSuccessfulGuilds := 0
	totalFailedGuilds := 0

	for result := range resultsChan {
		totalMembers += result.totalMembers
		totalSuccessfulGuilds += result.successfulGuilds
		totalFailedGuilds += result.failedGuilds

		if result.err != nil {
			w.logger.Error("Account sync failed",
				zap.Int("account_index", result.accountIndex),
				zap.Error(result.err))
		} else {
			w.logger.Info("Account sync completed",
				zap.Int("account_index", result.accountIndex),
				zap.Int("successful_guilds", result.successfulGuilds),
				zap.Int("failed_guilds", result.failedGuilds),
				zap.Int("total_members", result.totalMembers))
		}
	}

	w.logger.Info("Member sync statistics",
		zap.Int("members_seen_this_cycle", totalMembers),
		zap.Int("accounts_synced", len(w.states)),
		zap.Int("guilds_successful", totalSuccessfulGuilds),
		zap.Int("guilds_failed", totalFailedGuilds))

	w.bar.SetStepMessage(fmt.Sprintf("Synced %d servers (%d failed) across %d accounts",
		totalSuccessfulGuilds, totalFailedGuilds, len(w.states)), 100)
	w.reporter.UpdateStatus(fmt.Sprintf("Sync complete: %d OK, %d failed", totalSuccessfulGuilds, totalFailedGuilds), 100)
}

// syncAccountServers syncs all servers for a specific account.
func (w *Worker) syncAccountServers(
	ctx context.Context, accountIndex int, s *state.State, ms *memberstate.State, progress *syncProgress,
) (int, int, int, int, error) {
	// Get all guilds for this account
	guilds, err := s.Guilds()
	if err != nil {
		return 0, 0, 0, 0, fmt.Errorf("failed to get guilds: %w", err)
	}

	// Add this account's guild count to the total
	totalGuilds := len(guilds)
	progress.totalGuilds.Add(int64(totalGuilds))

	// Track counts for this account
	totalMembers := 0
	successfulGuilds := 0
	failedGuilds := 0
	now := time.Now()

	for _, guild := range guilds {
		// Check if context was cancelled
		select {
		case <-ctx.Done():
			w.logger.Info("Context cancelled during guild sync",
				zap.Int("account_index", accountIndex))

			return totalMembers, successfulGuilds, failedGuilds, totalGuilds, ctx.Err()
		default:
		}

		// Check for duplicate server across accounts
		serverID := uint64(guild.ID)

		w.seenServersMutex.Lock()

		if existingAccountIndex, exists := w.seenServers[serverID]; exists {
			w.logger.Info("Duplicate server detected across accounts, skipping",
				zap.Uint64("serverID", serverID),
				zap.String("server_name", guild.Name),
				zap.Int("account_index", accountIndex),
				zap.Int("existing_account_index", existingAccountIndex))

			w.seenServersMutex.Unlock()

			progress.processedGuilds.Add(1)
			progress.skippedGuilds.Add(1)

			continue
		}

		w.seenServers[serverID] = accountIndex
		w.seenServersMutex.Unlock()

		w.logger.Debug("Syncing guild",
			zap.Int("account_index", accountIndex),
			zap.String("name", guild.Name),
			zap.Uint64("id", serverID))

		// Store server info for this guild
		serverInfo := &types.DiscordServerInfo{
			ServerID:  serverID,
			Name:      guild.Name,
			UpdatedAt: now,
		}

		if err := w.db.Model().Sync().UpsertServerInfo(ctx, serverInfo); err != nil {
			return totalMembers, successfulGuilds, failedGuilds, totalGuilds, fmt.Errorf("failed to update server info for guild %d: %w", serverID, err)
		}

		// Request all members for this guild
		members, err := w.syncServerMembersForAccount(ctx, accountIndex, guild.ID, s, ms)
		if err != nil {
			w.logger.Error("Failed to sync guild members",
				zap.Int("account_index", accountIndex),
				zap.String("name", guild.Name),
				zap.Uint64("id", serverID),
				zap.Error(err))

			failedGuilds++

			// If we got partial results, we'll still try to save them
			if len(members) == 0 {
				progress.processedGuilds.Add(1)
				progress.failedGuilds.Add(1)

				continue
			}

			w.logger.Info("Adding partial member results despite sync error",
				zap.Int("account_index", accountIndex),
				zap.String("guild_name", guild.Name),
				zap.Uint64("guildID", serverID),
				zap.Int("partial_member_count", len(members)))
		}

		w.logger.Debug("Adding members to database",
			zap.Int("account_index", accountIndex),
			zap.String("guild_name", guild.Name),
			zap.Uint64("guildID", serverID),
			zap.Int("member_count", len(members)))

		// Batch update members for this guild
		if err := w.db.Model().Sync().UpsertServerMembers(ctx, members, false); err != nil {
			w.logger.Error("Failed to batch update members",
				zap.Int("account_index", accountIndex),
				zap.String("guild_name", guild.Name),
				zap.Uint64("guildID", serverID),
				zap.Int("member_count", len(members)),
				zap.Error(err))

			failedGuilds++

			progress.processedGuilds.Add(1)
			progress.failedGuilds.Add(1)

			continue
		}

		totalMembers += len(members)
		successfulGuilds++

		progress.processedGuilds.Add(1)
		progress.successfulGuilds.Add(1)

		// Add delay between guilds
		if utils.ContextSleep(ctx, 1500*time.Millisecond) == utils.SleepCancelled {
			return totalMembers, successfulGuilds, failedGuilds, totalGuilds, ctx.Err()
		}
	}

	return totalMembers, successfulGuilds, failedGuilds, totalGuilds, nil
}

// syncServerMembersForAccount gets all members for a guild using the lazy member list approach.
func (w *Worker) syncServerMembersForAccount(
	ctx context.Context, accountIndex int, guildID discord.GuildID, s *state.State, ms *memberstate.State,
) ([]*types.DiscordServerMember, error) {
	now := time.Now()

	attemptedChannels := make(map[discord.ChannelID]struct{})

	var (
		lastError  error
		allMembers []*types.DiscordServerMember
	)

	for attempt := range maxChannelAttempts {
		targetChannel, err := w.findTextChannelForAccount(accountIndex, guildID, attemptedChannels, s)
		if err != nil {
			lastError = err
			break
		}

		attemptedChannels[targetChannel] = struct{}{}

		w.logger.Debug("Trying member sync with channel",
			zap.Int("account_index", accountIndex),
			zap.String("guildID", guildID.String()),
			zap.String("channelID", targetChannel.String()),
			zap.Int("attempt", attempt+1),
			zap.Int("max_attempts", maxChannelAttempts))

		if attempt > 0 {
			thinkingDelay := 3 * time.Second
			w.logger.Debug("Waiting before trying new channel",
				zap.Int("account_index", accountIndex),
				zap.Duration("delay", thinkingDelay))
			time.Sleep(thinkingDelay)
		}

		// Wait for rate limit slot
		if err := w.discordRateLimiter.WaitForNextSlot(ctx); err != nil {
			return nil, err
		}

		ms.RequestMemberList(ctx, guildID, targetChannel, 0)

		// Wait for member list response
		time.Sleep(1 * time.Second)

		members, err := w.syncMemberChunksForAccount(ctx, accountIndex, guildID, targetChannel, ms, now)

		if err == nil || !errors.Is(err, ErrListNotFoundRetry) {
			return members, err
		}

		lastError = err

		allMembers = append(allMembers, members...)

		w.logger.Info("Retrying member sync with different channel",
			zap.Int("account_index", accountIndex),
			zap.String("guildID", guildID.String()),
			zap.String("previous_channel", targetChannel.String()),
			zap.Int("members_so_far", len(allMembers)),
			zap.Int("attempt", attempt+1))

		if attempt < maxChannelAttempts-1 {
			channelSwitchDelay := 7 * time.Second
			w.logger.Debug("Waiting before trying next channel",
				zap.Int("account_index", accountIndex),
				zap.Duration("delay", channelSwitchDelay))
			time.Sleep(channelSwitchDelay)
		}
	}

	if len(allMembers) > 0 {
		return allMembers, nil
	}

	return nil, fmt.Errorf("failed to sync guild members after %d channel attempts: %w", maxChannelAttempts, lastError)
}

// findTextChannelForAccount locates a suitable text channel in the guild for member list requests.
// The attemptedChannels map tracks channels that have already been tried to avoid repetition.
func (w *Worker) findTextChannelForAccount(
	accountIndex int, guildID discord.GuildID, attemptedChannels map[discord.ChannelID]struct{}, s *state.State,
) (discord.ChannelID, error) {
	channels, err := s.Channels(guildID)
	if err != nil {
		return 0, fmt.Errorf("failed to get guild channels: %w", err)
	}

	// Get bot's user ID for permission checks
	botUserID := s.Ready().User.ID

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
		perms, err := s.Permissions(channel.ID, botUserID)
		if err != nil {
			w.logger.Debug("Failed to check permissions for channel",
				zap.Int("account_index", accountIndex),
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

// syncMemberChunksForAccount handles the main sync loop, retrieving member chunks and processing them.
func (w *Worker) syncMemberChunksForAccount(
	ctx context.Context, accountIndex int, guildID discord.GuildID, channelID discord.ChannelID,
	ms *memberstate.State, now time.Time,
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

	w.logger.Debug("Starting member chunk sync",
		zap.Int("account_index", accountIndex),
		zap.String("guildID", guildID.String()),
		zap.String("channelID", channelID.String()))

	defer wg.Wait()

	for {
		select {
		case <-timeout:
			return allMembers, fmt.Errorf("timeout while syncing member list: %w", ErrTimeout)
		case <-ctx.Done():
			w.logger.Info("Context cancelled during member sync",
				zap.Int("account_index", accountIndex))

			return allMembers, ctx.Err()
		default:
			// Get current member list state
			list, err := ms.GetMemberList(guildID, channelID)
			if err != nil {
				if errors.Is(err, memberstate.ErrListNotFound) {
					shouldContinue, handleErr := w.handleMemberListNotFoundForAccount(
						ctx, accountIndex, guildID, channelID, ms, &consecutiveListNotFound)
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

			// Add delay between chunk requests
			if utils.ContextSleep(ctx, 300*time.Millisecond) == utils.SleepCancelled {
				return allMembers, ctx.Err()
			}

			if consecutiveNoProgress >= maxConsecutiveNoProgressIter {
				wg.Wait()

				// Get the final member count after all goroutines have finished
				membersMutex.Lock()

				finalMemberCount := len(allMembers)

				membersMutex.Unlock()

				w.logger.Debug("All goroutines complete, considering sync complete",
					zap.Int("account_index", accountIndex),
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
					zap.Int("account_index", accountIndex),
					zap.Int("total_visible", list.TotalVisible()),
					zap.Int("processed", finalMemberCount))

				return allMembers, nil
			}

			// Request the next chunk if we have data arriving and the list isn't empty
			if currentMaxChunk >= 0 && list.TotalVisible() > 100 && list.TotalVisible() > currentMemberCount {
				nextChunk := currentMaxChunk + 1

				// Wait for rate limit slot
				if err := w.discordRateLimiter.WaitForNextSlot(ctx); err != nil {
					return allMembers, err
				}

				ms.RequestMemberList(ctx, guildID, channelID, nextChunk)
			}
		}
	}
}

// handleMemberListNotFoundForAccount handles the case when the member list is not found.
// It retries the request up to maxConsecutiveListNotFound times before returning an error.
// Returns true to continue the loop, or false with an error to exit.
func (w *Worker) handleMemberListNotFoundForAccount(
	ctx context.Context, accountIndex int, guildID discord.GuildID, channelID discord.ChannelID,
	ms *memberstate.State, consecutiveListNotFound *int,
) (bool, error) {
	*consecutiveListNotFound++
	w.logger.Debug("Member list not found, will retry",
		zap.Int("account_index", accountIndex),
		zap.String("guildID", guildID.String()),
		zap.Int("attempt", *consecutiveListNotFound),
		zap.Int("max_attempts", maxConsecutiveListNotFound))

	// Break out after too many consecutive failures
	if *consecutiveListNotFound >= maxConsecutiveListNotFound {
		w.logger.Debug("Too many consecutive list not found errors, will try a different channel",
			zap.Int("account_index", accountIndex),
			zap.String("guildID", guildID.String()),
			zap.Int("max_attempts", maxConsecutiveListNotFound))

		return false, ErrListNotFoundRetry
	}

	// Apply exponential backoff delay
	backoffSeconds := min(1<<(*consecutiveListNotFound-1), 30)
	backoffDuration := time.Duration(backoffSeconds) * time.Second

	select {
	case <-time.After(backoffDuration):
		// Continue with retry
	case <-ctx.Done():
		return false, ctx.Err()
	}

	// Wait for rate limit slot
	if err := w.discordRateLimiter.WaitForNextSlot(ctx); err != nil {
		return false, err
	}

	// Try again with a new request
	ms.RequestMemberList(ctx, guildID, channelID, 0)

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
	twentyFourHoursAgo := now.Add(-24 * time.Hour)

	for userID, joinedAt := range potentialMembers {
		// If user doesn't already exist in our database, apply grace period
		if !existingUsers[userID] && joinedAt.After(twentyFourHoursAgo) {
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
