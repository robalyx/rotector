package appeal

import (
	"context"
	"strings"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/jaxron/axonet/middleware/redis"
	builder "github.com/robalyx/rotector/internal/bot/builder/appeal"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/pagination"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/bot/interfaces"
	"github.com/robalyx/rotector/internal/bot/utils"
	"github.com/robalyx/rotector/internal/common/storage/database/types"
	"github.com/robalyx/rotector/internal/common/storage/database/types/enum"
	"go.uber.org/zap"
)

// VerifyMenu handles the verification process for appeal submissions.
type VerifyMenu struct {
	layout *Layout
	page   *pagination.Page
}

// NewVerifyMenu creates a new verification menu.
func NewVerifyMenu(layout *Layout) *VerifyMenu {
	m := &VerifyMenu{layout: layout}
	m.page = &pagination.Page{
		Name: "Appeal Verification",
		Message: func(s *session.Session) *discord.MessageUpdateBuilder {
			return m.buildMessage(s)
		},
		ButtonHandlerFunc: m.handleButton,
	}
	return m
}

// Show displays the verification interface.
func (m *VerifyMenu) Show(event interfaces.CommonEvent, s *session.Session, userID uint64, reason string) {
	// Generate verification code
	verificationCode := utils.GenerateRandomWords(4)

	// Store data in session
	s.Set(constants.SessionKeyVerifyUserID, userID)
	s.Set(constants.SessionKeyVerifyReason, reason)
	s.Set(constants.SessionKeyVerifyCode, verificationCode)

	m.layout.paginationManager.NavigateTo(event, s, m.page, "")
}

// buildMessage creates the verification message with instructions.
func (m *VerifyMenu) buildMessage(s *session.Session) *discord.MessageUpdateBuilder {
	return builder.NewVerifyBuilder(s).Build()
}

// handleButton processes button interactions.
func (m *VerifyMenu) handleButton(event *events.ComponentInteractionCreate, s *session.Session, customID string) {
	switch customID {
	case constants.BackButtonCustomID:
		m.layout.paginationManager.NavigateBack(event, s, "Verification cancelled.")
	case constants.VerifyDescriptionButtonID:
		m.verifyDescription(event, s)
	}
}

// verifyDescription checks if the user has updated their description with the verification code.
func (m *VerifyMenu) verifyDescription(event *events.ComponentInteractionCreate, s *session.Session) {
	userID := s.GetUint64(constants.SessionKeyVerifyUserID)
	expectedCode := s.GetString(constants.SessionKeyVerifyCode)
	reason := s.GetString(constants.SessionKeyVerifyReason)

	// Fetch user profile
	ctx := context.Background()
	ctx = context.WithValue(ctx, redis.SkipCacheKey{}, true)

	userInfo, err := m.layout.roAPI.Users().GetUserByID(ctx, userID)
	if err != nil {
		m.layout.logger.Error("Failed to fetch user info",
			zap.Error(err),
			zap.Uint64("userID", userID))
		m.layout.paginationManager.RespondWithError(event, "Failed to verify description. Please try again.")
		return
	}

	// Check if description contains verification code
	if !strings.Contains(userInfo.Description, expectedCode) {
		m.layout.paginationManager.NavigateTo(event, s, m.page,
			"❌ Verification code not found in description. Please make sure you copied it exactly.")
		return
	}

	// Create appeal
	appeal := &types.Appeal{
		UserID:      userID,
		RequesterID: uint64(event.User().ID),
		Status:      enum.AppealStatusPending,
	}

	// Submit appeal
	if err := m.layout.db.Appeals().CreateAppeal(context.Background(), appeal, reason); err != nil {
		m.layout.logger.Error("Failed to create appeal", zap.Error(err))
		m.layout.paginationManager.RespondWithError(event, "Failed to submit appeal. Please try again.")
		return
	}

	s.Delete(constants.SessionKeyAppealCursor)
	s.Delete(constants.SessionKeyAppealPrevCursors)
	m.layout.ShowOverview(event, s, "✅ Account verified and appeal submitted successfully!")

	// Log the appeal submission
	m.layout.db.Activity().Log(context.Background(), &types.ActivityLog{
		ActivityTarget: types.ActivityTarget{
			UserID: userID,
		},
		ReviewerID:        uint64(event.User().ID),
		ActivityType:      enum.ActivityTypeAppealSubmitted,
		ActivityTimestamp: time.Now(),
		Details: map[string]interface{}{
			"reason": reason,
		},
	})
}
