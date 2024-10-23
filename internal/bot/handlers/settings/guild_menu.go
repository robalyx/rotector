package settings

import (
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/rotector/rotector/internal/bot/constants"
	"github.com/rotector/rotector/internal/bot/handlers/settings/builders"
	"github.com/rotector/rotector/internal/bot/interfaces"
	"github.com/rotector/rotector/internal/bot/pagination"
	"github.com/rotector/rotector/internal/bot/session"
	"github.com/rotector/rotector/internal/bot/utils"
	"go.uber.org/zap"
)

// GuildMenu is the handler for the guild settings menu.
type GuildMenu struct {
	handler *Handler
	page    *pagination.Page
}

// NewGuildMenu creates a new GuildMenu.
func NewGuildMenu(h *Handler) *GuildMenu {
	g := &GuildMenu{handler: h}
	g.page = &pagination.Page{
		Name: "Guild Settings Menu",
		Data: make(map[string]interface{}),
		Message: func(data map[string]interface{}) *discord.MessageUpdateBuilder {
			roles := data["roles"].([]discord.Role)
			currentValueFunc := data["currentValueFunc"].(func() string)

			return builders.NewGuildSettingsEmbed(currentValueFunc(), roles).Build()
		},
		SelectHandlerFunc: g.handleGuildSettingSelection,
		ButtonHandlerFunc: g.handleGuildSettingButton,
	}
	return g
}

// ShowMenu displays the guild settings menu.
func (g *GuildMenu) ShowMenu(event interfaces.CommonEvent, s *session.Session) {
	// Fetch guild roles
	roles, err := event.Client().Rest().GetRoles(*event.GuildID())
	if err != nil {
		g.handler.logger.Error("Failed to fetch guild roles", zap.Error(err))
		return
	}

	// Fetch current value for the setting
	currentValueFunc := func() string {
		// Fetch guild settings from the database
		settings, err := g.handler.db.Settings().GetGuildSettings(uint64(*event.GuildID()))
		if err != nil {
			g.handler.logger.Error("Failed to fetch guild settings", zap.Error(err))
			return ""
		}

		return utils.FormatWhitelistedRoles(settings.WhitelistedRoles, roles)
	}

	g.page.Data["roles"] = roles
	g.page.Data["currentValueFunc"] = currentValueFunc

	g.handler.paginationManager.NavigateTo(g.page.Name, s)
	g.handler.paginationManager.UpdateMessage(event, s, g.page, "")
}

// handleGuildSettingSelection handles the select menu for the guild settings menu.
func (g *GuildMenu) handleGuildSettingSelection(event *events.ComponentInteractionCreate, s *session.Session, _ string, option string) {
	roles := g.page.Data["roles"].([]discord.Role)
	currentValueFunc := g.page.Data["currentValueFunc"].(func() string)

	switch option {
	case constants.WhitelistedRolesOption:
		// Create options for each role
		options := make([]discord.StringSelectMenuOption, 0, len(roles))
		for _, role := range roles {
			options = append(options, discord.NewStringSelectMenuOption(role.Name, role.ID.String()))
		}

		g.handler.settingMenu.ShowMenu(event, s, "Whitelisted Roles", constants.GuildSettingPrefix, option, currentValueFunc, options)
	}
}

// handleGuildSettingButton handles the buttons for the guild settings menu.
func (g *GuildMenu) handleGuildSettingButton(event *events.ComponentInteractionCreate, _ *session.Session, customID string) {
	if customID == constants.BackButtonCustomID {
		g.handler.dashboardHandler.ShowDashboard(event)
	}
}
