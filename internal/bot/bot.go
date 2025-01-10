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

	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/pagination"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/bot/interfaces"
	"github.com/robalyx/rotector/internal/bot/menu/admin"
	"github.com/robalyx/rotector/internal/bot/menu/appeal"
	"github.com/robalyx/rotector/internal/bot/menu/ban"
	"github.com/robalyx/rotector/internal/bot/menu/captcha"
	"github.com/robalyx/rotector/internal/bot/menu/chat"
	"github.com/robalyx/rotector/internal/bot/menu/dashboard"
	"github.com/robalyx/rotector/internal/bot/menu/log"
	"github.com/robalyx/rotector/internal/bot/menu/queue"
	groupReview "github.com/robalyx/rotector/internal/bot/menu/review/group"
	userReview "github.com/robalyx/rotector/internal/bot/menu/review/user"
	"github.com/robalyx/rotector/internal/bot/menu/setting"
	"github.com/robalyx/rotector/internal/bot/utils"
	"github.com/robalyx/rotector/internal/common/setup"
	"github.com/robalyx/rotector/internal/common/storage/database"
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
	banLayout         interfaces.BanLayout
}

// New initializes a Bot instance by creating all required managers and layouts.
// It connects layouts through dependency injection and configures the Discord
// client with necessary gateway intents and event listeners.
func New(app *setup.App) (*Bot, error) {
	// Initialize session manager for persistent storage
	sessionManager, err := session.NewManager(app.DB, app.RedisManager, app.Logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create session manager: %w", err)
	}
	paginationManager := pagination.NewManager(sessionManager, app.Logger)

	// Create layouts with their dependencies
	settingLayout := setting.New(app, sessionManager, paginationManager)
	logLayout := log.New(app, sessionManager, paginationManager)
	chatLayout := chat.New(app, sessionManager, paginationManager)
	captchaLayout := captcha.New(app, sessionManager, paginationManager)
	userReviewLayout := userReview.New(app, sessionManager, paginationManager, settingLayout, logLayout, chatLayout, captchaLayout)
	groupReviewLayout := groupReview.New(app, sessionManager, paginationManager, settingLayout, logLayout, chatLayout, captchaLayout)
	queueLayout := queue.New(app, sessionManager, paginationManager, userReviewLayout)
	appealLayout := appeal.New(app, sessionManager, paginationManager, userReviewLayout)
	adminLayout := admin.New(app, sessionManager, paginationManager, settingLayout)
	dashboardLayout := dashboard.New(
		app,
		sessionManager,
		paginationManager,
		userReviewLayout,
		groupReviewLayout,
		settingLayout,
		logLayout,
		queueLayout,
		chatLayout,
		appealLayout,
		adminLayout,
	)
	banLayout := ban.New(app, sessionManager, paginationManager, dashboardLayout)

	// Initialize bot structure with all components
	b := &Bot{
		db:                app.DB,
		logger:            app.Logger,
		sessionManager:    sessionManager,
		paginationManager: paginationManager,
		dashboardLayout:   dashboardLayout,
		banLayout:         banLayout,
	}

	// Configure Discord client with required gateway intents and event handlers
	client, err := disgo.New(app.Config.Bot.Discord.Token,
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
		s, ok := b.validateAndGetSession(event, event.User().ID)
		if !ok {
			return
		}

		// Check if user is banned
		if b.checkBanStatus(event, s, event.User().ID, false) {
			return
		}

		// Check if the session has a valid current page
		page := b.paginationManager.GetPage(s.GetString(constants.SessionKeyCurrentPage))
		if page == nil {
			// If no valid page exists, reset to dashboard
			b.dashboardLayout.Show(event, s, "New session created.")
			s.Touch(context.Background())
			return
		}

		// Navigate to stored page
		b.paginationManager.NavigateTo(event, s, page, "")
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
		s, ok := b.validateAndGetSession(event, event.User().ID)
		if !ok {
			return
		}

		// Get current page
		page := b.paginationManager.GetPage(s.GetString(constants.SessionKeyCurrentPage))

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
		if !isModal || page == nil {
			updateBuilder := discord.NewMessageUpdateBuilder().
				SetContent(utils.GetTimestampedSubtext("Processing...")).
				ClearContainerComponents()

			if err := event.UpdateMessage(updateBuilder.Build()); err != nil {
				b.logger.Error("Failed to update message", zap.Error(err))
				return
			}
		}

		// Check if user is banned
		if b.checkBanStatus(event, s, event.User().ID, true) {
			return
		}

		// Check if the session has a valid current page
		if page == nil {
			// If no valid page exists, reset to dashboard
			b.dashboardLayout.Show(event, s, "New session created.")
			s.Touch(context.Background())
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
		s, ok := b.validateAndGetSession(event, event.User().ID)
		if !ok {
			return
		}

		// Check if user is banned
		if b.checkBanStatus(event, s, event.User().ID, true) {
			return
		}

		// Check if the session has a valid current page
		page := b.paginationManager.GetPage(s.GetString(constants.SessionKeyCurrentPage))
		if page == nil {
			// If no valid page exists, reset to dashboard
			b.dashboardLayout.Show(event, s, "New session created.")
			s.Touch(context.Background())
			return
		}

		// Handle submission and update session
		b.paginationManager.HandleInteraction(event, s)
		s.Touch(context.Background())
	}()
}

// checkBanStatus checks if a user is banned and shows the ban menu if they are.
// Returns true if the user is banned and should not proceed.
func (b *Bot) checkBanStatus(event interfaces.CommonEvent, s *session.Session, userID snowflake.ID, closeSession bool) bool {
	// Check if user is banned
	banned, err := b.db.Bans().IsBanned(context.Background(), uint64(userID))
	if err != nil {
		b.logger.Error("Failed to check ban status",
			zap.Error(err),
			zap.Uint64("user_id", uint64(userID)))
		b.paginationManager.RespondWithError(event, "Failed to verify access status. Please try again later.")
		return true
	}

	// If not banned, allow access
	if !banned {
		return false
	}

	// User is banned, show ban menu
	b.banLayout.Show(event, s)

	// Delete session after
	if closeSession {
		b.sessionManager.CloseSession(context.Background(), s.UserID())
	}
	return true
}

// validateAndGetSession retrieves or creates a session for the given user and validates its state.
func (b *Bot) validateAndGetSession(event interfaces.CommonEvent, userID snowflake.ID) (*session.Session, bool) {
	// Get or create user session
	s, err := b.sessionManager.GetOrCreateSession(context.Background(), userID)
	if err != nil {
		if errors.Is(err, session.ErrSessionLimitReached) {
			b.paginationManager.RespondWithError(event, "Session limit reached. Please try again later.")
		} else {
			b.logger.Error("Failed to get or create session", zap.Error(err))
			b.paginationManager.RespondWithError(event, "Failed to get or create session.")
		}
		return nil, false
	}

	return s, true
}
