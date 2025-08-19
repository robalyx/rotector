package queue

import (
	"fmt"
	"strconv"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/bot/utils"
	"github.com/robalyx/rotector/internal/cloudflare/manager"
)

// Builder creates the visual layout for the queue interface.
type Builder struct {
	stats           *manager.Stats
	queuedUserID    int64
	queuedTimestamp time.Time
	isProcessing    bool
	isProcessed     bool
	isFlagged       bool
	privacyMode     bool
	isAdmin         bool
}

// NewBuilder creates a new queue builder.
func NewBuilder(s *session.Session) *Builder {
	return &Builder{
		stats:           session.QueueStats.Get(s),
		queuedUserID:    session.QueuedUserID.Get(s),
		queuedTimestamp: session.QueuedUserTimestamp.Get(s),
		isProcessing:    session.QueuedUserProcessing.Get(s),
		isProcessed:     session.QueuedUserProcessed.Get(s),
		isFlagged:       session.QueuedUserFlagged.Get(s),
		privacyMode:     session.UserStreamerMode.Get(s),
		isAdmin:         s.BotSettings().IsAdmin(session.UserID.Get(s)),
	}
}

// Build creates a Discord message with queue information.
func (b *Builder) Build() *discord.MessageUpdateBuilder {
	builder := discord.NewMessageUpdateBuilder()

	// Create main info container
	mainInfoDisplays := []discord.ContainerSubComponent{
		discord.NewTextDisplay("# Queue Management"),
		discord.NewLargeSeparator(),
		discord.NewTextDisplay(b.buildQueueStats()),
	}

	// Add queued user info if one exists
	if b.queuedUserID > 0 {
		mainInfoDisplays = append(mainInfoDisplays,
			discord.NewLargeSeparator(),
			b.buildQueuedUserInfo(),
		)
	}

	// Add queue user section
	mainInfoDisplays = append(mainInfoDisplays,
		discord.NewLargeSeparator(),
		discord.NewSection(
			discord.NewTextDisplay("### 📥 Queue Users\nAdd users to the processing queue for automated scanning and review"),
		).WithAccessory(
			discord.NewPrimaryButton("Queue Users", constants.QueueUserButtonCustomID),
		),
		discord.NewSection(
			discord.NewTextDisplay("### 🔍 Direct User Review\nImmediately fetch and review a user's profile without queue processing"),
		).WithAccessory(
			discord.NewPrimaryButton("Review User", constants.ManualUserReviewButtonCustomID),
		),
	)

	// Add group review section only for admins
	if b.isAdmin {
		mainInfoDisplays = append(mainInfoDisplays,
			discord.NewSection(
				discord.NewTextDisplay("### 🔍 Direct Group Review\nImmediately fetch and review a group's profile"),
			).WithAccessory(
				discord.NewPrimaryButton("Review Group", constants.ManualGroupReviewButtonCustomID),
			),
		)
	}

	// Add control buttons
	buttons := []discord.InteractiveComponent{
		discord.NewSecondaryButton("🔄 Refresh", constants.RefreshButtonCustomID),
	}

	// Add abort button if a user is queued and not processed
	if b.queuedUserID > 0 && !b.isProcessed {
		buttons = append(buttons,
			discord.NewDangerButton("❌ Abort", constants.AbortButtonCustomID),
		)
	}

	mainInfoDisplays = append(mainInfoDisplays,
		discord.NewLargeSeparator(),
		discord.NewActionRow(buttons...),
	)

	// Add container and back button to builder
	builder.AddComponents(
		discord.NewContainer(mainInfoDisplays...).
			WithAccentColor(utils.GetContainerColor(b.privacyMode)),
		discord.NewActionRow(
			discord.NewSecondaryButton("◀️ Back", constants.BackButtonCustomID),
		),
	)

	return builder
}

// buildQueueStats formats the queue statistics.
func (b *Builder) buildQueueStats() string {
	if b.stats == nil {
		return "### 📊 Queue Statistics\n*No statistics available.*"
	}

	return fmt.Sprintf(
		"### 📊 Queue Statistics\n"+
			"**Total Items:** %d\n"+
			"**Processing:** %d\n"+
			"**Pending:** %d",
		b.stats.TotalItems,
		b.stats.Processing,
		b.stats.Unprocessed,
	)
}

// buildQueuedUserInfo formats the queued user information.
func (b *Builder) buildQueuedUserInfo() discord.ContainerSubComponent {
	// Determine status and emoji based on user status
	status := "Queued"

	var statusEmoji string

	switch {
	case b.isProcessing:
		status = "Processing"
		statusEmoji = "⚙️"
	case b.isFlagged:
		status = "Flagged"
		statusEmoji = "🚨"
	case b.isProcessed:
		status = "Processed (Cleared)"
		statusEmoji = "✅"
	default:
		statusEmoji = "⏳"
	}

	// Format user ID based on privacy mode
	userIDText := utils.CensorString(strconv.FormatInt(b.queuedUserID, 10), b.privacyMode)

	// Build content string
	content := fmt.Sprintf(
		"### %s Currently Queued User\n"+
			"**User ID:** %s\n"+
			"**Queued:** <t:%d:R>\n"+
			"**Status:** %s",
		statusEmoji,
		userIDText,
		b.queuedTimestamp.Unix(),
		status,
	)

	// If the user is processed and flagged, return a section with review button
	if b.isProcessed && b.isFlagged {
		return discord.NewSection(
			discord.NewTextDisplay(content),
		).WithAccessory(
			discord.NewPrimaryButton("Review User", constants.ReviewQueuedUserButtonCustomID),
		)
	}

	return discord.NewTextDisplay(content)
}
