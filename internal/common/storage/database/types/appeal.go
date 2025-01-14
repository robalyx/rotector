package types

import (
	"errors"
	"time"

	"github.com/robalyx/rotector/internal/common/storage/database/types/enum"
)

var (
	ErrNoAppealsFound      = errors.New("no appeals found")
	ErrInvalidAppealStatus = errors.New("invalid appeal status")
)

// Appeal represents a user appeal request in the database.
type Appeal struct {
	ID           int64             `bun:",pk,autoincrement"` // Unique numeric identifier
	UserID       uint64            `bun:",notnull"`          // The Roblox user ID being appealed
	RequesterID  uint64            `bun:",notnull"`          // The Discord user ID who submitted the appeal
	ReviewerID   uint64            `bun:",nullzero"`         // The Discord user ID who reviewed the appeal
	ReviewedAt   time.Time         `bun:",nullzero"`         // When the appeal was reviewed
	ReviewReason string            `bun:",nullzero"`         // The reason for accepting/rejecting the appeal
	Status       enum.AppealStatus `bun:",notnull"`          // Status of the appeal (pending, accepted, rejected)
	ClaimedBy    uint64            `bun:",nullzero"`         // Discord ID of reviewer who claimed the appeal
	ClaimedAt    time.Time         `bun:",nullzero"`         // When the appeal was claimed
	Timestamp    time.Time         `bun:"-"`                 // When the appeal was submitted
	LastViewed   time.Time         `bun:"-"`                 // When the appeal was last viewed
	LastActivity time.Time         `bun:"-"`                 // When the last message was sent
}

// AppealTimeline represents the time-series data for appeals in the hypertable.
type AppealTimeline struct {
	ID           int64     `bun:",pk"`         // Reference to Appeal.ID
	Timestamp    time.Time `bun:",pk,notnull"` // When the event occurred
	LastViewed   time.Time `bun:",notnull"`    // When the appeal was last viewed
	LastActivity time.Time `bun:",notnull"`    // When the last message was sent
}

// AppealMessage represents a message in an appeal conversation.
type AppealMessage struct {
	ID        int64            `bun:",pk,autoincrement"` // Unique identifier for the message
	AppealID  int64            `bun:",notnull"`          // ID of the appeal this message belongs to
	UserID    uint64           `bun:",notnull"`          // Discord ID of the message sender
	Role      enum.MessageRole `bun:",notnull"`          // Role of the message sender
	Content   string           `bun:",notnull"`          // Message content
	CreatedAt time.Time        `bun:",notnull"`          // When the message was sent
}
