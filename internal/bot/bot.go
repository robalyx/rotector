package bot

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/disgoorg/disgo"
	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/disgo/gateway"
	"github.com/disgoorg/disgo/sharding"
	"github.com/disgoorg/snowflake/v2"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/pagination"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/bot/interfaces"
	"github.com/robalyx/rotector/internal/bot/menu/admin"
	"github.com/robalyx/rotector/internal/bot/menu/appeal"
	"github.com/robalyx/rotector/internal/bot/menu/ban"
	"github.com/robalyx/rotector/internal/bot/menu/captcha"
	"github.com/robalyx/rotector/internal/bot/menu/chat"
	"github.com/robalyx/rotector/internal/bot/menu/consent"
	"github.com/robalyx/rotector/internal/bot/menu/dashboard"
	"github.com/robalyx/rotector/internal/bot/menu/guild"
	"github.com/robalyx/rotector/internal/bot/menu/leaderboard"
	"github.com/robalyx/rotector/internal/bot/menu/log"
	"github.com/robalyx/rotector/internal/bot/menu/queue"
	groupReview "github.com/robalyx/rotector/internal/bot/menu/review/group"
	userReview "github.com/robalyx/rotector/internal/bot/menu/review/user"
	"github.com/robalyx/rotector/internal/bot/menu/reviewer"
	"github.com/robalyx/rotector/internal/bot/menu/setting"
	"github.com/robalyx/rotector/internal/bot/menu/status"
	"github.com/robalyx/rotector/internal/bot/menu/timeout"
	"github.com/robalyx/rotector/internal/bot/utils"
	"github.com/robalyx/rotector/internal/common/setup"
	"github.com/robalyx/rotector/internal/common/storage/database"
	"github.com/robalyx/rotector/internal/common/storage/database/types/enum"
	"go.uber.org/zap"
)

// Bot handles all the layouts and managers needed for Discord interaction.
type Bot struct {
	db                database.Client
	client            bot.Client
	logger            *zap.Logger
	sessionManager    *session.Manager
	paginationManager *pagination.Manager
}

// New initializes a Bot instance by creating all required managers and layouts.
func New(app *setup.App) (*Bot, error) {
	logger := app.Logger.Named("bot")

	// Initialize session manager for persistent storage
	sessionManager, err := session.NewManager(app.DB, app.RedisManager, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create session manager: %w", err)
	}
	paginationManager := pagination.NewManager(sessionManager, logger)

	// Create bot instance
	b := &Bot{
		db:                app.DB,
		logger:            logger,
		sessionManager:    sessionManager,
		paginationManager: paginationManager,
	}

	// Create Discord client
	client, err := disgo.New(app.Config.Bot.Discord.Token,
		bot.WithShardManagerConfigOpts(
			sharding.WithShardCount(app.Config.Bot.Discord.Sharding.Count),
			sharding.WithAutoScaling(app.Config.Bot.Discord.Sharding.AutoScale),
			sharding.WithShardSplitCount(app.Config.Bot.Discord.Sharding.SplitCount),
			func(config *sharding.Config) {
				// Parse shard IDs if specified
				if app.Config.Bot.Discord.Sharding.ShardIDs != "" {
					shardIDStrs := strings.SplitSeq(app.Config.Bot.Discord.Sharding.ShardIDs, ",")
					for idStr := range shardIDStrs {
						if id, err := strconv.Atoi(strings.TrimSpace(idStr)); err == nil {
							sharding.WithShardIDs(id)(config)
						}
					}
				}
			},
		),
		bot.WithGatewayConfigOpts(
			gateway.WithIntents(
				gateway.IntentGuilds,
				gateway.IntentGuildMessages,
				gateway.IntentDirectMessages,
				gateway.IntentGuildMembers,
			),
		),
		bot.WithEventListeners(&events.ListenerAdapter{
			OnApplicationCommandInteraction: b.handleApplicationCommandInteraction,
			OnComponentInteraction:          b.handleComponentInteraction,
			OnModalSubmit:                   b.handleModalSubmit,
		}),
	)
	if err != nil {
		return nil, err
	}
	b.client = client

	// Initialize layouts
	settingLayout := setting.New(app)
	logLayout := log.New(app)
	chatLayout := chat.New(app)
	captchaLayout := captcha.New(app)
	timeoutLayout := timeout.New(app)
	userReviewLayout := userReview.New(app, paginationManager)
	groupReviewLayout := groupReview.New(app, paginationManager)
	queueLayout := queue.New(app)
	appealLayout := appeal.New(app)
	adminLayout := admin.New(app)
	leaderboardLayout := leaderboard.New(app, client)
	statusLayout := status.New(app)
	dashboardLayout := dashboard.New(app, sessionManager)
	consentLayout := consent.New(app)
	banLayout := ban.New(app, sessionManager)
	reviewerLayout := reviewer.New(app, client)
	guildLayout := guild.New(app)

	paginationManager.AddPages(settingLayout.Pages())
	paginationManager.AddPages(logLayout.Pages())
	paginationManager.AddPages(chatLayout.Pages())
	paginationManager.AddPages(captchaLayout.Pages())
	paginationManager.AddPages(timeoutLayout.Pages())
	paginationManager.AddPages(userReviewLayout.Pages())
	paginationManager.AddPages(groupReviewLayout.Pages())
	paginationManager.AddPages(queueLayout.Pages())
	paginationManager.AddPages(appealLayout.Pages())
	paginationManager.AddPages(adminLayout.Pages())
	paginationManager.AddPages(leaderboardLayout.Pages())
	paginationManager.AddPages(statusLayout.Pages())
	paginationManager.AddPages(dashboardLayout.Pages())
	paginationManager.AddPages(consentLayout.Pages())
	paginationManager.AddPages(banLayout.Pages())
	paginationManager.AddPages(reviewerLayout.Pages())
	paginationManager.AddPages(guildLayout.Pages())
	return b, nil
}

