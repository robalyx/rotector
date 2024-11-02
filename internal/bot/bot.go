package bot

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/disgoorg/disgo"
	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/disgo/gateway"
	"go.uber.org/zap"

	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/rotector/rotector/internal/bot/constants"
	"github.com/rotector/rotector/internal/bot/handlers/dashboard"
	"github.com/rotector/rotector/internal/bot/handlers/log"
	"github.com/rotector/rotector/internal/bot/handlers/queue"
	"github.com/rotector/rotector/internal/bot/handlers/review"
	"github.com/rotector/rotector/internal/bot/handlers/setting"
	"github.com/rotector/rotector/internal/bot/pagination"
	"github.com/rotector/rotector/internal/bot/session"
	"github.com/rotector/rotector/internal/bot/utils"
	"github.com/rotector/rotector/internal/common/database"
	queueManager "github.com/rotector/rotector/internal/common/queue"
	"github.com/rotector/rotector/internal/common/redis"
	"github.com/rotector/rotector/internal/common/statistics"
)

// Bot represents the Discord bot.
type Bot struct {
	db                *database.Database
	client            bot.Client
	logger            *zap.Logger
	sessionManager    *session.Manager
	paginationManager *pagination.Manager
	dashboardHandler  *dashboard.Handler
	reviewHandler     *review.Handler
	settingHandler    *setting.Handler
	logHandler        *log.Handler
}

