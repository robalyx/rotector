package guild

import (
	"context"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	builder "github.com/robalyx/rotector/internal/bot/builder/guild"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/pagination"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/bot/interfaces"
	"go.uber.org/zap"
)

// LookupMenu handles the display of Discord user information and their flagged servers.
type LookupMenu struct {
	layout *Layout
	page   *pagination.Page
}

// NewLookupMenu creates a new Discord user lookup menu.
func NewLookupMenu(layout *Layout) *LookupMenu {
	m := &LookupMenu{layout: layout}
	m.page = &pagination.Page{
		Name: constants.GuildLookupPageName,
		Message: func(s *session.Session) *discord.MessageUpdateBuilder {
			return builder.NewLookupBuilder(s).Build()
		},
		ShowHandlerFunc:   m.Show,
		ButtonHandlerFunc: m.handleButton,
	}
	return m
}

// Show prepares and displays the Discord user information interface.
func (m *LookupMenu) Show(event interfaces.CommonEvent, s *session.Session, r *pagination.Respond) {
	// Get Discord user ID from session
	discordUserID := session.DiscordUserLookupID.Get(s)
	if discordUserID == 0 {
		r.Error(event, "Invalid Discord user ID.")
		return
	}

	// Fetch the user's guild memberships from database
	userGuilds, err := m.layout.db.Models().Sync().GetDiscordUserGuilds(
		context.Background(),
		discordUserID,
	)
	if err != nil {
		m.layout.logger.Error("Failed to get Discord user guilds",
			zap.Error(err),
			zap.Uint64("discord_user_id", discordUserID))
		r.Error(event, "Failed to retrieve guild membership data. Please try again.")
		return
	}

	// If we found guilds, get guild names
	guildIDs := make([]uint64, len(userGuilds))
	for i, guild := range userGuilds {
		guildIDs[i] = guild.ServerID
	}

	guildNames := make(map[uint64]string)
	if len(guildIDs) > 0 {
		guildInfos, err := m.layout.db.Models().Sync().GetServerInfo(
			context.Background(),
			guildIDs,
		)
		if err != nil {
			m.layout.logger.Error("Failed to get guild names",
				zap.Error(err),
				zap.Uint64s("guild_ids", guildIDs))
		} else {
			for _, info := range guildInfos {
				guildNames[info.ServerID] = info.Name
			}
		}
	}

	// Store results in session
	session.DiscordUserGuilds.Set(s, userGuilds)
	session.DiscordUserGuildNames.Set(s, guildNames)

	m.layout.logger.Debug("Loaded Discord user guild data",
		zap.Uint64("discord_user_id", discordUserID),
		zap.Int("guild_count", len(userGuilds)))
}

// handleButton processes button interactions.
func (m *LookupMenu) handleButton(event *events.ComponentInteractionCreate, s *session.Session, r *pagination.Respond, customID string) {
	switch customID {
	case constants.BackButtonCustomID:
		r.NavigateBack(event, s, "")
	case constants.RefreshButtonCustomID:
		r.Reload(event, s, "Refreshed Discord user data.")
	}
}
