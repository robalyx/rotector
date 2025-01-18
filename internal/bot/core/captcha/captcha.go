package captcha

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"image/png"

	"github.com/dchest/captcha"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/common/storage/database"
	"github.com/robalyx/rotector/internal/common/storage/database/types"
	"github.com/robalyx/rotector/internal/common/storage/database/types/enum"
	"go.uber.org/zap"
)

// Manager handles CAPTCHA-related operations.
type Manager struct {
	db     *database.Client
	logger *zap.Logger
}

// NewManager creates a new CAPTCHA manager.
func NewManager(db *database.Client, logger *zap.Logger) *Manager {
	return &Manager{
		db:     db,
		logger: logger,
	}
}

// GenerateImage creates a new CAPTCHA image with random digits.
func (m *Manager) GenerateImage() ([]byte, *bytes.Buffer, error) {
	digits := captcha.RandomDigits(6)

	// Generate random hex string to use as a CAPTCHA ID
	captchaIDBytes := make([]byte, 16)
	if _, err := rand.Read(captchaIDBytes); err != nil {
		return nil, nil, fmt.Errorf("failed to generate random ID: %w", err)
	}
	captchaID := hex.EncodeToString(captchaIDBytes)

	// Create image from digits
	img := captcha.NewImage(captchaID, digits, captcha.StdWidth, captcha.StdHeight)

	// Create buffer to store PNG image
	buf := new(bytes.Buffer)

	// Encode image as PNG
	if err := png.Encode(buf, img); err != nil {
		return nil, nil, fmt.Errorf("failed to encode CAPTCHA image: %w", err)
	}

	return digits, buf, nil
}

// IncrementReviewCounter increments the review counter and updates settings.
func (m *Manager) IncrementReviewCounter(ctx context.Context, s *session.Session) error {
	var settings *types.UserSetting
	s.GetInterface(constants.SessionKeyUserSettings, &settings)
	var botSettings *types.BotSetting
	s.GetInterface(constants.SessionKeyBotSettings, &botSettings)

	// Only increment for non-reviewers in training mode
	if !botSettings.IsReviewer(s.UserID()) && settings.ReviewMode == enum.ReviewModeTraining {
		settings.CaptchaUsage.ReviewCount++
	}

	// Save updated settings
	if err := m.db.Settings().SaveUserSettings(ctx, settings); err != nil {
		return fmt.Errorf("failed to update review counter: %w", err)
	}
	s.Set(constants.SessionKeyUserSettings, settings)

	return nil
}

// IsRequired checks if CAPTCHA verification is needed.
func (m *Manager) IsRequired(s *session.Session) bool {
	var settings *types.UserSetting
	s.GetInterface(constants.SessionKeyUserSettings, &settings)

	isRequired := settings.CaptchaUsage.ReviewCount >= 10
	return isRequired
}
