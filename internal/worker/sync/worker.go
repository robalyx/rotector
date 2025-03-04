package sync

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/diamondburned/arikawa/v3/discord"
	"github.com/diamondburned/arikawa/v3/gateway"
	"github.com/diamondburned/arikawa/v3/state"
	"github.com/diamondburned/ningen/v3"
	"github.com/diamondburned/ningen/v3/states/member"
	"github.com/robalyx/rotector/internal/common/client/ai"
	"github.com/robalyx/rotector/internal/common/progress"
	"github.com/robalyx/rotector/internal/common/setup"
	"github.com/robalyx/rotector/internal/common/setup/config"
	"github.com/robalyx/rotector/internal/common/storage/database"
	"github.com/robalyx/rotector/internal/common/storage/database/types"
	"github.com/robalyx/rotector/internal/worker/core"
	"github.com/robalyx/rotector/internal/worker/sync/events"
	"go.uber.org/zap"
)

var (
	ErrTimeout              = errors.New("timed out waiting for member chunks")
	ErrNoTextChannel        = errors.New("no text channel found in guild")
	ErrAllChannelsAttempted = errors.New("all available channels have been attempted")
	ErrListNotFoundRetry    = errors.New("member list not found after multiple attempts")
)

// Worker handles syncing Discord server members.
type Worker struct {
	db              database.Client
	state           *ningen.State
	bar             *progress.Bar
	reporter        *core.StatusReporter
	logger          *zap.Logger
	config          *config.Config
	messageAnalyzer *ai.MessageAnalyzer
	eventHandler    *events.Handler
}

// New creates a new sync worker.
func New(app *setup.App, bar *progress.Bar, logger *zap.Logger) *Worker {
	// Create Discord state with sync token and required intents
	s := state.NewWithIntents(app.Config.Common.Discord.SyncToken,
		gateway.IntentGuilds|gateway.IntentGuildMembers|gateway.IntentGuildPresences|
			gateway.IntentGuildMessages|gateway.IntentMessageContent)

	// Create ningen state from discord state
	n := ningen.FromState(s)
	n.MemberState.OnError = func(err error) {
		logger.Warn("Member state error", zap.Error(err))
	}

	// Create status reporter
	reporter := core.NewStatusReporter(app.StatusClient, "sync", logger)

	// Create message analyzer
	messageAnalyzer := ai.NewMessageAnalyzer(app, logger)

	// Create event handler
	eventHandler := events.New(app, n, messageAnalyzer, logger)
	eventHandler.Setup()

	return &Worker{
		db:              app.DB,
		state:           n,
		bar:             bar,
		reporter:        reporter,
		logger:          logger,
		config:          app.Config,
		messageAnalyzer: messageAnalyzer,
		eventHandler:    eventHandler,
	}
}

// Start begins the sync worker's main loop.
func (w *Worker) Start() {
	w.logger.Info("Sync Worker started", zap.String("workerID", w.reporter.GetWorkerID()))
	w.reporter.Start()
	defer w.reporter.Stop()

	// Open Discord gateway connection
	if err := w.state.Open(context.Background()); err != nil {
		w.logger.Fatal("Failed to open gateway", zap.Error(err))
	}
	defer w.state.Close()

	// Set up event handlers for real-time member tracking
	w.eventHandler.Setup()

	for {
		w.bar.Reset()
		w.reporter.SetHealthy(true)

		// Run sync cycle
		if err := w.syncCycle(); err != nil {
			w.logger.Error("Failed to sync servers", zap.Error(err))
			w.reporter.SetHealthy(false)
			time.Sleep(1 * time.Minute)
			continue
		}

		// Short pause between cycles
		w.bar.SetStepMessage("Waiting for next cycle", 100)
		w.reporter.UpdateStatus("Waiting for next cycle", 100)
		time.Sleep(15 * time.Minute)
	}
}

