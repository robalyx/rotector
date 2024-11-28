package bot

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/disgoorg/disgo"
	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/disgo/gateway"
	"github.com/disgoorg/snowflake/v2"
	"go.uber.org/zap"

	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/rotector/rotector/internal/bot/constants"
	"github.com/rotector/rotector/internal/bot/core/pagination"
	"github.com/rotector/rotector/internal/bot/core/session"
	"github.com/rotector/rotector/internal/bot/interfaces"
	"github.com/rotector/rotector/internal/bot/menu/dashboard"
	"github.com/rotector/rotector/internal/bot/menu/log"
	"github.com/rotector/rotector/internal/bot/menu/queue"
	groupReview "github.com/rotector/rotector/internal/bot/menu/review/group"
	userReview "github.com/rotector/rotector/internal/bot/menu/review/user"
	"github.com/rotector/rotector/internal/bot/menu/setting"
	"github.com/rotector/rotector/internal/bot/utils"
	queueManager "github.com/rotector/rotector/internal/common/queue"
	"github.com/rotector/rotector/internal/common/storage/database"
	"github.com/rotector/rotector/internal/common/storage/redis"
)

// Bot handles all the layouts and managers needed for Discord interaction.
// It maintains connections to the database, Discord client, and various layouts
// while processing user interactions through the session and pagination managers.
type Bot struct {
	db                *database.Client
	client            bot.Client
	logger            *zap.Logger
	sessionManager    *session.Manager
	paginationManager *pagination.Manager
	dashboardLayout   interfaces.DashboardLayout
	userReviewLayout  interfaces.UserReviewLayout
	settingLayout     interfaces.SettingLayout
	logLayout         interfaces.LogLayout
}

