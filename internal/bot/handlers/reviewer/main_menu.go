package reviewer

import (
	"strconv"
	"strings"

	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/snowflake/v2"
	"github.com/rotector/rotector/internal/bot/session"
	"github.com/rotector/rotector/internal/common/database"
	"go.uber.org/zap"
)

const (
	MainMenuPrefix           = "main_menu:"
	ReviewSelectMenuCustomID = "review_select"
)

// MainMenu handles the main menu functionality.
type MainMenu struct {
	handler *Handler
}

// NewMainMenu creates a new MainMenu instance.
func NewMainMenu(h *Handler) *MainMenu {
	return &MainMenu{handler: h}
}

// ShowMainMenu displays the main menu.
func (m *MainMenu) ShowMainMenu(client bot.Client, applicationID snowflake.ID, token string) {
	// Fetch pending and flagged user counts
	pendingCount, err := m.handler.db.Users().GetPendingUsersCount()
	if err != nil {
		m.handler.logger.Error("Failed to get pending user counts", zap.Error(err))
		return
	}

	flaggedCount, err := m.handler.db.Users().GetFlaggedUsersCount()
	if err != nil {
		m.handler.logger.Error("Failed to get flagged user counts", zap.Error(err))
		return
	}

	// Create necessary embed and components
	embed := discord.NewEmbedBuilder().
		AddField("Pending Users", strconv.Itoa(pendingCount), true).
		AddField("Flagged Users", strconv.Itoa(flaggedCount), true).
		SetColor(0x312D2B).
		Build()
	components := []discord.ContainerComponent{
		discord.NewActionRow(
			discord.NewStringSelectMenu(MainMenuPrefix+ReviewSelectMenuCustomID, "Select an action",
				discord.NewStringSelectMenuOption("Start reviewing flagged players", database.SortByRandom),
			),
		),
	}

	// Update the interaction response with the main menu
	_, err = client.Rest().UpdateInteractionResponse(applicationID, token, discord.NewMessageUpdateBuilder().
		SetContent("").
		AddEmbeds(embed).
		SetContainerComponents(components...).
		RetainAttachments().
		Build())
	if err != nil {
		m.handler.logger.Error("Failed to update message with review", zap.Error(err))
	}
}

// HandleMainMenu processes the main menu dropdown selection.
func (m *MainMenu) HandleMainMenu(event *events.ComponentInteractionCreate, s *session.Session) {
	// Parse the custom ID
	parts := strings.Split(event.Data.CustomID(), ":")
	if len(parts) != 2 {
		m.handler.logger.Warn("Invalid custom ID format", zap.String("customID", event.Data.CustomID()))
		m.handler.respondWithError(event, "Invalid button interaction.")
		return
	}

	// Determine the action based on the custom ID
	action := parts[1]
	if action == ReviewSelectMenuCustomID {
		// Start the review process
		s.Set(session.KeySortBy, database.SortByRandom)
		m.handler.reviewMenu.ShowReviewMenuAndFetchUser(event, s, "")
	}
}
