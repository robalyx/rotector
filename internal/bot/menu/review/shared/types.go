package shared

import (
	"errors"
	"time"

	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/captcha"
	"github.com/robalyx/rotector/internal/bot/core/interaction"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"go.uber.org/zap"
)

var ErrBreakRequired = errors.New("break required")

// BaseReviewMenu contains common fields and methods for review menus.
type BaseReviewMenu struct {
	logger  *zap.Logger
	captcha *captcha.Manager
}

// NewBaseReviewMenu creates a new base review menu.
func NewBaseReviewMenu(logger *zap.Logger, captcha *captcha.Manager) *BaseReviewMenu {
	return &BaseReviewMenu{
		logger:  logger,
		captcha: captcha,
	}
}

// CheckBreakRequired checks if a break is needed.
func (m *BaseReviewMenu) CheckBreakRequired(ctx *interaction.Context, s *session.Session) bool {
	// Check if user needs a break
	nextReviewTime := session.UserReviewBreakNextReviewTime.Get(s)
	if !nextReviewTime.IsZero() && time.Now().Before(nextReviewTime) {
		// Show timeout menu if break time hasn't passed
		ctx.Show(constants.TimeoutPageName, "")
		return true
	}

	// Check review count
	sessionReviews := session.UserReviewBreakSessionReviews.Get(s)
	sessionStartTime := session.UserReviewBreakSessionStartTime.Get(s)

	// Reset count if outside window
	if time.Since(sessionStartTime) > constants.ReviewSessionWindow {
		sessionReviews = 0
		sessionStartTime = time.Now()
		session.UserReviewBreakSessionStartTime.Set(s, sessionStartTime)
	}

	// Check if break needed
	if sessionReviews >= constants.MaxReviewsBeforeBreak {
		nextTime := time.Now().Add(constants.MinBreakDuration)
		session.UserReviewBreakSessionStartTime.Set(s, nextTime)
		session.UserReviewBreakNextReviewTime.Set(s, nextTime)
		session.UserReviewBreakSessionReviews.Set(s, 0) // Reset count
		ctx.Show(constants.TimeoutPageName, "")
		return true
	}

	// Increment review count
	session.UserReviewBreakSessionReviews.Set(s, sessionReviews+1)

	return false
}

// CheckCaptchaRequired checks if CAPTCHA verification is needed.
func (m *BaseReviewMenu) CheckCaptchaRequired(ctx *interaction.Context, s *session.Session) bool {
	if m.captcha.IsRequired(s) {
		ctx.Cancel("Please complete CAPTCHA verification to continue.")
		return true
	}
	return false
}

// UpdateCounters updates the review counters.
func (m *BaseReviewMenu) UpdateCounters(s *session.Session) {
	if err := m.captcha.IncrementReviewCounter(s); err != nil {
		m.logger.Error("Failed to update review counter", zap.Error(err))
	}
}