// New initializes a Bot instance by creating all required managers and layouts.
// It connects layouts through dependency injection and configures the Discord
// client with necessary gateway intents and event listeners.
func New(
	token string,
	db *database.Client,
	roAPI *api.API,
	queueManager *queueManager.Manager,
	redisManager *redis.Manager,
	logger *zap.Logger,
) (*Bot, error) {
	// Initialize session manager for persistent storage
	sessionManager, err := session.NewManager(db, redisManager, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create session manager: %w", err)
	}
	paginationManager := pagination.NewManager(sessionManager, logger)

	// Create layouts with their dependencies
	dashboardLayout := dashboard.New(db, logger, sessionManager, paginationManager, redisManager)
	logLayout := log.New(db, sessionManager, paginationManager, dashboardLayout, logger)
	settingLayout := setting.New(db, logger, sessionManager, paginationManager, dashboardLayout)
	userReviewLayout := userReview.New(db, logger, roAPI, sessionManager, paginationManager, queueManager, dashboardLayout, logLayout)
	groupReviewLayout := groupReview.New(db, logger, roAPI, sessionManager, paginationManager, dashboardLayout, logLayout)
	queueLayout := queue.New(db, logger, sessionManager, paginationManager, queueManager, dashboardLayout, userReviewLayout)

	// Cross-link layouts to enable navigation between different sections
	dashboardLayout.SetLogLayout(logLayout)
	dashboardLayout.SetSettingLayout(settingLayout)
	dashboardLayout.SetUserReviewLayout(userReviewLayout)
	dashboardLayout.SetGroupReviewLayout(groupReviewLayout)
	dashboardLayout.SetQueueLayout(queueLayout)

	// Initialize bot structure with all components
	b := &Bot{
		db:                db,
		logger:            logger,
		sessionManager:    sessionManager,
		paginationManager: paginationManager,
		dashboardLayout:   dashboardLayout,
		userReviewLayout:  userReviewLayout,
		settingLayout:     settingLayout,
		logLayout:         logLayout,
	}

	// Configure Discord client with required gateway intents and event handlers
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

// Start registers global commands with Discord and opens the gateway connection.
// It first ensures the dashboard command is registered globally before starting
// the bot's gateway connection to receive events.
func (b *Bot) Start() error {
	b.logger.Info("Registering commands")

	// Register the dashboard command globally for all guilds
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

// Close gracefully shuts down the Discord gateway connection.
// This ensures all pending events are processed before shutdown.
func (b *Bot) Close() {
	b.logger.Info("Closing bot")
	b.client.Close(context.Background())
}

// handleApplicationCommandInteraction processes slash commands by first deferring the response,
// then validating guild settings and user permissions before handling the command in a goroutine.
// The goroutine approach allows for concurrent processing of commands.
func (b *Bot) handleApplicationCommandInteraction(event *events.ApplicationCommandInteractionCreate) {
	go func() {
		// Defer response to prevent Discord timeout while processing
		if err := event.DeferCreateMessage(true); err != nil {
			b.logger.Error("Failed to defer create message", zap.Error(err))
			return
		}

		// Only handle dashboard command - respond with error for unknown commands
		if event.SlashCommandInteractionData().CommandName() != constants.DashboardCommandName {
			b.paginationManager.RespondWithError(event, "This command is not available.")
			return
		}

		start := time.Now()
		defer func() {
			if r := recover(); r != nil {
				b.logger.Error("Panic in application command interaction handler", zap.Any("panic", r))
				b.paginationManager.RespondWithError(event, "Internal error. Please report this to an administrator.")
			}
			duration := time.Since(start)
			b.logger.Debug("Application command interaction handled",
				zap.String("command", event.SlashCommandInteractionData().CommandName()),
				zap.Duration("duration", duration))
		}()

		// Validate session but return early if session creation failed or session expired
		s, ok := b.validateAndGetSession(event, event.User().ID)
		if !ok {
			return
		}

		// Navigate to stored page or show dashboard
		currentPage := s.GetString(constants.SessionKeyCurrentPage)
		page := b.paginationManager.GetPage(currentPage)
		b.paginationManager.NavigateTo(event, s, page, "")

		s.Touch(context.Background())
	}()
}

// handleComponentInteraction processes button clicks and select menu choices.
// It first updates the message to show "Processing..." and removes interactive components
// to prevent double-clicks, then processes the interaction in a goroutine.
func (b *Bot) handleComponentInteraction(event *events.ComponentInteractionCreate) {
	go func() {
		// WORKAROUND:
		// Special handling for modal interactions to prevent response conflicts.
		// If we are opening a modal and we try to defer, there will be an error
		// that the interaction is already responded to.
		// Please open a PR if you have a better solution or a fix for this.
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
		if !isModal {
			updateBuilder := discord.NewMessageUpdateBuilder().
				SetContent(utils.GetTimestampedSubtext("Processing...")).
				ClearContainerComponents()

			if err := event.UpdateMessage(updateBuilder.Build()); err != nil {
				b.logger.Error("Failed to update message", zap.Error(err))
				return
			}
		}

		start := time.Now()
		defer func() {
			if r := recover(); r != nil {
				b.logger.Error("Panic in component interaction handler", zap.Any("panic", r))
				b.paginationManager.RespondWithError(event, "Internal error. Please report this to an administrator.")
			}
			duration := time.Since(start)
			b.logger.Debug("Component interaction handled",
				zap.String("custom_id", event.Data.CustomID()),
				zap.Duration("duration", duration))
		}()

		// Validate session but return early if session creation failed or session expired
		s, ok := b.validateAndGetSession(event, event.User().ID)
		if !ok {
			return
		}

		// Verify interaction is for latest message
		sessionMessageID := s.GetUint64(constants.SessionKeyMessageID)
		if sessionMessageID != uint64(event.Message.ID) {
			b.logger.Debug("Interaction is outdated",
				zap.Uint64("session_message_id", sessionMessageID),
				zap.Uint64("event_message_id", uint64(event.Message.ID)))
			b.paginationManager.RespondWithError(event, "This interaction is outdated. Please use the latest interaction.")
			return
		}

		// Handle interaction and update session
		b.paginationManager.HandleInteraction(event, s)
		s.Touch(context.Background())
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
				b.logger.Error("Panic in modal submit interaction handler", zap.Any("panic", r))
				b.paginationManager.RespondWithError(event, "Internal error. Please report this to an administrator.")
			}
			duration := time.Since(start)
			b.logger.Debug("Modal submit interaction handled",
				zap.String("custom_id", event.Data.CustomID),
				zap.Duration("duration", duration))
		}()

		// Validate session but return early if session creation failed or session expired
		s, ok := b.validateAndGetSession(event, event.User().ID)
		if !ok {
			return
		}

		// Handle submission and update session
		b.paginationManager.HandleInteraction(event, s)
		s.Touch(context.Background())
	}()
}

// validateAndGetSession retrieves or creates a session for the given user and validates its state.
func (b *Bot) validateAndGetSession(event interfaces.CommonEvent, userID snowflake.ID) (*session.Session, bool) {
	// Get or create user session
	s, err := b.sessionManager.GetOrCreateSession(context.Background(), uint64(userID))
	if err != nil {
		b.logger.Error("Failed to get or create session", zap.Error(err))
		if errors.Is(err, session.ErrSessionLimitReached) {
			b.paginationManager.RespondWithError(event, "Session limit reached. Please try again later.")
		} else {
			b.paginationManager.RespondWithError(event, "Failed to get or create session.")
		}
		return nil, false
	}

	// Check if the session has a valid current page
	currentPage := s.GetString(constants.SessionKeyCurrentPage)
	page := b.paginationManager.GetPage(currentPage)
	if page == nil {
		// If no valid page exists, reset to dashboard
		b.dashboardLayout.Show(event, s, "New session created.")
		s.Touch(context.Background())
		return s, false
	}

	return s, true
}