// New creates a new Bot instance.
func New(
	token string,
	db *database.Database,
	stats *statistics.Statistics,
	roAPI *api.API,
	queueManager *queueManager.Manager,
	redisManager *redis.Manager,
	logger *zap.Logger,
) (*Bot, error) {
	sessionManager, err := session.NewManager(db, redisManager, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create session manager: %w", err)
	}
	paginationManager := pagination.NewManager(logger)

	// Initialize the handlers
	dashboardHandler := dashboard.New(db, stats, logger, sessionManager, paginationManager)
	reviewHandler := review.New(db, logger, roAPI, sessionManager, paginationManager, queueManager, dashboardHandler)
	settingHandler := setting.New(db, logger, sessionManager, paginationManager, dashboardHandler)
	logHandler := log.New(db, sessionManager, paginationManager, dashboardHandler, logger)
	queueHandler := queue.New(db, logger, sessionManager, paginationManager, queueManager, dashboardHandler, reviewHandler)

	dashboardHandler.SetReviewHandler(reviewHandler)
	dashboardHandler.SetSettingsHandler(settingHandler)
	dashboardHandler.SetLogsHandler(logHandler)
	dashboardHandler.SetQueueHandler(queueHandler)

	// Initialize the bot
	b := &Bot{
		db:                db,
		logger:            logger,
		sessionManager:    sessionManager,
		paginationManager: paginationManager,
		dashboardHandler:  dashboardHandler,
		reviewHandler:     reviewHandler,
		settingHandler:    settingHandler,
		logHandler:        logHandler,
	}

	// Initialize the Discord client
	client, err := disgo.New(token,
		bot.WithGatewayConfigOpts(
			gateway.WithIntents(
				gateway.IntentGuilds,
				gateway.IntentGuildMessages,
				gateway.IntentDirectMessages,
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
	return b, nil
}

// Start initializes and starts the bot.
func (b *Bot) Start() error {
	b.logger.Info("Registering commands")

	_, err := b.client.Rest().SetGlobalCommands(b.client.ApplicationID(), []discord.ApplicationCommandCreate{
		discord.SlashCommandCreate{
			Name:        constants.DashboardCommandName,
			Description: "View the dashboard",
		},
	})
	if err != nil {
		return fmt.Errorf("failed to register commands: %w", err)
	}

	b.logger.Info("Starting bot")
	return b.client.OpenGateway(context.Background())
}

// Close gracefully shuts down the bot.
func (b *Bot) Close() {
	b.logger.Info("Closing bot")
	b.client.Close(context.Background())
}

// handleApplicationCommandInteraction processes application command interactions.
func (b *Bot) handleApplicationCommandInteraction(event *events.ApplicationCommandInteractionCreate) {
	// Defer the response
	if err := event.DeferCreateMessage(true); err != nil {
		b.logger.Error("Failed to defer create message", zap.Error(err))
		return
	}

	// Respond with an error if the command is not the dashboard command
	if event.SlashCommandInteractionData().CommandName() != constants.DashboardCommandName {
		b.paginationManager.RespondWithError(event, "This command is not available.")
		return
	}

	// Get the guild settings
	guildSettings, err := b.db.Settings().GetGuildSettings(uint64(*event.GuildID()))
	if err != nil {
		b.logger.Error("Failed to get guild settings", zap.Error(err))
		return
	}

	// Respond with an error if the user is not in the whitelisted roles
	if !guildSettings.HasAnyRole(event.Member().RoleIDs) {
		b.paginationManager.RespondWithError(event, "You are not authorized to use this command.")
		return
	}

	// Handle the interaction in a goroutine
	go func() {
		start := time.Now()
		defer func() {
			if r := recover(); r != nil {
				b.logger.Error("Panic in application command interaction handler", zap.Any("panic", r))
			}
			duration := time.Since(start)
			b.logger.Debug("Application command interaction handled",
				zap.String("command", event.SlashCommandInteractionData().CommandName()),
				zap.Duration("duration", duration))
		}()

		// Get or create a session for the user
		s, err := b.sessionManager.GetOrCreateSession(context.Background(), event.User().ID)
		if err != nil {
			b.logger.Error("Failed to get or create session", zap.Error(err))
			b.paginationManager.RespondWithError(event, "Failed to get or create session.")
			return
		}

		// Save session data after handling the interaction
		b.dashboardHandler.ShowDashboard(event, s, "")
		s.Touch(context.Background())
	}()
}

// handleComponentInteraction processes component interactions.
func (b *Bot) handleComponentInteraction(event *events.ComponentInteractionCreate) {
	// WORKAROUND:
	// Check if the interaction is something other than opening a modal so that we can defer the message update.
	// If we are opening a modal and we try to defer, there will be an error that the interaction is already responded to.
	// Please open a PR if you have a better solution or a fix for this.
	isModal := false
	stringSelectData, ok := event.Data.(discord.StringSelectMenuInteractionData)
	if ok && strings.HasSuffix(stringSelectData.Values[0], "modal") {
		isModal = true
	}

	if !isModal {
		// Create a new message update builder
		updateBuilder := discord.NewMessageUpdateBuilder().
			SetContent(utils.GetTimestampedSubtext("Processing...")).
			ClearContainerComponents()

		// Update the message without interactable components
		if err := event.UpdateMessage(updateBuilder.Build()); err != nil {
			b.logger.Error("Failed to update message", zap.Error(err))
			return
		}
	}

	// Handle the interaction in a goroutine
	go func() {
		start := time.Now()
		defer func() {
			if r := recover(); r != nil {
				b.logger.Error("Panic in component interaction handler", zap.Any("panic", r))
			}
			duration := time.Since(start)
			b.logger.Debug("Component interaction handled",
				zap.String("custom_id", event.Data.CustomID()),
				zap.Duration("duration", duration))
		}()

		// Get or create a session for the user
		s, err := b.sessionManager.GetOrCreateSession(context.Background(), event.User().ID)
		if err != nil {
			b.logger.Error("Failed to get or create session", zap.Error(err))
			b.paginationManager.RespondWithError(event, "Failed to get or create session.")
			return
		}

		// Ensure the interaction is for the latest message
		if s.GetUint64(constants.SessionKeyMessageID) != uint64(event.Message.ID) {
			b.logger.Debug("Interaction is outdated",
				zap.Uint64("session_message_id", s.GetUint64(constants.SessionKeyMessageID)),
				zap.Uint64("event_message_id", uint64(event.Message.ID)))
			b.paginationManager.RespondWithError(event, "This interaction is outdated. Please use the latest interaction.")
			return
		}

		// Save session data after handling the interaction
		b.paginationManager.HandleInteraction(event, s)
		s.Touch(context.Background())
	}()
}

// handleModalSubmit processes modal submit interactions.
func (b *Bot) handleModalSubmit(event *events.ModalSubmitInteractionCreate) {
	// Create a new message update builder
	updateBuilder := discord.NewMessageUpdateBuilder().
		SetContent(utils.GetTimestampedSubtext("Processing...")).
		ClearContainerComponents()

	// Update the message without interactable components
	if err := event.UpdateMessage(updateBuilder.Build()); err != nil {
		b.logger.Error("Failed to update message", zap.Error(err))
		return
	}

	// Handle the interaction in a goroutine
	go func() {
		start := time.Now()
		defer func() {
			if r := recover(); r != nil {
				b.logger.Error("Panic in modal submit interaction handler", zap.Any("panic", r))
			}
			duration := time.Since(start)
			b.logger.Debug("Modal submit interaction handled",
				zap.String("custom_id", event.Data.CustomID),
				zap.Duration("duration", duration))
		}()

		// Get or create a session for the user
		s, err := b.sessionManager.GetOrCreateSession(context.Background(), event.User().ID)
		if err != nil {
			b.logger.Error("Failed to get or create session", zap.Error(err))
			b.paginationManager.RespondWithError(event, "Failed to get or create session.")
			return
		}

		// Save session data after handling the interaction
		b.paginationManager.HandleInteraction(event, s)
		s.Touch(context.Background())
	}()
}
