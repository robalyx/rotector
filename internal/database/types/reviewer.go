package types

import "time"

// ReviewerStats represents statistics for a reviewer's activity.
type ReviewerStats struct {
	ReviewerID     uint64    `json:"reviewerId"`
	UsersViewed    int64     `json:"usersViewed"`
	UsersConfirmed int64     `json:"usersConfirmed"`
	UsersCleared   int64     `json:"usersCleared"`
	LastActivity   time.Time `json:"lastActivity"`
}

// ReviewerStatsCursor represents a pagination cursor for reviewer stats results.
type ReviewerStatsCursor struct {
	LastActivity time.Time `json:"lastActivity"`
	ReviewerID   uint64    `json:"reviewerId"`
}

// ReviewerInfo stores Discord user information for reviewers.
type ReviewerInfo struct {
	UserID      uint64    `bun:",pk"`
	Username    string    `bun:",notnull"`
	DisplayName string    `bun:",notnull"`
	UpdatedAt   time.Time `bun:",notnull"`
}
