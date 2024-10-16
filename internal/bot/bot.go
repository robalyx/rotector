package bot

import (
	"context"
	"fmt"

	"github.com/disgoorg/disgo"
	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/disgo/gateway"
	"go.uber.org/zap"

	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/rotector/rotector/internal/bot/handlers/reviewer"
	"github.com/rotector/rotector/internal/common/database"
	"github.com/rotector/rotector/internal/common/utils"
)

const (
	ReviewCommandName = "review"
)

// Bot represents the Discord bot.
type Bot struct {
	client          bot.Client
	reviewerHandler *reviewer.Handler
	logger          *zap.Logger
}

// New creates a new Bot instance.
func New(token string, db *database.Database, roAPI *api.API, logger *zap.Logger) (*Bot, error) {
	reviewerHandler := reviewer.New(db, logger, roAPI)

	b := &Bot{
		reviewerHandler: reviewerHandler,
		logger:          logger,
	}

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
		}),
	)
	if err != nil {
		return nil, err
	}

	b.client = client
	return b, nil
}

// registerCommands registers the bot's slash commands.
func (b *Bot) registerCommands() error {
	_, err := b.client.Rest().SetGlobalCommands(b.client.ApplicationID(), []discord.ApplicationCommandCreate{
		discord.SlashCommandCreate{
			Name:        "review",
			Description: "Review a flagged user account",
		},
	})
	return err
}

// Start initializes and starts the bot.
func (b *Bot) Start() error {
	b.logger.Info("Registering commands")
	if err := b.registerCommands(); err != nil {
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
	if event.Data.CommandName() != ReviewCommandName {
		return
	}

	if err := event.DeferCreateMessage(true); err != nil {
		b.logger.Error("Failed to defer create message", zap.Error(err))
		return
	}

	// Handle the interaction in a goroutine
	go func() {
		defer func() {
			if r := recover(); r != nil {
				b.logger.Error("Panic in application command interaction handler", zap.Any("panic", r))
			}
		}()
		b.reviewerHandler.ShowMainMenu(b.client, b.client.ApplicationID(), event.Token())
	}()
}

// handleComponentInteraction processes component interactions.
func (b *Bot) handleComponentInteraction(event *events.ComponentInteractionCreate) {
	b.logger.Debug("Component interaction", zap.String("customID", event.Data.CustomID()))

	// Create a new message update builder
	updateBuilder := discord.NewMessageUpdateBuilder().SetContent(utils.GetTimestampedMessage("Processing..."))

	// Grey out all buttons and remove other components in the message
	for _, component := range event.Message.Components {
		if actionRow, ok := component.(discord.ActionRowComponent); ok {
			var newComponents []discord.InteractiveComponent
			for _, c := range actionRow.Components() {
				if button, ok := c.(discord.ButtonComponent); ok {
					newButton := button.WithDisabled(true)
					newComponents = append(newComponents, newButton)
				}
			}
			if len(newComponents) > 0 {
				updateBuilder.AddActionRow(newComponents...)
			}
		}
	}

	// Update the message with greyed out buttons and removed components
	if err := event.UpdateMessage(updateBuilder.Build()); err != nil {
		b.logger.Error("Failed to update message", zap.Error(err))
	}

	// Handle the interaction in a goroutine
	go func() {
		defer func() {
			if r := recover(); r != nil {
				b.logger.Error("Panic in component interaction handler", zap.Any("panic", r))
			}
		}()
		b.reviewerHandler.HandleComponentInteraction(event)
	}()
}
