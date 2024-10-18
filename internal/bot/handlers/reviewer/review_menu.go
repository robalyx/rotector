package reviewer

import (
	"bytes"
	"strings"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/rotector/rotector/assets"
	"github.com/rotector/rotector/internal/bot/handlers/reviewer/builders"
	"github.com/rotector/rotector/internal/bot/interfaces"
	"github.com/rotector/rotector/internal/bot/session"
	"github.com/rotector/rotector/internal/common/database"
	"github.com/rotector/rotector/internal/common/translator"
	"go.uber.org/zap"
)

const (
	ReviewProcessPrefix = "review_process:"

	SortSelectMenuCustomID   = "sort_select_menu"
	ActionSelectMenuCustomID = "action_select_menu"

	BanWithReasonButtonCustomID   = "ban_with_reason_modal"
	BanWithReasonModalCustomID    = "ban_with_reason_modal"
	OpenGroupViewerButtonCustomID = "open_group_viewer"

	BackButtonCustomID  = "back"
	BanButtonCustomID   = "ban"
	ClearButtonCustomID = "clear"
	SkipButtonCustomID  = "skip"
)

// ReviewMenu handles the review process for flagged users.
type ReviewMenu struct {
	handler    *Handler
	translator *translator.Translator
}

// NewReviewMenu creates a new ReviewMenu instance.
func NewReviewMenu(h *Handler) *ReviewMenu {
	return &ReviewMenu{
		handler:    h,
		translator: translator.New(h.roAPI.GetClient()),
	}
}

// ShowReviewMenu displays the review menu.
func (r *ReviewMenu) ShowReviewMenu(event interfaces.CommonEvent, s *session.Session, message string) {
	// Get the user from the session
	user := s.GetPendingUser(session.KeyTarget)
	if user == nil {
		r.handler.respondWithError(event, "Bot lost track of the user. Please try again.")
		return
	}

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

	// Create the embed and components
	embed := builders.NewReviewEmbed(user, r.translator, flaggedFriends).Build()
	components := []discord.ContainerComponent{
		discord.NewActionRow(
			discord.NewStringSelectMenu(ReviewProcessPrefix+SortSelectMenuCustomID, "Sorting",
				discord.NewStringSelectMenuOption("Selected by random", database.SortByRandom).
					WithDefault(s.GetString(session.KeySortBy) == database.SortByRandom).
					WithEmoji(discord.ComponentEmoji{Name: "🔀"}),
				discord.NewStringSelectMenuOption("Selected by confidence", database.SortByConfidence).
					WithDefault(s.GetString(session.KeySortBy) == database.SortByConfidence).
					WithEmoji(discord.ComponentEmoji{Name: "🔮"}),
				discord.NewStringSelectMenuOption("Selected by last updated time", database.SortByLastUpdated).
					WithDefault(s.GetString(session.KeySortBy) == database.SortByLastUpdated).
					WithEmoji(discord.ComponentEmoji{Name: "📅"}),
			),
		),
		discord.NewActionRow(
			discord.NewStringSelectMenu(ReviewProcessPrefix+ActionSelectMenuCustomID, "Actions",
				discord.NewStringSelectMenuOption("Ban with reason", BanWithReasonButtonCustomID),
				discord.NewStringSelectMenuOption("Open outfit viewer", OpenOutfitsMenuButtonCustomID),
				discord.NewStringSelectMenuOption("Open friends viewer", OpenFriendsMenuButtonCustomID),
				discord.NewStringSelectMenuOption("Open group viewer", OpenGroupViewerButtonCustomID),
			),
		),
		discord.NewActionRow(
			discord.NewSecondaryButton("◀️", ReviewProcessPrefix+BackButtonCustomID),
			discord.NewDangerButton("Ban", ReviewProcessPrefix+BanButtonCustomID),
			discord.NewSuccessButton("Clear", ReviewProcessPrefix+ClearButtonCustomID),
			discord.NewSecondaryButton("Skip", ReviewProcessPrefix+SkipButtonCustomID),
		),
	}

	// Create response builder
	responseBuilder := builders.NewResponse().
		SetContent(message).
		SetEmbeds(embed).
		SetComponents(components...)

	// Add placeholder image if thumbnail URL is empty
	if user.ThumbnailURL == "" {
		placeholderImage, err := assets.Images.ReadFile("images/content_deleted.png")
		if err != nil {
			r.handler.logger.Error("Failed to read placeholder image", zap.Error(err))
		} else {
			responseBuilder.AddFile(discord.NewFile("content_deleted.png", "", bytes.NewReader(placeholderImage)))
		}
	}

	// Send the response
	r.handler.respond(event, responseBuilder)
}

// ShowReviewMenuAndFetchUser displays the review menu and fetches a new user.
func (r *ReviewMenu) ShowReviewMenuAndFetchUser(event interfaces.CommonEvent, s *session.Session, message string) {
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
		r.handler.ShowMainMenu(event)
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
		switch value {
		case BanWithReasonButtonCustomID:
			r.handleBanWithReason(event, s)
		case OpenOutfitsMenuButtonCustomID:
			r.handler.outfitsMenu.ShowOutfitsMenu(event, s, 0)
		case OpenFriendsMenuButtonCustomID:
			r.handler.friendsMenu.ShowFriendsMenu(event, s, 0)
		case OpenGroupViewerButtonCustomID:
			// Implement group viewer logic here
		}
	}
}

// handleBanWithReason processes the ban with reason button interaction.
func (r *ReviewMenu) handleBanWithReason(event *events.ComponentInteractionCreate, s *session.Session) {
	// Get the user from the session
	user := s.GetPendingUser(session.KeyTarget)
	if user == nil {
		r.handler.respondWithError(event, "Bot lost track of the user. Please try again.")
		return
	}

	// Create the modal
	modal := discord.NewModalCreateBuilder().
		SetCustomID(ReviewProcessPrefix + BanWithReasonModalCustomID).
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
	// Get the user from the session
	user := s.GetPendingUser(session.KeyTarget)
	if user == nil {
		r.handler.respondWithError(event, "Bot lost track of the user. Please try again.")
		return
	}

	// Get the ban reason from the modal
	reason := event.Data.Text("ban_reason")
	if reason == "" {
		r.handler.respondWithError(event, "Ban reason cannot be empty. Please try again.")
		return
	}

	// Update the user's reason
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