// syncCycle attempts to sync all servers.
func (w *Worker) syncCycle() error {
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
	ctx := context.Background()

	for i, guild := range guilds {
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

		if err := w.db.Models().Sync().UpsertServerInfo(ctx, serverInfo); err != nil {
			w.logger.Error("Failed to update server info",
				zap.String("name", guild.Name),
				zap.Uint64("id", uint64(guild.ID)),
				zap.Error(err))
			// Continue to next guild even if server info update fails
			failedGuilds++
			continue
		}

		// Request all members for this guild
		members, err := w.syncServerMembers(guild.ID)
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
				zap.Uint64("guild_id", uint64(guild.ID)),
				zap.Int("partial_member_count", len(members)))
		}

		w.logger.Debug("Adding members to database",
			zap.String("guild_name", guild.Name),
			zap.Uint64("guild_id", uint64(guild.ID)),
			zap.Int("member_count", len(members)))

		// Batch update members for this guild
		guildID := uint64(guild.ID)
		if err := w.db.Models().Sync().BatchUpsertServerMembers(ctx, guildID, members); err != nil {
			w.logger.Error("Failed to batch update members",
				zap.String("guild_name", guild.Name),
				zap.Uint64("guild_id", guildID),
				zap.Int("member_count", len(members)),
				zap.Error(err))
			failedGuilds++
			continue
		}

		// Process bans for this guild's members
		userIDs := make([]uint64, 0, len(members))
		for _, member := range members {
			userIDs = append(userIDs, member.UserID)
		}
		w.eventHandler.CreateBansForUsers(ctx, userIDs)

		totalMembers += len(members)
		successfulGuilds++
	}

	// Get total unique members in database for reporting
	uniqueUserCount, err := w.db.Models().Sync().GetUniqueUserCount(ctx)
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
func (w *Worker) syncServerMembers(guildID discord.GuildID) ([]*types.DiscordServerMember, error) {
	now := time.Now()

	// Keep track of attempted channels to avoid repeating
	attemptedChannels := make(map[discord.ChannelID]struct{})
	maxRetries := 8

	var lastError error
	var allMembers []*types.DiscordServerMember

	// Try with different channels up to maxRetries times
	for attempt := range maxRetries {
		// Find a suitable text channel for member list requests, excluding already attempted ones
		targetChannel, err := w.findTextChannel(guildID, attemptedChannels)
		if err != nil {
			lastError = err
			break // No more channels to try
		}

		// Mark this channel as attempted
		attemptedChannels[targetChannel] = struct{}{}

		w.logger.Debug("Trying member sync with channel",
			zap.String("guild_id", guildID.String()),
			zap.String("channel_id", targetChannel.String()),
			zap.Int("attempt", attempt+1),
			zap.Int("max_attempts", maxRetries))

		// Initialize the sync by requesting the first chunk
		w.state.MemberState.RequestMemberList(guildID, targetChannel, 0)

		// Wait for initial data to arrive
		time.Sleep(1 * time.Second)

		// Main sync loop - continue until we've synced all members or hit timeout
		members, err := w.syncMemberChunks(guildID, targetChannel, now)

		// If successful or not a retry error, return immediately
		if err == nil || !errors.Is(err, ErrListNotFoundRetry) {
			return members, err
		}

		// For retry error, save any members we got and try with another channel
		lastError = err
		allMembers = append(allMembers, members...)

		w.logger.Info("Retrying member sync with different channel",
			zap.String("guild_id", guildID.String()),
			zap.String("previous_channel", targetChannel.String()),
			zap.Int("members_so_far", len(allMembers)),
			zap.Int("attempt", attempt+1))
	}

	// If we got some members despite errors, return them
	if len(allMembers) > 0 {
		return allMembers, nil
	}

	return nil, fmt.Errorf("failed to sync guild members after %d channel attempts: %w", maxRetries, lastError)
}

