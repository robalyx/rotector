package enum

// LeaderboardPeriod represents different time periods for the leaderboard.
//
//go:generate enumer -type=LeaderboardPeriod -trimprefix=LeaderboardPeriod
type LeaderboardPeriod int

const (
	LeaderboardPeriodDaily LeaderboardPeriod = iota
	LeaderboardPeriodWeekly
	LeaderboardPeriodBiWeekly
	LeaderboardPeriodMonthly
	LeaderboardPeriodBiAnnually
	LeaderboardPeriodAnnually
	LeaderboardPeriodAllTime
)

// VoteType represents the type of entity being voted on.
//
//go:generate enumer -type=VoteType -trimprefix=VoteType
type VoteType int

const (
	VoteTypeUser VoteType = iota
	VoteTypeGroup
)
