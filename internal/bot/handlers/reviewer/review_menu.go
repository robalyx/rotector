package reviewer

import (
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/rotector/rotector/internal/bot/constants"
	"github.com/rotector/rotector/internal/bot/handlers/reviewer/builders"
	"github.com/rotector/rotector/internal/bot/interfaces"
	"github.com/rotector/rotector/internal/bot/pagination"
	"github.com/rotector/rotector/internal/bot/session"
	"github.com/rotector/rotector/internal/bot/utils"
	"github.com/rotector/rotector/internal/common/translator"
	"go.uber.org/zap"
)

// ReviewMenu handles the review process for flagged users.
type ReviewMenu struct {
	handler *Handler
	page    *pagination.Page
}

// NewReviewMenu creates a new ReviewMenu instance.
func NewReviewMenu(h *Handler) *ReviewMenu {
	translator := translator.New(h.roAPI.GetClient())

	m := ReviewMenu{handler: h}
	m.page = &pagination.Page{
		Name: "Review Menu",
		Message: func(s *session.Session) *discord.MessageUpdateBuilder {
			return builders.NewReviewEmbed(s, translator).Build()
		},
		SelectHandlerFunc: m.handleSelectMenu,
		ButtonHandlerFunc: m.handleButton,
		ModalHandlerFunc:  m.handleModal,
	}
	return &m
}

// ShowReviewMenuAndFetchUser displays the review menu and fetches a new user.
func (m *ReviewMenu) ShowReviewMenuAndFetchUser(event interfaces.CommonEvent, s *session.Session, content string) {
	// Fetch a new user
	sortBy := s.GetString(constants.KeySortBy)
	user, err := m.handler.db.Users().GetRandomPendingUser(sortBy)
	if err != nil {
		m.handler.logger.Error("Failed to fetch a new user", zap.Error(err))
		utils.RespondWithError(event, "Failed to fetch a new user. Please try again.")
		return
	}
	s.Set(constants.KeyTarget, user)

	// Display the review menu
	m.ShowReviewMenu(event, s, content)
}

// ShowReviewMenu displays the review menu.
func (m *ReviewMenu) ShowReviewMenu(event interfaces.CommonEvent, s *session.Session, content string) {
	user := s.GetPendingUser(constants.KeyTarget)

	// Check which friends are flagged
	friendIDs := make([]uint64, len(user.Friends))
	for i, friend := range user.Friends {
		friendIDs[i] = friend.ID
	}

	flaggedFriends, err := m.handler.db.Users().CheckExistingUsers(friendIDs)
	if err != nil {
		m.handler.logger.Error("Failed to check existing friends", zap.Error(err))
		return
	}

	// Get user settings
	settings, err := m.handler.db.Settings().GetUserSettings(uint64(event.User().ID))
	if err != nil {
		m.handler.logger.Error("Failed to get user settings", zap.Error(err))
		utils.RespondWithError(event, "Failed to get user settings. Please try again.")
		return
	}

	s.Set(constants.SessionKeyUser, user)
	s.Set(constants.SessionKeyFlaggedFriends, flaggedFriends)
	s.Set(constants.SessionKeyStreamerMode, settings.StreamerMode)

	m.handler.paginationManager.NavigateTo(m.page.Name, s)
	m.handler.paginationManager.UpdateMessage(event, s, m.page, content)
}

// handleSelectMenu handles the select menu for the review menu.
func (m *ReviewMenu) handleSelectMenu(event *events.ComponentInteractionCreate, s *session.Session, customID string, option string) {
	switch customID {
	case constants.SortOrderSelectMenuCustomID:
		s.Set(constants.KeySortBy, option)
		m.ShowReviewMenuAndFetchUser(event, s, "Changed sort order")
	case constants.ActionSelectMenuCustomID:
		switch option {
		case constants.BanWithReasonButtonCustomID:
			m.handleBanWithReason(event, s)
		case constants.OpenOutfitsMenuButtonCustomID:
			m.handler.outfitsMenu.ShowOutfitsMenu(event, s, 0)
		case constants.OpenFriendsMenuButtonCustomID:
			m.handler.friendsMenu.ShowFriendsMenu(event, s, 0)
		case constants.OpenGroupsMenuButtonCustomID:
			m.handler.groupsMenu.ShowGroupsMenu(event, s, 0)
		}
	}
}

