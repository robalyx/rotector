package bot

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	aGateway "github.com/diamondburned/arikawa/v3/gateway"
	"github.com/diamondburned/arikawa/v3/state"
	"github.com/disgoorg/disgo"
	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	disgoEvents "github.com/disgoorg/disgo/events"
	"github.com/disgoorg/disgo/gateway"
	"github.com/disgoorg/disgo/sharding"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/interaction"
	"github.com/robalyx/rotector/internal/bot/core/session"
	eventHandler "github.com/robalyx/rotector/internal/bot/events"
	"github.com/robalyx/rotector/internal/bot/handlers/admin"
	"github.com/robalyx/rotector/internal/bot/handlers/appeal"
	"github.com/robalyx/rotector/internal/bot/handlers/ban"
	"github.com/robalyx/rotector/internal/bot/handlers/captcha"
	"github.com/robalyx/rotector/internal/bot/handlers/chat"
	"github.com/robalyx/rotector/internal/bot/handlers/consent"
	"github.com/robalyx/rotector/internal/bot/handlers/dashboard"
	"github.com/robalyx/rotector/internal/bot/handlers/guild"
	"github.com/robalyx/rotector/internal/bot/handlers/leaderboard"
	"github.com/robalyx/rotector/internal/bot/handlers/log"
	"github.com/robalyx/rotector/internal/bot/handlers/queue"
	groupReview "github.com/robalyx/rotector/internal/bot/handlers/review/group"
	userReview "github.com/robalyx/rotector/internal/bot/handlers/review/user"
	"github.com/robalyx/rotector/internal/bot/handlers/reviewer"
	"github.com/robalyx/rotector/internal/bot/handlers/selector"
	"github.com/robalyx/rotector/internal/bot/handlers/setting"
	"github.com/robalyx/rotector/internal/bot/handlers/status"
	"github.com/robalyx/rotector/internal/bot/handlers/timeout"
	"github.com/robalyx/rotector/internal/bot/utils"
	"github.com/robalyx/rotector/internal/database"
	"github.com/robalyx/rotector/internal/database/types/enum"
	"github.com/robalyx/rotector/internal/setup"
	"go.uber.org/zap"
)

// Bot handles all the layouts and managers needed for Discord interaction.
type Bot struct {
	db                 database.Client
	client             bot.Client
	logger             *zap.Logger
	sessionManager     *session.Manager
	interactionManager *interaction.Manager
	guildEventHandler  *eventHandler.GuildEventHandler
}

// New initializes a Bot instance by creating all required managers and layouts.
func New(app *setup.App) (*Bot, error) {
	logger := app.Logger.Named("bot")

	// Initialize session manager for persistent storage
	sessionManager, err := session.NewManager(app.DB, app.RedisManager, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create session manager: %w", err)
	}
	interactionManager := interaction.NewManager(sessionManager, logger)
	guildEventHandler := eventHandler.NewGuildEventHandler(logger)

	// Initialize self-bot client
	selfClient := state.NewWithIntents(app.Config.Common.Discord.SyncToken,
		aGateway.IntentGuilds|aGateway.IntentGuildMembers)

	// Disguise user agent
	selfClient.UserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:136.0) Gecko/20100101 Firefox/136.0"

	// Create bot instance
	b := &Bot{
		db:                 app.DB,
		logger:             logger,
		sessionManager:     sessionManager,
		interactionManager: interactionManager,
		guildEventHandler:  guildEventHandler,
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
		bot.WithEventListeners(&disgoEvents.ListenerAdapter{
			OnApplicationCommandInteraction: b.handleApplicationCommandInteraction,
			OnComponentInteraction:          b.handleComponentInteraction,
			OnModalSubmit:                   b.handleModalSubmit,
			OnGuildJoin:                     b.guildEventHandler.OnGuildJoin,
		}),
	)
	if err != nil {
		return nil, err
	}
	b.client = client

	// Initialize layouts
	selectorLayout := selector.New(app, sessionManager)
	settingLayout := setting.New(app)
	logLayout := log.New(app)
	chatLayout := chat.New(app)
	captchaLayout := captcha.New(app)
	timeoutLayout := timeout.New(app)
	userReviewLayout := userReview.New(app, interactionManager)
	groupReviewLayout := groupReview.New(app, interactionManager)
	appealLayout := appeal.New(app)
	adminLayout := admin.New(app)
	leaderboardLayout := leaderboard.New(app, client)
	statusLayout := status.New(app)
	dashboardLayout := dashboard.New(app, sessionManager)
	consentLayout := consent.New(app)
	banLayout := ban.New(app, sessionManager)
	reviewerLayout := reviewer.New(app, client)
	guildLayout := guild.New(app, selfClient)
	queueLayout := queue.New(app)

	interactionManager.AddPages(selectorLayout.Pages())
	interactionManager.AddPages(settingLayout.Pages())
	interactionManager.AddPages(logLayout.Pages())
	interactionManager.AddPages(chatLayout.Pages())
	interactionManager.AddPages(captchaLayout.Pages())
	interactionManager.AddPages(timeoutLayout.Pages())
	interactionManager.AddPages(userReviewLayout.Pages())
	interactionManager.AddPages(groupReviewLayout.Pages())
	interactionManager.AddPages(appealLayout.Pages())
	interactionManager.AddPages(adminLayout.Pages())
	interactionManager.AddPages(leaderboardLayout.Pages())
	interactionManager.AddPages(statusLayout.Pages())
	interactionManager.AddPages(dashboardLayout.Pages())
	interactionManager.AddPages(consentLayout.Pages())
	interactionManager.AddPages(banLayout.Pages())
	interactionManager.AddPages(reviewerLayout.Pages())
	interactionManager.AddPages(guildLayout.Pages())
	interactionManager.AddPages(queueLayout.Pages())

	return b, nil
}