// findTextChannel locates a suitable text channel in the guild for member list requests.
// The attemptedChannels map tracks channels that have already been tried to avoid repetition.
func (w *Worker) findTextChannel(
	guildID discord.GuildID, attemptedChannels map[discord.ChannelID]struct{},
) (discord.ChannelID, error) {
	channels, err := w.state.Channels(guildID, []discord.ChannelType{discord.GuildText})
	if err != nil {
		return 0, fmt.Errorf("failed to get guild channels: %w", err)
	}

	if len(channels) == 0 {
		return 0, ErrNoTextChannel
	}

	// Priority channel names that typically contain full member lists
	priorityNames := map[string]struct{}{
		"general":       {},
		"main":          {},
		"announcements": {},
		"welcome":       {},
		"lobby":         {},
		"chat":          {},
		"lounge":        {},
		"hangout":       {},
		"discussion":    {},
		"community":     {},
	}

	// First pass: check for priority channels by name
	for _, channel := range channels {
		// Skip channels that are already attempted
		if _, attempted := attemptedChannels[channel.ID]; attempted {
			continue
		}

		// Check if the channel has a priority name
		channelName := strings.ToLower(channel.Name)
		if _, ok := priorityNames[channelName]; ok {
			w.logger.Debug("Selected priority channel for member list",
				zap.String("guild_id", guildID.String()),
				zap.String("channel_id", channel.ID.String()),
				zap.String("channel_name", channel.Name))
			return channel.ID, nil
		}
	}

	// Second pass: find the most suitable channel based on recent activity
	var mostSuitableChannel discord.ChannelID
	var highestMessageID discord.MessageID

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
			zap.String("guild_id", guildID.String()),
			zap.String("channel_id", mostSuitableChannel.String()),
			zap.String("message_id", highestMessageID.String()))
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
				zap.String("guild_id", guildID.String()),
				zap.String("channel_id", channel.ID.String()))
			return channel.ID, nil
		}
	}

	return 0, ErrNoTextChannel
}

// syncMemberChunks handles the main sync loop, retrieving member chunks and processing them.
func (w *Worker) syncMemberChunks(
	guildID discord.GuildID,
	channelID discord.ChannelID,
	now time.Time,
) ([]*types.DiscordServerMember, error) {
	var allMembers []*types.DiscordServerMember
	processed := make(map[uint64]struct{}) // Track which members we've processed

	timeout := time.After(5 * time.Minute)
	lastMaxChunk := -1
	consecutiveNoProgress := 0
	maxConsecutiveNoProgress := 3

	// Track list not found errors
	consecutiveListNotFound := 0
	maxConsecutiveListNotFound := 5 // Max retries before giving up

	w.logger.Debug("Starting member chunk sync",
		zap.String("guild_id", guildID.String()),
		zap.String("channel_id", channelID.String()))

	for {
		select {
		case <-timeout:
			return allMembers, fmt.Errorf("timeout while syncing member list: %w", ErrTimeout)
		default:
			// Get current member list state
			list, err := w.state.MemberState.GetMemberList(guildID, channelID)
			if err != nil {
				if errors.Is(err, member.ErrListNotFound) {
					consecutiveListNotFound++
					w.logger.Debug("Member list not found, will retry",
						zap.String("guild_id", guildID.String()),
						zap.Int("attempt", consecutiveListNotFound),
						zap.Int("max_attempts", maxConsecutiveListNotFound))

					// Break out after too many consecutive failures
					if consecutiveListNotFound >= maxConsecutiveListNotFound {
						w.logger.Debug("Too many consecutive list not found errors, will try a different channel",
							zap.String("guild_id", guildID.String()),
							zap.Int("max_attempts", maxConsecutiveListNotFound))
						return allMembers, ErrListNotFoundRetry
					}

					// Try again with a new request
					w.state.MemberState.RequestMemberList(guildID, channelID, 0)
					time.Sleep(1 * time.Second)
					continue
				}

				w.logger.Error("Failed to get member list",
					zap.String("guild_id", guildID.String()),
					zap.Error(err))
				return allMembers, fmt.Errorf("failed to get member list: %w", err)
			}

			// Reset the consecutive failure counter since we got a successful response
			consecutiveListNotFound = 0

			// Process any new members
			newMembers := w.processMemberList(list, processed, guildID, now)
			allMembers = append(allMembers, newMembers...)
			currentMaxChunk := list.MaxChunk()

			w.logger.Debug("Member list status",
				zap.Int("max_chunk", currentMaxChunk),
				zap.Int("total_visible", list.TotalVisible()),
				zap.Int("processed_members", len(allMembers)),
				zap.Int("new_members", len(newMembers)))

			// Check if we're making progress
			if len(newMembers) == 0 && currentMaxChunk == lastMaxChunk {
				consecutiveNoProgress++
				w.logger.Debug("No progress in current iteration",
					zap.Int("consecutive_no_progress", consecutiveNoProgress),
					zap.Int("max_consecutive_allowed", maxConsecutiveNoProgress))
			} else {
				consecutiveNoProgress = 0
				lastMaxChunk = currentMaxChunk
			}

			// Request the next chunk if we have data arriving and the list isn't empty
			if currentMaxChunk >= 0 && list.TotalVisible() > 100 && list.TotalVisible() > len(allMembers) {
				nextChunk := currentMaxChunk + 1
				w.state.MemberState.RequestMemberList(guildID, channelID, nextChunk)
			}

			// Termination conditions
			if consecutiveNoProgress >= maxConsecutiveNoProgress {
				w.logger.Debug("No new members for several iterations, considering sync complete",
					zap.Int("consecutive_iterations", consecutiveNoProgress))
				return allMembers, nil
			}

			if list.TotalVisible() > 0 && len(allMembers) >= list.TotalVisible() {
				w.logger.Debug("Reached all visible members",
					zap.Int("total_visible", list.TotalVisible()),
					zap.Int("processed", len(allMembers)))
				return allMembers, nil
			}

			// Wait longer between checks to avoid excessive polling
			time.Sleep(1 * time.Second)
		}
	}
}

