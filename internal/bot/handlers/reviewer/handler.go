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
	paginationManager *pagination.Manager
	groupsMenu        *GroupsMenu
	dashboardHandler  interfaces.DashboardHandler
}

// New creates a new Handler instance.
func New(db *database.Database, logger *zap.Logger, roAPI *api.API, sessionManager *session.Manager, paginationManager *pagination.Manager, dashboardHandler interfaces.DashboardHandler) *Handler {
	h := &Handler{
		db:                db,
		roAPI:             roAPI,
		sessionManager:    sessionManager,
		logger:            logger,
		paginationManager: paginationManager,
		dashboardHandler:  dashboardHandler,
	}

	// Add necessary menus
	h.reviewMenu = NewReviewMenu(h)
	h.outfitsMenu = NewOutfitsMenu(h)
	h.friendsMenu = NewFriendsMenu(h)
	h.groupsMenu = NewGroupsMenu(h)

	// Add pages to the pagination manager
	paginationManager.AddPage(h.reviewMenu.page)
	paginationManager.AddPage(h.outfitsMenu.page)
	paginationManager.AddPage(h.friendsMenu.page)
	paginationManager.AddPage(h.groupsMenu.page)

	return h
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

// ShowReviewMenuAndFetchUser displays the review menu and fetches a new user.
func (h *Handler) ShowReviewMenuAndFetchUser(event interfaces.CommonEvent, s *session.Session, content string) {
	h.reviewMenu.ShowReviewMenuAndFetchUser(event, s, content)
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