// Start registers global commands with Discord and opens the gateway connection.
func (b *Bot) Start() error {
	b.logger.Info("Registering commands")

	// Register the dashboard command globally
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

// handleApplicationCommandInteraction processes slash commands.
func (b *Bot) handleApplicationCommandInteraction(event *disgoEvents.ApplicationCommandInteractionCreate) {
	go func() {
		start := time.Now()
		wrappedEvent := interaction.WrapEvent(event, nil)
		defer func() {
			if r := recover(); r != nil {
				b.logger.Error("Application command interaction failed",
					zap.String("command", event.SlashCommandInteractionData().CommandName()),
					zap.String("user_id", wrappedEvent.User().ID.String()),
					zap.Any("panic", r),
				)
				b.interactionManager.RespondWithError(wrappedEvent, "Internal error. Please report this to an administrator.")
			}
			duration := time.Since(start)
			b.logger.Debug("Application command interaction handled",
				zap.String("command", event.SlashCommandInteractionData().CommandName()),
				zap.Duration("duration", duration))
		}()

		// Defer response to prevent Discord timeout while processing
		if err := event.DeferCreateMessage(true); err != nil {
			b.logger.Error("Failed to defer create message", zap.Error(err))
			return
		}

		// Only handle dashboard command
		if event.SlashCommandInteractionData().CommandName() != constants.RotectorCommandName {
			b.interactionManager.RespondWithError(wrappedEvent, "This command is not available.")
			return
		}

		// Get initial response message
		message, err := event.Client().Rest().GetInteractionResponse(event.ApplicationID(), event.Token())
		if err != nil {
			b.logger.Error("Failed to get interaction response", zap.Error(err))
			b.interactionManager.RespondWithError(wrappedEvent, "Failed to initialize session. Please try again.")
			return
		}
		wrappedEvent.SetMessage(message)

		// Initialize session
		s, isNewSession, showSelector, err := b.initializeSession(wrappedEvent, message, false)
		if err != nil {
			return
		}

		// Check if we should show the selector menu
		if showSelector {
			b.interactionManager.Show(wrappedEvent, s, constants.SessionSelectorPageName, "")
			s.Touch(context.Background())
			return
		}

		// Run common validation checks
		if !b.validateInteraction(wrappedEvent, s, isNewSession, true, "") {
			return
		}

		// Navigate to stored page
		pageName := session.CurrentPage.Get(s)
		b.interactionManager.Show(wrappedEvent, s, pageName, "")
		s.Touch(context.Background())
	}()
}

// handleComponentInteraction processes button clicks and select menu choices.
func (b *Bot) handleComponentInteraction(event *disgoEvents.ComponentInteractionCreate) {
	go func() {
		start := time.Now()
		wrappedEvent := interaction.WrapEvent(event, &event.Message)
		defer func() {
			if r := recover(); r != nil {
				b.logger.Error("Component interaction failed",
					zap.String("component_id", event.Data.CustomID()),
					zap.String("component_type", fmt.Sprintf("%T", event.Data)),
					zap.String("user_id", wrappedEvent.User().ID.String()),
					zap.Any("panic", r),
				)
				b.interactionManager.RespondWithError(wrappedEvent, "Internal error. Please report this to an administrator.")
			}
			duration := time.Since(start)
			b.logger.Debug("Component interaction handled",
				zap.String("custom_id", event.Data.CustomID()),
				zap.Duration("duration", duration))
		}()

		// Initialize session
		s, isNewSession, showSelector, err := b.initializeSession(wrappedEvent, &event.Message, true)
		if err != nil {
			return
		}

		// Get current page
		page := b.interactionManager.GetPage(session.CurrentPage.Get(s))

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
				AddComponents(utils.CreateTimestampedTextDisplay("Processing...")).
				AddFlags(discord.MessageFlagIsComponentsV2)

			if err := event.UpdateMessage(updateBuilder.Build()); err != nil {
				b.logger.Error("Failed to update message", zap.Error(err))
				return
			}
		}

		// Check if we should show the selector menu
		if showSelector {
			b.interactionManager.Show(wrappedEvent, s, constants.SessionSelectorPageName, "")
			s.Touch(context.Background())
			return
		}

		// Run common validation checks
		if !b.validateInteraction(wrappedEvent, s, isNewSession, false, event.Data.CustomID()) {
			return
		}

		// Handle interaction and update session
		b.interactionManager.HandleInteraction(wrappedEvent, s)
	}()
}