// Start registers global commands with Discord and opens the gateway connection.
// It first ensures the dashboard command is registered globally before starting
// the bot's gateway connection to receive events.
func (b *Bot) Start() error {
	b.logger.Info("Registering commands")

	// Register the dashboard command globally for all guilds
	_, err := b.client.Rest().SetGlobalCommands(b.client.ApplicationID(), []discord.ApplicationCommandCreate{
		discord.SlashCommandCreate{
			Name:        constants.RotectorCommandName,
			Description: "Open the moderation interface",
		},
	})
	if err != nil {
		return fmt.Errorf("failed to register commands: %w", err)
	}

	// Open gateway connection
	err = b.client.OpenGateway(context.Background())
	if err != nil {
		return fmt.Errorf("failed to open gateway: %w", err)
	}

	b.logger.Info("Started bot")
	return nil
}

// Close gracefully shuts down the Discord gateway connection.
// This ensures all pending events are processed before shutdown.
func (b *Bot) Close() {
	b.logger.Info("Closing bot")
	b.client.Close(context.Background())
}

// handleApplicationCommandInteraction processes slash commands by first deferring the response,
// then validating guild settings and user permissions before handling the command in a goroutine.
func (b *Bot) handleApplicationCommandInteraction(event *events.ApplicationCommandInteractionCreate) {
	go func() {
		// Defer response to prevent Discord timeout while processing
		if err := event.DeferCreateMessage(true); err != nil {
			b.logger.Error("Failed to defer create message", zap.Error(err))
			return
		}

		// Only handle dashboard command - respond with error for unknown commands
		if event.SlashCommandInteractionData().CommandName() != constants.RotectorCommandName {
			b.paginationManager.RespondWithError(event, "This command is not available.")
			return
		}

		start := time.Now()
		defer func() {
			if r := recover(); r != nil {
				b.logger.Error("Application command interaction failed",
					zap.String("command", event.SlashCommandInteractionData().CommandName()),
					zap.String("user_id", event.User().ID.String()),
					zap.Any("panic", r),
				)
				b.paginationManager.RespondWithError(event, "Internal error. Please report this to an administrator.")
			}
			duration := time.Since(start)
			b.logger.Debug("Application command interaction handled",
				zap.String("command", event.SlashCommandInteractionData().CommandName()),
				zap.Duration("duration", duration))
		}()

		// Validate session but return early if session creation failed or session expired
		s, isNewSession, ok := b.validateAndGetSession(event, event.User().ID)
		if !ok {
			return
		}

		// Run common validation checks
		if !b.validateInteraction(event, s, isNewSession, true, "") {
			return
		}

		// Navigate to stored page
		pageName := session.CurrentPage.Get(s)
		b.paginationManager.Show(event, s, pageName, "")
		s.Touch(context.Background())
	}()
}

