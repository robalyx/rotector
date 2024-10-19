package reviewer

import (
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/rotector/rotector/internal/bot/interfaces"
	"github.com/rotector/rotector/internal/bot/pagination"
	"github.com/rotector/rotector/internal/bot/session"
	"github.com/rotector/rotector/internal/common/database"
	"github.com/rotector/rotector/internal/common/utils"
	"go.uber.org/zap"
)

// Handler manages the review process for flagged users.
type Handler struct {
	db                *database.Database
	roAPI             *api.API
	sessionManager    *session.Manager
	logger            *zap.Logger
	reviewMenu        *ReviewMenu
	outfitsMenu       *OutfitsMenu
	friendsMenu       *FriendsMenu
	mainMenu          *MainMenu
	paginationManager *pagination.Manager
	groupsMenu        *GroupsMenu
}

// New creates a new Handler instance.
func New(db *database.Database, logger *zap.Logger, roAPI *api.API) *Handler {
	paginationManager := pagination.NewManager(logger)
	h := &Handler{
		db:                db,
		roAPI:             roAPI,
		sessionManager:    session.NewManager(db),
		logger:            logger,
		paginationManager: paginationManager,
	}

	// Add necessary menus
	h.reviewMenu = NewReviewMenu(h)
	h.outfitsMenu = NewOutfitsMenu(h)
	h.friendsMenu = NewFriendsMenu(h)
	h.groupsMenu = NewGroupsMenu(h)
	h.mainMenu = NewMainMenu(h)

	// Add pages to the pagination manager
	paginationManager.AddPage(h.mainMenu.page)
	paginationManager.AddPage(h.reviewMenu.page)
	paginationManager.AddPage(h.outfitsMenu.page)
	paginationManager.AddPage(h.friendsMenu.page)
	paginationManager.AddPage(h.groupsMenu.page)

	return h
}

// HandleApplicationCommandInteraction handles application command interactions.
func (h *Handler) HandleApplicationCommandInteraction(event *events.ApplicationCommandInteractionCreate) {
	h.mainMenu.ShowMainMenu(event)
}

// HandleComponentInteraction processes component interactions.
func (h *Handler) HandleComponentInteraction(event *events.ComponentInteractionCreate) {
	s := h.sessionManager.GetOrCreateSession(event.User().ID)

	// Ensure the interaction is for the latest message
	if event.Message.ID.String() != s.GetString(session.KeyMessageID) {
		h.respondWithError(event, "This interaction is outdated. Please use the latest interaction.")
		return
	}

	h.paginationManager.HandleInteraction(event, s)
}

// HandleModalSubmit handles modal submit interactions.
func (h *Handler) HandleModalSubmit(event *events.ModalSubmitInteractionCreate) {
	s := h.sessionManager.GetOrCreateSession(event.User().ID)
	h.paginationManager.HandleInteraction(event, s)
}

// Update the respondWithError method.
func (h *Handler) respondWithError(event interfaces.CommonEvent, message string) {
	messageUpdate := discord.NewMessageUpdateBuilder().
		SetContent(utils.GetTimestampedSubtext("Fatal error: " + message)).
		ClearEmbeds().
		ClearFiles().
		ClearContainerComponents().
		Build()

	_, err := event.Client().Rest().UpdateInteractionResponse(event.ApplicationID(), event.Token(), messageUpdate)
	if err != nil {
		h.logger.Error("Failed to send response", zap.Error(err))
	}
}