// handleButton handles the buttons for the review menu.
func (m *ReviewMenu) handleButton(event *events.ComponentInteractionCreate, s *session.Session, customID string) {
	switch customID {
	case constants.BackButtonCustomID:
		m.handler.dashboardHandler.ShowDashboard(event)
	case constants.BanButtonCustomID:
		m.handleBanUser(event, s)
	case constants.ClearButtonCustomID:
		m.handleClearUser(event, s)
	case constants.SkipButtonCustomID:
		m.ShowReviewMenuAndFetchUser(event, s, "Skipped user.")
	}
}

// handleModal handles the modal for the review menu.
func (m *ReviewMenu) handleModal(event *events.ModalSubmitInteractionCreate, s *session.Session) {
	if event.Data.CustomID == constants.BanWithReasonModalCustomID {
		m.handleBanWithReasonModalSubmit(event, s)
	}
}

// handleBanUser handles the ban user button interaction.
func (m *ReviewMenu) handleBanUser(event interfaces.CommonEvent, s *session.Session) {
	user := s.GetPendingUser(constants.KeyTarget)

	// Perform the ban
	if err := m.handler.db.Users().BanUser(user); err != nil {
		m.handler.logger.Error("Failed to accept user", zap.Error(err))
		utils.RespondWithError(event, "Failed to accept the user. Please try again.")
		return
	}

	m.ShowReviewMenuAndFetchUser(event, s, "User banned.")
}

// handleClearUser handles the clear user button interaction.
func (m *ReviewMenu) handleClearUser(event interfaces.CommonEvent, s *session.Session) {
	user := s.GetPendingUser(constants.KeyTarget)

	// Clear the user
	if err := m.handler.db.Users().ClearUser(user); err != nil {
		m.handler.logger.Error("Failed to reject user", zap.Error(err))
		utils.RespondWithError(event, "Failed to reject the user. Please try again.")
		return
	}

	m.ShowReviewMenuAndFetchUser(event, s, "User cleared.")
}

// handleBanWithReason processes the ban with a modal for a custom reason.
func (m *ReviewMenu) handleBanWithReason(event *events.ComponentInteractionCreate, s *session.Session) {
	user := s.GetPendingUser(constants.KeyTarget)

	// Create the modal
	modal := discord.NewModalCreateBuilder().
		SetCustomID(constants.BanWithReasonModalCustomID).
		SetTitle("Ban User with Reason").
		AddActionRow(
			discord.NewTextInput("ban_reason", discord.TextInputStyleParagraph, "Ban Reason").
				WithRequired(true).
				WithPlaceholder("Enter the reason for banning this user...").
				WithValue(user.Reason), // Pre-fill with the original reason if available
		).
		Build()

	// Send the modal
	if err := event.Modal(modal); err != nil {
		m.handler.logger.Error("Failed to create modal", zap.Error(err))
		utils.RespondWithError(event, "Failed to open the ban reason form. Please try again.")
	}
}

// handleBanWithReasonModalSubmit processes the modal submit interaction.
func (m *ReviewMenu) handleBanWithReasonModalSubmit(event *events.ModalSubmitInteractionCreate, s *session.Session) {
	user := s.GetPendingUser(constants.KeyTarget)

	// Get the ban reason from the modal
	reason := event.Data.Text("ban_reason")
	if reason == "" {
		utils.RespondWithError(event, "Ban reason cannot be empty. Please try again.")
		return
	}

	// Update the user's reason with the custom input
	user.Reason = reason

	// Perform the ban
	if err := m.handler.db.Users().BanUser(user); err != nil {
		m.handler.logger.Error("Failed to accept user", zap.Error(err))
		utils.RespondWithError(event, "Failed to ban the user. Please try again.")
		return
	}

	// Show the review menu and fetch a new user
	m.ShowReviewMenuAndFetchUser(event, s, "User banned.")
}