// handleComponentInteraction processes button clicks and select menu choices.
// It first updates the message to show "Processing..." and removes interactive components
// to prevent double-clicks, then processes the interaction in a goroutine.
func (b *Bot) handleComponentInteraction(event *events.ComponentInteractionCreate) {
	go func() {
		start := time.Now()
		defer func() {
			if r := recover(); r != nil {
				b.logger.Error("Component interaction failed",
					zap.String("component_id", event.Data.CustomID()),
					zap.String("component_type", fmt.Sprintf("%T", event.Data)),
					zap.String("user_id", event.User().ID.String()),
					zap.Any("panic", r),
				)
				b.paginationManager.RespondWithError(event, "Internal error. Please report this to an administrator.")
			}
			duration := time.Since(start)
			b.logger.Debug("Component interaction handled",
				zap.String("custom_id", event.Data.CustomID()),
				zap.Duration("duration", duration))
		}()

		// Validate session but return early if session creation failed or session expired
		s, isNewSession, ok := b.validateAndGetSession(event, event.User().ID)
		if !ok {
			return
		}

		// Get current page
		page := b.paginationManager.GetPage(session.CurrentPage.Get(s))

		// WORKAROUND:
		// Special handling for modal interactions to prevent response conflicts.
		// If we are opening a modal and we try to defer, there will be an error
		// that the interaction is already responded to.
		isModal := false

		// Check conditions for modal interaction
		stringSelectData, ok := event.Data.(discord.StringSelectMenuInteractionData)
		if ok && strings.HasSuffix(stringSelectData.Values[0], constants.ModalOpenSuffix) {
			isModal = true
		}

		if strings.HasSuffix(event.Data.CustomID(), constants.ModalOpenSuffix) {
			isModal = true
		}

		// Update message to prevent double-clicks (skip for modals)
		if !isModal || page == nil {
			updateBuilder := discord.NewMessageUpdateBuilder().
				SetContent(utils.GetTimestampedSubtext("Processing...")).
				ClearContainerComponents()

			if err := event.UpdateMessage(updateBuilder.Build()); err != nil {
				b.logger.Error("Failed to update message", zap.Error(err))
				return
			}
		}

		// Run common validation checks
		if !b.validateInteraction(event, s, isNewSession, false, event.Data.CustomID()) {
			return
		}

		// Handle interaction and update session
		b.paginationManager.HandleInteraction(event, s)
	}()
}

// handleModalSubmit processes form submissions similarly to component interactions.
// It updates the message to show "Processing..." and removes interactive components,
// then processes the submission in a goroutine.
func (b *Bot) handleModalSubmit(event *events.ModalSubmitInteractionCreate) {
	go func() {
		// Update message to prevent double-submissions
		updateBuilder := discord.NewMessageUpdateBuilder().
			SetContent(utils.GetTimestampedSubtext("Processing...")).
			ClearContainerComponents()

		if err := event.UpdateMessage(updateBuilder.Build()); err != nil {
			b.logger.Error("Failed to update message", zap.Error(err))
			return
		}

		start := time.Now()
		defer func() {
			if r := recover(); r != nil {
				formData := make(map[string]string)
				for id, comp := range event.Data.Components {
					formData[id] = fmt.Sprintf("Component type: %T", comp)
				}

				b.logger.Error("Modal submission failed",
					zap.String("modal_id", event.Data.CustomID),
					zap.String("user_id", event.User().ID.String()),
					zap.Any("form_data", formData),
					zap.Any("panic", r),
				)
				b.paginationManager.RespondWithError(event, "Internal error. Please report this to an administrator.")
			}
			duration := time.Since(start)
			b.logger.Debug("Modal submit interaction handled",
				zap.String("custom_id", event.Data.CustomID),
				zap.Duration("duration", duration))
		}()

		// Validate session but return early if session creation failed or session expired
		s, isNewSession, ok := b.validateAndGetSession(event, event.User().ID)
		if !ok {
			return
		}

		// Run common validation checks
		if !b.validateInteraction(event, s, isNewSession, false, "") {
			return
		}

		// Handle submission and update session
		b.paginationManager.HandleInteraction(event, s)
	}()
}

