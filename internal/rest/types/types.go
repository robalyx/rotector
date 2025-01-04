package types

import "time"

// UserStatus represents which table the user exists in.
type UserStatus string

const (
	UserStatusFlagged   UserStatus = "flagged"
	UserStatusConfirmed UserStatus = "confirmed"
	UserStatusCleared   UserStatus = "cleared"
	UserStatusBanned    UserStatus = "banned"
	UserStatusUnflagged UserStatus = "unflagged"
)

// GroupStatus represents which table the group exists in.
type GroupStatus string

const (
	GroupStatusFlagged   GroupStatus = "flagged"
	GroupStatusConfirmed GroupStatus = "confirmed"
	GroupStatusCleared   GroupStatus = "cleared"
	GroupStatusLocked    GroupStatus = "locked"
	GroupStatusUnflagged GroupStatus = "unflagged"
)

// UserGroup represents a group that a user is a member of.
type UserGroup struct {
	ID   uint64 `json:"id"`
	Name string `json:"name"`
	Role string `json:"role"`
}

// Friend represents a user's friend information.
type Friend struct {
	ID               uint64 `json:"id"`
	Name             string `json:"name"`
	DisplayName      string `json:"displayName"`
	HasVerifiedBadge bool   `json:"hasVerifiedBadge"`
}

// Game represents a game that a user has played.
type Game struct {
	ID   uint64 `json:"id"`
	Name string `json:"name"`
}

// User represents detailed user information.
type User struct {
	ID             uint64      `json:"id"`
	Name           string      `json:"name"`
	DisplayName    string      `json:"displayName"`
	Description    string      `json:"description"`
	CreatedAt      time.Time   `json:"createdAt"`
	Reason         string      `json:"reason"`
	Groups         []UserGroup `json:"groups"`
	Friends        []Friend    `json:"friends"`
	Games          []Game      `json:"games"`
	FlaggedContent []string    `json:"flaggedContent"`
	FlaggedGroups  []uint64    `json:"flaggedGroups"`
	FollowerCount  uint64      `json:"followerCount"`
	FollowingCount uint64      `json:"followingCount"`
	Confidence     float64     `json:"confidence"`
	LastScanned    time.Time   `json:"lastScanned"`
	LastUpdated    time.Time   `json:"lastUpdated"`
	LastViewed     time.Time   `json:"lastViewed"`
	ThumbnailURL   string      `json:"thumbnailUrl"`
	Upvotes        int32       `json:"upvotes"`
	Downvotes      int32       `json:"downvotes"`
	Reputation     int32       `json:"reputation"`
}

// GroupUser represents a user in the context of a group.
type GroupUser struct {
	ID          uint64 `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
}

// GroupShout represents a group's shout information.
type GroupShout struct {
	Content string    `json:"content"`
	Poster  GroupUser `json:"poster"`
}

// Group represents detailed group information.
type Group struct {
	ID           uint64     `json:"id"`
	Name         string     `json:"name"`
	Description  string     `json:"description"`
	Owner        GroupUser  `json:"owner"`
	Shout        GroupShout `json:"shout"`
	Reason       string     `json:"reason"`
	Confidence   float64    `json:"confidence"`
	LastScanned  time.Time  `json:"lastScanned"`
	LastUpdated  time.Time  `json:"lastUpdated"`
	LastViewed   time.Time  `json:"lastViewed"`
	ThumbnailURL string     `json:"thumbnailUrl"`
	Upvotes      int32      `json:"upvotes"`
	Downvotes    int32      `json:"downvotes"`
	Reputation   int32      `json:"reputation"`
}

// GetUserResponse represents the response for the get user endpoint.
type GetUserResponse struct {
	Status UserStatus `json:"status,omitempty"`
	User   *User      `json:"user,omitempty"`
}

// GetGroupResponse represents the response for the get group endpoint.
type GetGroupResponse struct {
	Status GroupStatus `json:"status,omitempty"`
	Group  *Group      `json:"group,omitempty"`
}
