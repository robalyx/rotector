package guild

import (
	"fmt"
	"strings"

	"github.com/disgoorg/disgo/discord"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/common/storage/database/types"
)

// LookupBuilder creates the visual layout for the Discord user lookup interface.
type LookupBuilder struct {
	userID     uint64
	username   string
	userGuilds []*types.UserGuildInfo
	guildNames map[uint64]string
}

// NewLookupBuilder creates a new Discord user builder.
func NewLookupBuilder(s *session.Session) *LookupBuilder {
	return &LookupBuilder{
		userID:     session.DiscordUserLookupID.Get(s),
		username:   session.DiscordUserLookupName.Get(s),
		userGuilds: session.DiscordUserGuilds.Get(s),
		guildNames: session.DiscordUserGuildNames.Get(s),
	}
}

// Build creates a Discord message showing the user's flagged guild memberships.
func (b *LookupBuilder) Build() *discord.MessageUpdateBuilder {
	userEmbed := b.buildUserEmbed()
	guildsEmbed := b.buildGuildsEmbed()

	return discord.NewMessageUpdateBuilder().
		SetEmbeds(userEmbed.Build(), guildsEmbed.Build()).
		AddContainerComponents(
			discord.NewActionRow(
				discord.NewSecondaryButton("◀️", constants.BackButtonCustomID),
				discord.NewSecondaryButton("🔄 Refresh", constants.RefreshButtonCustomID),
			),
		)
}

// buildUserEmbed creates the embed with user information.
func (b *LookupBuilder) buildUserEmbed() *discord.EmbedBuilder {
	description := "Displaying information about this Discord user and their memberships in flagged servers."

	userID := b.userID
	username := b.username
	if username == "" {
		username = fmt.Sprintf("Unknown User (%d)", userID)
	}

	return discord.NewEmbedBuilder().
		SetTitle("Discord User: "+username).
		SetDescription(description).
		AddField("User ID", fmt.Sprintf("`%d`", userID), true).
		AddField("Flagged Servers", fmt.Sprintf("`%d`", len(b.userGuilds)), true).
		AddField("Mention", fmt.Sprintf("<@%d>", userID), true).
		SetColor(constants.DefaultEmbedColor)
}

// buildGuildsEmbed creates the embed showing detailed server membership information.
func (b *LookupBuilder) buildGuildsEmbed() *discord.EmbedBuilder {
	embed := discord.NewEmbedBuilder().
		SetTitle("Server Memberships").
		SetColor(constants.DefaultEmbedColor)

	// Check if the user is not a member of any flagged servers
	if len(b.userGuilds) == 0 {
		embed.SetDescription("This user is not a member of any flagged servers in our database.")
		return embed
	}

	// Create a list of all guild entries
	guildEntries := make([]string, len(b.userGuilds))
	for i, guild := range b.userGuilds {
		guildName := b.guildNames[guild.ServerID]
		if guildName == "" {
			guildName = "Unknown Server"
		}

		joinedTimestamp := guild.JoinedAt.Unix()
		guildEntries[i] = fmt.Sprintf("%d. **%s** (ID: `%d`) - Joined <t:%d:R>",
			i+1,
			guildName,
			guild.ServerID,
			joinedTimestamp,
		)
	}

	// Set guild entries in the description
	description := fmt.Sprintf("This user is a member of %d flagged servers:\n\n%s",
		len(b.userGuilds),
		strings.Join(guildEntries, "\n"))

	embed.SetDescription(description)

	return embed
}
