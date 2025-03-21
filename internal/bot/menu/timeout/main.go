package timeout

import (
	"github.com/disgoorg/disgo/discord"
	builder "github.com/robalyx/rotector/internal/bot/builder/timeout"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/interaction"
	"github.com/robalyx/rotector/internal/bot/core/session"
)

// Menu handles timeout menu operations.
type Menu struct {
	layout *Layout
	page   *interaction.Page
}

// NewMenu creates a new timeout menu.
func NewMenu(layout *Layout) *Menu {
	m := &Menu{layout: layout}
	m.page = &interaction.Page{
		Name: constants.TimeoutPageName,
		Message: func(s *session.Session) *discord.MessageUpdateBuilder {
			return builder.NewBuilder(s).Build()
		},
		ButtonHandlerFunc: m.handleButton,
	}
	return m
}

// handleButton processes button interactions.
func (m *Menu) handleButton(ctx *interaction.Context, _ *session.Session, customID string) {
	switch customID {
	case constants.BackButtonCustomID:
		ctx.NavigateBack("")
	}
}
