package types

import (
	"errors"
	"time"
)

var ErrInvalidVoteType = errors.New("invalid vote type")

// LeaderboardPeriod represents different time periods for the leaderboard.
type LeaderboardPeriod string

const (
	LeaderboardPeriodDaily      LeaderboardPeriod = "daily"
	LeaderboardPeriodWeekly     LeaderboardPeriod = "weekly"
	LeaderboardPeriodBiWeekly   LeaderboardPeriod = "biweekly"
	LeaderboardPeriodMonthly    LeaderboardPeriod = "monthly"
	LeaderboardPeriodBiAnnually LeaderboardPeriod = "biannually"
	LeaderboardPeriodAnnually   LeaderboardPeriod = "annually"
	LeaderboardPeriodAllTime    LeaderboardPeriod = "all_time"
)

// VoteType represents the type of entity being voted on.
type VoteType string

const (
	VoteTypeUser  VoteType = "user"
	VoteTypeGroup VoteType = "group"
)

// LeaderboardCursor represents a pagination cursor for leaderboard results.
type LeaderboardCursor struct {
	CorrectVotes  int64     `json:"correctVotes"`
	Accuracy      float64   `json:"accuracy"`
	VotedAt       time.Time `json:"votedAt"`
	DiscordUserID string    `json:"discordUserId"`
	BaseRank      int       `json:"baseRank"`
}

// GetBaseRank returns the base rank, or 0 if cursor is nil.
func (c *LeaderboardCursor) GetBaseRank() int {
	if c == nil {
		return 0
	}
	return c.BaseRank
}

// Vote represents a vote by a Discord user.
type Vote struct {
	ID            uint64    `bun:",pk"      json:"id"`
	DiscordUserID uint64    `bun:",pk"      json:"discordUserId"`
	IsUpvote      bool      `bun:",notnull" json:"isUpvote"`
	IsCorrect     bool      `bun:",notnull" json:"isCorrect"`
	IsVerified    bool      `bun:",notnull" json:"isVerified"`
	VotedAt       time.Time `bun:",notnull" json:"votedAt"`
}

// UserVote represents a vote on a user by a Discord user.
type UserVote struct {
	Vote `bun:"embed"`
}

// GroupVote represents a vote on a group by a Discord user.
type GroupVote struct {
	Vote `bun:"embed"`
}

// VoteStats tracks voting statistics for Discord users.
type VoteStats struct {
	DiscordUserID uint64    `bun:",pk"      json:"discordUserId"`
	VotedAt       time.Time `bun:",pk"      json:"votedAt"`
	IsCorrect     bool      `bun:",notnull" json:"isCorrect"`
}

// VoteAccuracy represents a user's voting accuracy for leaderboard display.
type VoteAccuracy struct {
	DiscordUserID uint64    `json:"discordUserId"`
	CorrectVotes  int64     `json:"correctVotes"`
	TotalVotes    int64     `json:"totalVotes"`
	Accuracy      float64   `json:"accuracy"`
	VotedAt       time.Time `json:"votedAt"`
	Rank          int       `json:"rank"`
}