// validateInteraction performs common validation checks for all interaction types.
// Returns true if the interaction should proceed, false if it should be stopped.
func (b *Bot) validateInteraction(
	event interfaces.CommonEvent, s *session.Session, isNewSession, isCommandEvent bool, customID string,
) bool {
	// Check if system is in maintenance mode
	pageName := session.CurrentPage.Get(s)
	if session.BotAnnouncementType.Get(s) == enum.AnnouncementTypeMaintenance {
		isAdmin := s.BotSettings().IsAdmin(uint64(event.User().ID))
		if !isAdmin && (pageName != constants.DashboardPageName || customID != constants.RefreshButtonCustomID) {
			b.paginationManager.Show(event, s, constants.DashboardPageName, "System is currently under maintenance.")
			return false
		}
	}

	// Check if user is banned
	if b.checkBanStatus(event, s, pageName, isCommandEvent) {
		return false
	}

	// Check consent for new sessions
	if isNewSession && b.checkConsentStatus(event, s) {
		return false
	}

	// Check if the session has a valid current page
	page := b.paginationManager.GetPage(pageName)
	if page == nil {
		// If no valid page exists, reset to dashboard
		b.paginationManager.Show(event, s, constants.DashboardPageName, "New session created.")
		s.Touch(context.Background())
		return false
	}

	return true
}

// checkBanStatus checks if a user is banned and shows the ban menu if they are.
// Returns true if the user is banned and should not proceed.
func (b *Bot) checkBanStatus(event interfaces.CommonEvent, s *session.Session, pageName string, isCommandEvent bool) bool {
	userID := uint64(event.User().ID)

	// Check if user is banned
	banned, err := b.db.Models().Bans().IsBanned(context.Background(), userID)
	if err != nil {
		b.logger.Error("Failed to check ban status",
			zap.Error(err),
			zap.Uint64("user_id", userID))
		b.paginationManager.RespondWithError(event, "Failed to verify access status. Please try again later.")
		return true
	}

	// If not banned, allow access
	if !banned {
		return false
	}

	// Handle banned user interactions
	if !isCommandEvent && (pageName == constants.BanPageName ||
		pageName == constants.AppealOverviewPageName ||
		pageName == constants.AppealTicketPageName ||
		pageName == constants.AppealVerifyPageName) {
		b.paginationManager.HandleInteraction(event, s)
		return true
	}

	// User is banned, show ban menu
	b.paginationManager.Show(event, s, constants.BanPageName, "")
	s.Touch(context.Background())
	return true
}

// checkConsentStatus checks if a user has consented and shows the consent menu if not.
// Returns true if the user needs to consent (hasn't consented yet).
func (b *Bot) checkConsentStatus(event interfaces.CommonEvent, s *session.Session) bool {
	hasConsented, err := b.db.Models().Consent().HasConsented(context.Background(), uint64(event.User().ID))
	if err != nil {
		b.logger.Error("Failed to check consent status", zap.Error(err))
		return true
	}

	if !hasConsented {
		// Show consent menu first
		b.paginationManager.Show(event, s, constants.ConsentPageName, "")
		s.Touch(context.Background())
		return true
	}

	return false
}

// validateAndGetSession retrieves or creates a session for the given user and validates its state.
func (b *Bot) validateAndGetSession(event interfaces.CommonEvent, userID snowflake.ID) (*session.Session, bool, bool) {
	// Check if user is a guild owner if bot is in the guild
	isGuildOwner := false
	if guildID := event.GuildID(); guildID != nil {
		if _, err := event.Client().Rest().GetGuild(*guildID, false); err == nil {
			isGuildOwner = event.Member().Permissions.Has(discord.PermissionAdministrator)
		}
	}

	// Get or create user session
	s, isNewSession, err := b.sessionManager.GetOrCreateSession(context.Background(), userID, isGuildOwner)
	if err != nil {
		if errors.Is(err, session.ErrSessionLimitReached) {
			b.paginationManager.RespondWithError(event, "Session limit reached. Please try again later.")
		} else {
			b.logger.Error("Failed to get or create session", zap.Error(err))
			b.paginationManager.RespondWithError(event, "Failed to get or create session.")
		}
		return nil, false, false
	}

	return s, isNewSession, true
}
