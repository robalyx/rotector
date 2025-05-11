package types

import (
	"time"
)

// UserComment represents a community note/comment on a user.
type UserComment struct {
	Comment `json:"comment"`
}

// GroupComment represents a community note/comment on a group.
type GroupComment struct {
	Comment `json:"comment"`
}

// Comment represents a community note/comment on a user or group.
type Comment struct {
	TargetID    uint64    `bun:",pk,notnull" json:"targetId"`    // User or Group ID
	CommenterID uint64    `bun:",pk,notnull" json:"commenterId"` // Discord user ID who wrote the comment
	Message     string    `bun:",notnull"    json:"message"`
	CreatedAt   time.Time `bun:",notnull"    json:"createdAt"`
	UpdatedAt   time.Time `bun:",notnull"    json:"updatedAt"`
}