// processMemberList extracts members from the member list that haven't been processed yet.
// Returns a slice of new members found.
func (w *Worker) processMemberList(
	list *member.List,
	processed map[uint64]struct{},
	guildID discord.GuildID,
	now time.Time,
) []*types.DiscordServerMember {
	// Collect users to check which ones are already in our database
	potentialMembers := make(map[uint64]time.Time)
	userIDsToCheck := make([]uint64, 0, len(potentialMembers))
	list.ViewItems(func(items []gateway.GuildMemberListOpItem) {
		for i := range items {
			// Use array indexing instead of iteration to avoid potential copy issues
			item := items[i]

			// Skip invalid items
			if item.Member == nil || item.Member.User.ID == 0 {
				continue
			}

			userID := uint64(item.Member.User.ID)

			// Skip if already processed in this sync cycle
			if _, ok := processed[userID]; ok {
				continue
			}

			// Skip if bot user
			if item.Member.User.Bot {
				continue
			}

			// Store the member for processing
			potentialMembers[userID] = item.Member.Joined.Time()
			userIDsToCheck = append(userIDsToCheck, userID)
			processed[userID] = struct{}{}
		}
	})

	// If no members, return empty slice
	if len(potentialMembers) == 0 || len(userIDsToCheck) == 0 {
		return nil
	}

	// Check which users already exist in our database
	existingUsers := make(map[uint64]bool)
	if len(userIDsToCheck) > 0 {
		existingMembersMap, err := w.db.Models().Sync().GetFlaggedServerMembers(context.Background(), userIDsToCheck)
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
	oneHourAgo := now.Add(-1 * time.Hour)

	for userID, joinedAt := range potentialMembers {
		// If user doesn't already exist in our database, apply grace period
		if !existingUsers[userID] && joinedAt.After(oneHourAgo) {
			w.logger.Debug("Skipping recently joined member (grace period)",
				zap.Uint64("server_id", uint64(guildID)),
				zap.Uint64("user_id", userID),
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