// handleModalSubmit processes modal form submissions.
func (b *Bot) handleModalSubmit(event *disgoEvents.ModalSubmitInteractionCreate) {
	go func() {
		start := time.Now()
		wrappedEvent := interaction.WrapEvent(event, nil)
		defer func() {
			if r := recover(); r != nil {
				formData := make(map[int]string)
				for comp := range event.Data.AllComponents() {
					formData[comp.GetID()] = fmt.Sprintf("Component type: %T", comp)
				}

				b.logger.Error("Modal submission failed",
					zap.String("modal_id", event.Data.CustomID),
					zap.String("user_id", wrappedEvent.User().ID.String()),
					zap.Any("form_data", formData),
					zap.Any("panic", r),
				)
				b.interactionManager.RespondWithError(wrappedEvent, "Internal error. Please report this to an administrator.")
			}
			duration := time.Since(start)
			b.logger.Debug("Modal submit interaction handled",
				zap.String("custom_id", event.Data.CustomID),
				zap.Duration("duration", duration))
		}()

		// Update message to prevent double-submissions
		updateBuilder := discord.NewMessageUpdateBuilder().
			AddComponents(utils.CreateTimestampedTextDisplay("Processing...")).
			AddFlags(discord.MessageFlagIsComponentsV2)

		if err := event.UpdateMessage(updateBuilder.Build()); err != nil {
			b.logger.Error("Failed to update message", zap.Error(err))
			return
		}

		// Get response message
		message, err := event.Client().Rest().GetInteractionResponse(event.ApplicationID(), event.Token())
		if err != nil {
			b.logger.Error("Failed to get interaction response", zap.Error(err))
			b.interactionManager.RespondWithError(wrappedEvent, "Failed to initialize session. Please try again.")
			return
		}
		wrappedEvent.SetMessage(message)

		// Initialize session
		s, isNewSession, showSelector, err := b.initializeSession(wrappedEvent, message, false)
		if err != nil {
			return
		}

		// Check if we should show the selector menu
		if showSelector {
			b.interactionManager.Show(wrappedEvent, s, constants.SessionSelectorPageName, "")
			s.Touch(context.Background())
			return
		}

		// Run common validation checks
		if !b.validateInteraction(wrappedEvent, s, isNewSession, false, "") {
			return
		}

		// Handle submission and update session
		b.interactionManager.HandleInteraction(wrappedEvent, s)
	}()
}

