package types

import "time"

// UserFriendCount tracks cached friend counts to avoid redundant friend list fetches.
type UserFriendCount struct {
	UserID      int64     `bun:",pk"      json:"userId"`      // User ID
	FriendCount int       `bun:",notnull" json:"friendCount"` // Current friend count
	LastUpdated time.Time `bun:",notnull" json:"lastUpdated"` // When the count was last cached
}

// UserProcessingLog tracks when users were last processed to prevent duplicate work.
type UserProcessingLog struct {
	UserID        int64     `bun:",pk"      json:"userId"`        // User ID
	LastProcessed time.Time `bun:",notnull" json:"lastProcessed"` // When the user was last processed
}
