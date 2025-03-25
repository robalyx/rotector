package appeal

import (
	"strings"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/jaxron/axonet/middleware/redis"
	builder "github.com/robalyx/rotector/internal/bot/builder/appeal"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/interaction"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/bot/utils"
	"github.com/robalyx/rotector/internal/common/storage/database/types"
	"github.com/robalyx/rotector/internal/common/storage/database/types/enum"
	"go.uber.org/zap"
)

// VerifyMenu handles the verification process for appeal submissions.
type VerifyMenu struct {
	layout *Layout
	page   *interaction.Page
}

// NewVerifyMenu creates a new verification menu.
func NewVerifyMenu(layout *Layout) *VerifyMenu {
	m := &VerifyMenu{layout: layout}
	m.page = &interaction.Page{
		Name: constants.AppealVerifyPageName,
		Message: func(s *session.Session) *discord.MessageUpdateBuilder {
			return builder.NewVerifyBuilder(s).Build()
		},
		ShowHandlerFunc:   m.Show,
		ButtonHandlerFunc: m.handleButton,
	}
	return m
}

// Show displays the verification interface.
func (m *VerifyMenu) Show(_ *interaction.Context, s *session.Session) {
	verificationCode := utils.GenerateRandomWords(4)
	session.VerifyCode.Set(s, verificationCode)
}

// handleButton processes button interactions.
func (m *VerifyMenu) handleButton(ctx *interaction.Context, s *session.Session, customID string) {
	switch customID {
	case constants.BackButtonCustomID:
		ctx.NavigateBack("Verification cancelled.")
	case constants.VerifyDescriptionButtonID:
		m.verifyDescription(ctx, s)
	}
}

// verifyDescription checks if the user has updated their description with the verification code.
func (m *VerifyMenu) verifyDescription(ctx *interaction.Context, s *session.Session) {
	userID := session.VerifyUserID.Get(s)
	expectedCode := session.VerifyCode.Get(s)
	reason := session.VerifyReason.Get(s)

	// Fetch user profile
	ctx = ctx.WithValue(redis.SkipCacheKey{}, true)

	userInfo, err := m.layout.roAPI.Users().GetUserByID(ctx.Context(), userID)
	if err != nil {
		m.layout.logger.Error("Failed to fetch user info",
			zap.Error(err),
			zap.Uint64("userID", userID))
		ctx.Error("Failed to verify description. Please try again.")
		return
	}

	// Check if description contains verification code
	if !strings.Contains(userInfo.Description, expectedCode) {
		ctx.Cancel("❌ Verification code not found in description. Please make sure you copied it exactly.")
		return
	}

	// Create appeal
	appeal := &types.Appeal{
		UserID:      userID,
		RequesterID: uint64(ctx.Event().User().ID),
		Status:      enum.AppealStatusPending,
		Timestamp:   time.Now(),
	}

	// Submit appeal
	if err := m.layout.db.Model().Appeal().CreateAppeal(ctx.Context(), appeal, reason); err != nil {
		m.layout.logger.Error("Failed to create appeal", zap.Error(err))
		ctx.Error("Failed to submit appeal. Please try again.")
		return
	}

	session.AppealCursor.Delete(s)
	session.AppealPrevCursors.Delete(s)
	ctx.Show(constants.AppealOverviewPageName, "✅ Account verified and appeal submitted successfully!")

	// Log the appeal submission
	m.layout.db.Model().Activity().Log(ctx.Context(), &types.ActivityLog{
		ActivityTarget: types.ActivityTarget{
			UserID: userID,
		},
		ReviewerID:        uint64(ctx.Event().User().ID),
		ActivityType:      enum.ActivityTypeAppealSubmitted,
		ActivityTimestamp: time.Now(),
		Details: map[string]any{
			"reason": reason,
			"type":   enum.AppealTypeRoblox.String(),
		},
	})
}
