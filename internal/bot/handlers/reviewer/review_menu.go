package reviewer

import (
	"strings"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/rotector/rotector/internal/bot/session"
	"go.uber.org/zap"
)

const (
	ReviewProcessPrefix = "review_process:"

	SortSelectMenuCustomID   = "sort_select_menu"
	ActionSelectMenuCustomID = "action_select_menu"

	BanWithReasonButtonCustomID   = "ban_with_reason"
	OpenGroupViewerButtonCustomID = "open_group_viewer"

	BackButtonCustomID  = "back"
	BanButtonCustomID   = "ban"
	ClearButtonCustomID = "clear"
	SkipButtonCustomID  = "skip"
)

// ReviewMenu handles the review process for flagged users.
type ReviewMenu struct {
	handler *Handler
}

// NewReviewMenu creates a new ReviewMenu instance.
func NewReviewMenu(h *Handler) *ReviewMenu {
	return &ReviewMenu{handler: h}
}

// ShowReviewMenu displays the review menu.
func (r *ReviewMenu) ShowReviewMenu(event *events.ComponentInteractionCreate, s *session.Session, message string) {
	// Get the user from the session
	user := s.GetPendingUser(session.KeyTarget)
	if user == nil {
		r.handler.respondWithError(event, "Bot lost track of the user. Please try again.")
		return
	}

	// Create the embed and components
	embed := NewReviewEmbedBuilder(user).Build()
	components := NewComponentBuilder().
		AddSortSelectMenu(s.GetString(session.KeySortBy)).
		AddActionSelectMenu().
		AddReviewButtons().
		Build()

	// Send the response
	r.handler.respond(event, NewResponseBuilder().
		SetContent(message).
		SetEmbeds(embed).
		SetComponents(components...))
}

// ShowReviewMenuAndFetchUser displays the review menu and fetches a new user.
func (r *ReviewMenu) ShowReviewMenuAndFetchUser(event *events.ComponentInteractionCreate, s *session.Session, message string) {
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
	r.ShowReviewMenu(event, s, message)
}

// HandleReviewMenu processes review-related interactions.
func (r *ReviewMenu) HandleReviewMenu(event *events.ComponentInteractionCreate, s *session.Session) {
	buttonData, ok := event.Data.(discord.ButtonInteractionData)
	if ok {
		r.handleReviewButtonInteraction(event, s, &buttonData)
	}

	stringSelectData, ok := event.Data.(discord.StringSelectMenuInteractionData)
	if ok {
		r.handleReviewSelectMenuInteraction(event, s, &stringSelectData)
	}
}

// handleReviewButtonInteraction processes review-related button interactions.
func (r *ReviewMenu) handleReviewButtonInteraction(event *events.ComponentInteractionCreate, s *session.Session, data *discord.ButtonInteractionData) {
	// Parse the custom ID
	parts := strings.Split(data.CustomID(), ":")
	if len(parts) < 2 {
		r.handler.logger.Warn("Invalid custom ID format", zap.String("customID", data.CustomID()))
		r.handler.respondWithError(event, "Invalid button interaction.")
		return
	}

	// Get the user from the session
	user := s.GetPendingUser(session.KeyTarget)
	if user == nil {
		r.handler.respondWithError(event, "Bot lost track of the user. Please try again.")
		return
	}

	// Determine the action based on the custom ID
	action := parts[1]
	switch action {
	case BackButtonCustomID:
		r.handler.mainMenu.ShowMainMenu(event.Client(), event.ApplicationID(), event.Token())
	case BanButtonCustomID:
		// Accept the user
		if err := r.handler.db.Users().AcceptUser(user); err != nil {
			r.handler.logger.Error("Failed to accept user", zap.Error(err))
			r.handler.respondWithError(event, "Failed to accept the user. Please try again.")
			return
		}

		r.ShowReviewMenuAndFetchUser(event, s, "User banned.")
	case ClearButtonCustomID:
		// Reject the user
		if err := r.handler.db.Users().RejectUser(user); err != nil {
			r.handler.logger.Error("Failed to reject user", zap.Error(err))
			r.handler.respondWithError(event, "Failed to reject the user. Please try again.")
			return
		}

		r.ShowReviewMenuAndFetchUser(event, s, "User cleared.")
	case SkipButtonCustomID:
		// Skip the user
		r.ShowReviewMenuAndFetchUser(event, s, "Skipped user.")
	default:
		r.handler.logger.Warn("Invalid button interaction", zap.String("action", action))
		r.handler.respondWithError(event, "Invalid button interaction.")
	}
}

// handleReviewSelectMenuInteraction processes the review select menu interaction.
func (r *ReviewMenu) handleReviewSelectMenuInteraction(event *events.ComponentInteractionCreate, s *session.Session, data *discord.StringSelectMenuInteractionData) {
	// Parse the data's values
	if len(data.Values) != 1 {
		r.handler.logger.Error("Invalid number of values for action menu", zap.Int("valuesCount", len(data.Values)))
		return
	}

	// Parse the custom ID
	parts := strings.Split(data.CustomID(), ":")
	if len(parts) < 2 {
		r.handler.logger.Warn("Invalid custom ID format", zap.String("customID", data.CustomID()))
		r.handler.respondWithError(event, "Invalid select menu interaction.")
		return
	}

	value := data.Values[0]
	action := parts[1]

	// Determine the action based on the custom ID
	switch action {
	case SortSelectMenuCustomID:
		// Set the sort by value in the session
		s.Set(session.KeySortBy, value)
		r.ShowReviewMenuAndFetchUser(event, s, "Changed sort order.")
	case ActionSelectMenuCustomID:
		// Determine the button that was pressed
		if value == OpenOutfitsMenuButtonCustomID {
			r.handler.outfitsMenu.ShowOutfitsMenu(event, s, 0)
		} else if value == OpenFriendsMenuButtonCustomID {
			r.handler.friendsMenu.ShowFriendsMenu(event, s, 0)
		}
	}
}
