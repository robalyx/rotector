package reviewer

import (
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/rotector/rotector/internal/bot/handlers/reviewer/builders"
	"github.com/rotector/rotector/internal/bot/handlers/reviewer/constants"
	"github.com/rotector/rotector/internal/bot/interfaces"
	"github.com/rotector/rotector/internal/bot/pagination"
	"github.com/rotector/rotector/internal/bot/session"
	"github.com/rotector/rotector/internal/common/database"
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

	r := ReviewMenu{handler: h}
	r.page = &pagination.Page{
		Name: "Review Menu",
		Data: make(map[string]interface{}),
		Message: func(data map[string]interface{}) *discord.MessageUpdateBuilder {
			user := data["user"].(*database.PendingUser)
			sortBy := data["sortBy"].(string)
			flaggedFriends := data["flaggedFriends"].(map[uint64]string)

			return builders.NewReviewEmbed(user, translator, flaggedFriends, sortBy).Build()
		},
		SelectHandlerFunc: func(event *events.ComponentInteractionCreate, s *session.Session, customID string, option string) {
			switch customID {
			case constants.SortOrderSelectMenuCustomID:
				s.Set(session.KeySortBy, option)
				r.ShowReviewMenuAndFetchUser(event, s, "Changed sort order")
			case constants.ActionSelectMenuCustomID:
				switch option {
				case constants.BanWithReasonButtonCustomID:
					r.handleBanWithReason(event, s)
				case constants.OpenOutfitsMenuButtonCustomID:
					h.outfitsMenu.ShowOutfitsMenu(event, s, 0)
				case constants.OpenFriendsMenuButtonCustomID:
					h.friendsMenu.ShowFriendsMenu(event, s, 0)
				case constants.OpenGroupViewerButtonCustomID:
					// Implement group viewer logic here
				}
			}
		},
		ButtonHandlerFunc: func(event *events.ComponentInteractionCreate, s *session.Session, option string) {
			switch option {
			case constants.BackButtonCustomID:
				h.mainMenu.ShowMainMenu(event)
			case constants.BanButtonCustomID:
				r.handleBanUser(event, s)
			case constants.ClearButtonCustomID:
				r.handleClearUser(event, s)
			case constants.SkipButtonCustomID:
				r.ShowReviewMenuAndFetchUser(event, s, "Skipped user.")
			}
		},
		ModalHandlerFunc: func(event *events.ModalSubmitInteractionCreate, s *session.Session) {
			if event.Data.CustomID == constants.BanWithReasonModalCustomID {
				r.handleBanWithReasonModalSubmit(event, s)
			}
		},
	}

	return &r
}

// ShowReviewMenuAndFetchUser displays the review menu and fetches a new user.
func (r *ReviewMenu) ShowReviewMenuAndFetchUser(event interfaces.CommonEvent, s *session.Session, content string) {
	// Fetch a new user
	sortBy := s.GetString(session.KeySortBy)
	user, err := r.handler.db.Users().GetRandomPendingUser(sortBy)
	if err != nil {
		r.handler.logger.Error("Failed to fetch a new user", zap.Error(err))
		r.handler.respondWithError(event, "Failed to fetch a new user. Please try again.")
		return
	}
	s.Set(session.KeyTarget, user)

	// Display the review menu
	r.ShowReviewMenu(event, s, content)
}

func (r *ReviewMenu) ShowReviewMenu(event interfaces.CommonEvent, s *session.Session, content string) {
	user := s.GetPendingUser(session.KeyTarget)

	// Check which friends are flagged
	friendIDs := make([]uint64, len(user.Friends))
	for i, friend := range user.Friends {
		friendIDs[i] = friend.ID
	}

	flaggedFriends, err := r.handler.db.Users().CheckExistingUsers(friendIDs)
	if err != nil {
		r.handler.logger.Error("Failed to check existing friends", zap.Error(err))
		return
	}

	r.page.Data["user"] = user
	r.page.Data["sortBy"] = s.GetString(session.KeySortBy)
	r.page.Data["flaggedFriends"] = flaggedFriends

	r.handler.paginationManager.NavigateTo(r.page.Name, s)
	r.handler.paginationManager.UpdateMessage(event, s, r.page, content)
}

func (r *ReviewMenu) handleBanUser(event interfaces.CommonEvent, s *session.Session) {
	user := s.GetPendingUser(session.KeyTarget)

	// Perform the ban
	if err := r.handler.db.Users().AcceptUser(user); err != nil {
		r.handler.logger.Error("Failed to accept user", zap.Error(err))
		r.handler.respondWithError(event, "Failed to accept the user. Please try again.")
		return
	}

	r.ShowReviewMenuAndFetchUser(event, s, "User banned.")
}

func (r *ReviewMenu) handleClearUser(event interfaces.CommonEvent, s *session.Session) {
	user := s.GetPendingUser(session.KeyTarget)

	// Clear the user
	if err := r.handler.db.Users().RejectUser(user); err != nil {
		r.handler.logger.Error("Failed to reject user", zap.Error(err))
		r.handler.respondWithError(event, "Failed to reject the user. Please try again.")
		return
	}

	r.ShowReviewMenuAndFetchUser(event, s, "User cleared.")
}

// handleBanWithReason processes the ban with reason button interaction.
func (r *ReviewMenu) handleBanWithReason(event *events.ComponentInteractionCreate, s *session.Session) {
	user := s.GetPendingUser(session.KeyTarget)

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
		r.handler.logger.Error("Failed to create modal", zap.Error(err))
		r.handler.respondWithError(event, "Failed to open the ban reason form. Please try again.")
	}
}

// handleBanWithReasonModalSubmit processes the modal submit interaction.
func (r *ReviewMenu) handleBanWithReasonModalSubmit(event *events.ModalSubmitInteractionCreate, s *session.Session) {
	user := s.GetPendingUser(session.KeyTarget)

	// Get the ban reason from the modal
	reason := event.Data.Text("ban_reason")
	if reason == "" {
		r.handler.respondWithError(event, "Ban reason cannot be empty. Please try again.")
		return
	}

	// Update the user's reason with the custom input
	user.Reason = reason

	// Perform the ban
	if err := r.handler.db.Users().AcceptUser(user); err != nil {
		r.handler.logger.Error("Failed to accept user", zap.Error(err))
		r.handler.respondWithError(event, "Failed to ban the user. Please try again.")
		return
	}

	// Show the review menu and fetch a new user
	r.ShowReviewMenuAndFetchUser(event, s, "User banned.")
}
