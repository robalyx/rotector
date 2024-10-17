package reviewer

import (
	"strings"

	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/snowflake/v2"
	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/rotector/rotector/internal/bot/handlers/reviewer/builders"
	"github.com/rotector/rotector/internal/bot/session"
	"github.com/rotector/rotector/internal/common/database"
	"go.uber.org/zap"
)

// Handler manages the review process for flagged users.
type Handler struct {
	db             *database.Database
	roAPI          *api.API
	sessionManager *session.Manager
	logger         *zap.Logger
	reviewMenu     *ReviewMenu
	outfitsMenu    *OutfitsMenu
	friendsMenu    *FriendsMenu
	mainMenu       *MainMenu
}

// New creates a new Handler instance.
func New(db *database.Database, logger *zap.Logger, roAPI *api.API) *Handler {
	h := &Handler{
		db:             db,
		roAPI:          roAPI,
		sessionManager: session.NewManager(db),
		logger:         logger,
	}
	h.reviewMenu = NewReviewMenu(h)
	h.outfitsMenu = NewOutfitsMenu(h)
	h.friendsMenu = NewFriendsMenu(h)
	h.mainMenu = NewMainMenu(h)
	return h
}

// ShowMainMenu displays the main menu.
func (h *Handler) ShowMainMenu(client bot.Client, applicationID snowflake.ID, token string) {
	h.mainMenu.ShowMainMenu(client, applicationID, token)
}

// HandleComponentInteraction processes component interactions.
func (h *Handler) HandleComponentInteraction(event *events.ComponentInteractionCreate) {
	session := h.sessionManager.GetOrCreateSession(event.User().ID)

	switch {
	case strings.HasPrefix(event.Data.CustomID(), ReviewProcessPrefix):
		h.reviewMenu.HandleReviewMenu(event, session)
	case strings.HasPrefix(event.Data.CustomID(), OutfitsMenuPrefix):
		h.outfitsMenu.HandleOutfitsMenu(event, session)
	case strings.HasPrefix(event.Data.CustomID(), FriendsMenuPrefix):
		h.friendsMenu.HandleFriendsMenu(event, session)
	case strings.HasPrefix(event.Data.CustomID(), MainMenuPrefix):
		h.mainMenu.HandleMainMenu(event, session)
	}
}

// Update the respond method.
func (h *Handler) respond(event *events.ComponentInteractionCreate, builder *builders.Response) {
	_, err := event.Client().Rest().UpdateInteractionResponse(event.ApplicationID(), event.Token(), builder.Build())
	if err != nil {
		h.logger.Error("Failed to send response", zap.Error(err))
	}
}

// Update the respondWithError method.
func (h *Handler) respondWithError(event *events.ComponentInteractionCreate, message string) {
	builder := builders.NewResponse().
		SetContent("Fatal error: " + message).
		SetEphemeral(true)
	h.respond(event, builder)
}