// initializeSession creates or retrieves a session for the given user and message.
func (b *Bot) initializeSession(
	event interaction.CommonEvent, message *discord.Message, isInteraction bool,
) (s *session.Session, isNewSession bool, showSelector bool, err error) {
	userID := event.User().ID

	// NOTE: Do not use the interaction manager since
	// the interaction has not been responded to yet
	updateMessage := func(content string) {
		messageUpdate := discord.NewMessageUpdateBuilder().
			ClearFiles().
			ClearComponents().
			RetainAttachments().
			AddComponents(utils.CreateTimestampedTextDisplay(content)).
			AddFlags(discord.MessageFlagIsComponentsV2).
			Build()

		if err := event.UpdateMessage(messageUpdate); err != nil {
			b.logger.Error("Failed to update message", zap.Error(err))
		}
	}

	// Check for existing sessions
	existingSessions, err := b.sessionManager.GetUserSessions(context.Background(), uint64(userID), !isInteraction)
	if err != nil {
		b.logger.Error("Failed to check existing sessions", zap.Error(err))
		updateMessage("Failed to check existing sessions. Please try again.")
		return nil, false, false, err
	}

	// Create new session
	s, isNewSession, err = b.sessionManager.GetOrCreateSession(
		context.Background(), userID, uint64(message.ID),
		event.Member().Permissions.Has(discord.PermissionAdministrator),
		isInteraction,
	)
	if err != nil {
		switch {
		case errors.Is(err, session.ErrSessionLimitReached):
			updateMessage("The global session limit has been reached. Please wait for other users to finish their sessions.")
		case errors.Is(err, session.ErrSessionNotFound):
			updateMessage("This session has expired. Please start a new session by using the /rotector command.")
		default:
			updateMessage("Failed to create session. Please try again.")
		}
		return nil, false, false, err
	}

	// If there are existing sessions and this is a new session, show selector menu
	showSelector = len(existingSessions) > 0 && isNewSession
	if showSelector {
		session.ExistingSessions.Set(s, existingSessions)
	}

	return s, isNewSession, showSelector, nil
}

// validateInteraction performs common validation checks for all interaction types.
// Returns true if the interaction should proceed, false if it should be stopped.
func (b *Bot) validateInteraction(
	event interaction.CommonEvent, s *session.Session, isNewSession, isCommandEvent bool, customID string,
) bool {
	// Check if system is in maintenance mode
	pageName := session.CurrentPage.Get(s)
	if session.BotAnnouncementType.Get(s) == enum.AnnouncementTypeMaintenance {
		isAdmin := s.BotSettings().IsAdmin(uint64(event.User().ID))
		if !isAdmin && (pageName != constants.DashboardPageName || customID != constants.RefreshButtonCustomID) {
			b.interactionManager.Show(event, s, constants.DashboardPageName, "System is currently under maintenance.")
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
	page := b.interactionManager.GetPage(pageName)
	if page == nil {
		// If no valid page exists, reset to dashboard
		b.interactionManager.Show(event, s, constants.DashboardPageName, "New session created.")
		s.Touch(context.Background())
		return false
	}

	return true
}

// checkBanStatus checks if a user is banned and shows the ban menu if they are.
// Returns true if the user is banned and should not proceed.
func (b *Bot) checkBanStatus(event interaction.CommonEvent, s *session.Session, pageName string, isCommandEvent bool) bool {
	userID := uint64(event.User().ID)

	// Check if user is banned
	banned, err := b.db.Model().Ban().IsBanned(context.Background(), userID)
	if err != nil {
		b.logger.Error("Failed to check ban status",
			zap.Error(err),
			zap.Uint64("user_id", userID))
		b.interactionManager.RespondWithError(event, "Failed to verify access status. Please try again later.")
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
		b.interactionManager.HandleInteraction(event, s)
		return true
	}

	// User is banned, show ban menu
	b.interactionManager.Show(event, s, constants.BanPageName, "")
	s.Touch(context.Background())
	return true
}

// checkConsentStatus checks if a user has consented and shows the consent menu if not.
// Returns true if the user needs to consent (hasn't consented yet).
func (b *Bot) checkConsentStatus(event interaction.CommonEvent, s *session.Session) bool {
	hasConsented, err := b.db.Model().Consent().HasConsented(context.Background(), uint64(event.User().ID))
	if err != nil {
		b.logger.Error("Failed to check consent status", zap.Error(err))
		return true
	}

	if !hasConsented {
		// Show consent menu first
		b.interactionManager.Show(event, s, constants.ConsentPageName, "")
		s.Touch(context.Background())
		return true
	}

	return false
}
