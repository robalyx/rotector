package types

import (
	"time"
)

// InappropriateMessage represents a flagged inappropriate message from a Discord user.
type InappropriateMessage struct {
	ServerID   uint64    `bun:",pk"        json:"serverId"`   // Discord server ID
	ChannelID  uint64    `bun:",pk"        json:"channelId"`  // Discord channel ID
	UserID     uint64    `bun:",pk"        json:"userId"`     // Discord user ID
	MessageID  string    `bun:",pk"        json:"messageId"`  // Discord message ID
	Content    string    `bun:",type:text" json:"content"`    // Message content
	Reason     string    `bun:",type:text" json:"reason"`     // Reason message was flagged
	Confidence float64   `bun:",notnull"   json:"confidence"` // AI confidence score
	DetectedAt time.Time `bun:",notnull"   json:"detectedAt"` // When the message was flagged
	UpdatedAt  time.Time `bun:",notnull"   json:"updatedAt"`  // Last update time
}

// InappropriateUserSummary contains summary info about a user with inappropriate messages.
type InappropriateUserSummary struct {
	UserID       uint64    `bun:",pk"        json:"userId"`       // Discord user ID
	Reason       string    `bun:",type:text" json:"reason"`       // Recent reason for flagging
	MessageCount int       `bun:",notnull"   json:"messageCount"` // Number of inappropriate messages
	LastDetected time.Time `bun:",notnull"   json:"lastDetected"` // When the last message was flagged
	UpdatedAt    time.Time `bun:",notnull"   json:"updatedAt"`    // Last update time
}

// MessageCursor represents a cursor for paginating through messages.
type MessageCursor struct {
	DetectedAt time.Time `json:"detectedAt"` // Timestamp for cursor position
	MessageID  string    `json:"messageId"`  // Message ID for cursor position
}
