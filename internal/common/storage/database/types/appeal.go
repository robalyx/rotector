package types

import (
	"errors"
	"time"
)

// ErrNoAppealsFound is returned when no appeals are available for review.
var ErrNoAppealsFound = errors.New("no appeals found")

// AppealStatus represents the status of an appeal.
type AppealStatus string

const (
	AppealStatusPending  AppealStatus = "pending"
	AppealStatusAccepted AppealStatus = "accepted"
	AppealStatusRejected AppealStatus = "rejected"
)

// Appeal represents a user appeal request in the database.
type Appeal struct {
	ID           int64        `bun:",pk,autoincrement"` // Unique numeric identifier
	UserID       uint64       `bun:",notnull"`          // The Roblox user ID being appealed
	RequesterID  uint64       `bun:",notnull"`          // The Discord user ID who submitted the appeal
	ReviewerID   uint64       `bun:",nullzero"`         // The Discord user ID who reviewed the appeal
	ReviewedAt   time.Time    `bun:",nullzero"`         // When the appeal was reviewed
	ReviewReason string       `bun:",nullzero"`         // The reason for accepting/rejecting the appeal
	Status       AppealStatus `bun:",nullzero"`         // Status of the appeal (pending, accepted, rejected)
	ClaimedBy    uint64       `bun:",nullzero"`         // Discord ID of reviewer who claimed the appeal
	ClaimedAt    time.Time    `bun:",nullzero"`         // When the appeal was claimed
	Timestamp    time.Time    `bun:"-"`                 // When the appeal was submitted
	LastViewed   time.Time    `bun:"-"`                 // When the appeal was last viewed
	LastActivity time.Time    `bun:"-"`                 // When the last message was sent
}

// AppealTimeline represents the time-series data for appeals in the hypertable.
type AppealTimeline struct {
	ID           int64     `bun:",pk"`         // Reference to Appeal.ID
	Timestamp    time.Time `bun:",pk,notnull"` // When the event occurred
	LastViewed   time.Time `bun:",notnull"`    // When the appeal was last viewed
	LastActivity time.Time `bun:",notnull"`    // When the last message was sent
}

// MessageRole represents the role of the message sender.
type MessageRole string

const (
	MessageRoleUser      MessageRole = "user"
	MessageRoleModerator MessageRole = "moderator"
)

// AppealMessage represents a message in an appeal conversation.
type AppealMessage struct {
	ID        int64       `bun:",pk,autoincrement"` // Unique identifier for the message
	AppealID  int64       `bun:",notnull"`          // ID of the appeal this message belongs to
	UserID    uint64      `bun:",notnull"`          // Discord ID of the message sender
	Role      MessageRole `bun:",notnull"`          // Role of the message sender
	Content   string      `bun:",notnull"`          // Message content
	CreatedAt time.Time   `bun:",notnull"`          // When the message was sent
}

// AppealFields represents the fields that can be requested when fetching appeals.
type AppealFields struct {
	Basic      bool // ID, UserID, RequesterID
	Reason     bool // Appeal reason
	Timestamps bool // SubmittedAt, LastViewed, ReviewedAt
	Review     bool // ReviewerID, ReviewReason, Status
	Messages   bool // Include associated messages
}

// Columns returns the list of database columns to fetch based on the selected fields.
func (f AppealFields) Columns() []string {
	var columns []string

	if f.Basic {
		columns = append(columns, "id", "user_id", "requester_id")
	}
	if f.Reason {
		columns = append(columns, "reason")
	}
	if f.Timestamps {
		columns = append(columns, "submitted_at", "last_viewed", "reviewed_at", "last_activity")
	}
	if f.Review {
		columns = append(columns, "reviewer_id", "review_reason", "status", "claimed_by", "claimed_at")
	}

	// Select all if no fields specified
	if len(columns) == 0 {
		columns = []string{"*"}
	}

	return columns
}
