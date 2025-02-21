package captcha

import (
	"bytes"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	builder "github.com/robalyx/rotector/internal/bot/builder/captcha"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/captcha"
	"github.com/robalyx/rotector/internal/bot/core/pagination"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/bot/interfaces"
	"go.uber.org/zap"
)

// Menu handles the CAPTCHA verification interface.
type Menu struct {
	layout  *Layout
	page    *pagination.Page
	captcha *captcha.Manager
}

// NewMenu creates a new CAPTCHA menu.
func NewMenu(layout *Layout) *Menu {
	m := &Menu{layout: layout}
	m.page = &pagination.Page{
		Name: constants.CaptchaPageName,
		Message: func(s *session.Session) *discord.MessageUpdateBuilder {
			return builder.NewBuilder(s).Build()
		},
		CleanupHandlerFunc: m.Cleanup,
		ShowHandlerFunc:    m.Show,
		ButtonHandlerFunc:  m.handleButton,
		ModalHandlerFunc:   m.handleModal,
	}
	return m
}

// Show displays the CAPTCHA verification interface.
func (m *Menu) Show(event interfaces.CommonEvent, s *session.Session, r *pagination.Respond) {
	digits, imgBuffer, err := m.captcha.GenerateImage()
	if err != nil {
		m.layout.logger.Error("Failed to generate CAPTCHA image", zap.Error(err))
		r.Error(event, "Failed to generate CAPTCHA. Please try again.")
		return
	}

	session.CaptchaAnswer.Set(s, string(digits))
	session.ImageBuffer.Set(s, imgBuffer)

	r.Show(event, s, constants.CaptchaPageName, "Generated new CAPTCHA.")
}

// Cleanup handles the cleanup of the CAPTCHA menu.
func (m *Menu) Cleanup(s *session.Session) {
	session.CaptchaAnswer.Delete(s)
	session.ImageBuffer.Delete(s)
}

// handleButton processes button interactions.
func (m *Menu) handleButton(event *events.ComponentInteractionCreate, s *session.Session, r *pagination.Respond, customID string) {
	switch customID {
	case constants.BackButtonCustomID:
		r.NavigateBack(event, s, "")
		session.CaptchaAnswer.Delete(s)
		session.ImageBuffer.Delete(s)
	case constants.CaptchaRefreshButtonCustomID:
		r.Reload(event, s, "Generated new CAPTCHA.")
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
			r.Error(event, "Failed to open CAPTCHA input form. Please try again.")
		}
	}
}

// handleModal processes modal submissions.
func (m *Menu) handleModal(event *events.ModalSubmitInteractionCreate, s *session.Session, r *pagination.Respond) {
	if event.Data.CustomID != constants.CaptchaAnswerModalCustomID {
		return
	}

	// Convert user's answer to digits
	answer := event.Data.Text(constants.CaptchaAnswerInputCustomID)
	if len(answer) != 6 {
		r.Cancel(event, s, "❌ Invalid answer length. Please enter exactly 6 digits.")
		return
	}

	userDigits := make([]byte, 6)
	for i, rn := range answer {
		if rn < '0' || rn > '9' {
			r.Cancel(event, s, "❌ Invalid answer. Please enter only digits.")
			return
		}
		userDigits[i] = byte(rn - '0')
	}

	// Compare answers
	correctDigits := session.CaptchaAnswer.Get(s)

	if !bytes.Equal(userDigits, []byte(correctDigits)) {
		r.Cancel(event, s, "❌ Incorrect CAPTCHA answer. Please try again.")
		return
	}

	// Reset reviews counter
	session.UserCaptchaUsageCaptchaReviewCount.Set(s, 0)

	// Return to previous page
	r.NavigateBack(event, s, "✅ CAPTCHA verified successfully!")
	session.CaptchaAnswer.Delete(s)
	session.ImageBuffer.Delete(s)
}
