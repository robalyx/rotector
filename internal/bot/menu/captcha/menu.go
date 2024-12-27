package captcha

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"image/png"

	"github.com/dchest/captcha"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	builder "github.com/rotector/rotector/internal/bot/builder/captcha"
	"github.com/rotector/rotector/internal/bot/constants"
	"github.com/rotector/rotector/internal/bot/core/pagination"
	"github.com/rotector/rotector/internal/bot/core/session"
	"github.com/rotector/rotector/internal/bot/interfaces"
	"github.com/rotector/rotector/internal/common/storage/database/types"
	"go.uber.org/zap"
)

// Menu handles the CAPTCHA verification interface.
type Menu struct {
	layout *Layout
	page   *pagination.Page
}

// NewMenu creates a new CAPTCHA menu.
func NewMenu(layout *Layout) *Menu {
	m := &Menu{layout: layout}
	m.page = &pagination.Page{
		Name: "CAPTCHA Menu",
		Message: func(s *session.Session) *discord.MessageUpdateBuilder {
			return builder.NewBuilder(s).Build()
		},
		ButtonHandlerFunc: m.handleButton,
		ModalHandlerFunc:  m.handleModal,
	}
	return m
}

// Show displays the CAPTCHA verification interface.
func (m *Menu) Show(event interfaces.CommonEvent, s *session.Session, content string) {
	digits := captcha.RandomDigits(6)

	// Generate CAPTCHA image
	imgBuffer, err := generateCaptchaImage(digits)
	if err != nil {
		m.layout.logger.Error("Failed to generate CAPTCHA image", zap.Error(err))
		m.layout.paginationManager.RespondWithError(event, "Failed to generate CAPTCHA. Please try again.")
		return
	}

	// Store CAPTCHA info in session
	s.Set(constants.SessionKeyCaptchaAnswer, digits)
	s.SetBuffer(constants.SessionKeyCaptchaImage, imgBuffer)

	m.layout.paginationManager.NavigateTo(event, s, m.page, content)
}

// handleButton processes button interactions.
func (m *Menu) handleButton(event *events.ComponentInteractionCreate, s *session.Session, customID string) {
	switch customID {
	case constants.BackButtonCustomID:
		m.layout.paginationManager.NavigateBack(event, s, "")
		s.Delete(constants.SessionKeyCaptchaAnswer)
		s.Delete(constants.SessionKeyCaptchaImage)
	case constants.CaptchaRefreshButtonCustomID:
		m.Show(event, s, "Generated new CAPTCHA.")
	case constants.CaptchaAnswerButtonCustomID:
		modal := discord.NewModalCreateBuilder().
			SetCustomID(constants.CaptchaAnswerModalCustomID).
			SetTitle("Enter CAPTCHA Answer").
			AddActionRow(
				discord.NewTextInput(constants.CaptchaAnswerInputCustomID, discord.TextInputStyleShort, "Answer").
					WithRequired(true).
					WithPlaceholder("Enter the 6 digits you see..."),
			).
			Build()

		if err := event.Modal(modal); err != nil {
			m.layout.logger.Error("Failed to show CAPTCHA modal", zap.Error(err))
			m.layout.paginationManager.RespondWithError(event, "Failed to open CAPTCHA input form. Please try again.")
		}
	}
}

// handleModal processes modal submissions.
func (m *Menu) handleModal(event *events.ModalSubmitInteractionCreate, s *session.Session) {
	if event.Data.CustomID != constants.CaptchaAnswerModalCustomID {
		return
	}

	// Convert user's answer to digits
	answer := event.Data.Text(constants.CaptchaAnswerInputCustomID)
	if len(answer) != 6 {
		m.Show(event, s, "❌ Invalid answer length. Please enter exactly 6 digits.")
		return
	}

	userDigits := make([]byte, 6)
	for i, r := range answer {
		if r < '0' || r > '9' {
			m.Show(event, s, "❌ Invalid answer. Please enter only digits.")
			return
		}
		userDigits[i] = byte(r - '0')
	}

	// Compare answers
	var correctDigits []byte
	s.GetInterface(constants.SessionKeyCaptchaAnswer, &correctDigits)

	if !bytes.Equal(userDigits, correctDigits) {
		m.Show(event, s, "❌ Incorrect CAPTCHA answer. Please try again.")
		return
	}

	// Reset reviews counter
	var settings *types.UserSetting
	s.GetInterface(constants.SessionKeyUserSettings, &settings)
	settings.ReviewsSinceCaptcha = 0

	if err := m.layout.db.Settings().SaveUserSettings(context.Background(), settings); err != nil {
		m.layout.logger.Error("Failed to reset CAPTCHA counter", zap.Error(err))
		m.layout.paginationManager.RespondWithError(event, "Failed to verify CAPTCHA. Please try again.")
		return
	}
	s.Set(constants.SessionKeyUserSettings, settings)

	// Return to previous page
	m.layout.paginationManager.NavigateBack(event, s, "✅ CAPTCHA verified successfully!")
	s.Delete(constants.SessionKeyCaptchaAnswer)
	s.Delete(constants.SessionKeyCaptchaImage)
}

// generateCaptchaImage creates a new CAPTCHA image with the given digits.
func generateCaptchaImage(digits []byte) (*bytes.Buffer, error) {
	// Generate random hex string to use as a CAPTCHA ID
	captchaIDBytes := make([]byte, 16)
	if _, err := rand.Read(captchaIDBytes); err != nil {
		return nil, fmt.Errorf("failed to generate random ID: %w", err)
	}
	captchaID := hex.EncodeToString(captchaIDBytes)

	// Create image from digits
	img := captcha.NewImage(captchaID, digits, captcha.StdWidth, captcha.StdHeight)

	// Create buffer to store PNG image
	buf := new(bytes.Buffer)

	// Encode image as PNG
	if err := png.Encode(buf, img); err != nil {
		return nil, fmt.Errorf("failed to encode CAPTCHA image: %w", err)
	}

	return buf, nil
}
