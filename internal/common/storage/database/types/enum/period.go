package enum

// LeaderboardPeriod represents different time periods for the leaderboard.
//
//go:generate go tool enumer -type=LeaderboardPeriod -trimprefix=LeaderboardPeriod
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

// ReviewerStatsPeriod represents different time periods for reviewer statistics.
//
//go:generate go tool enumer -type=ReviewerStatsPeriod -trimprefix=ReviewerStatsPeriod
type ReviewerStatsPeriod int

const (
	ReviewerStatsPeriodDaily ReviewerStatsPeriod = iota
	ReviewerStatsPeriodWeekly
	ReviewerStatsPeriodMonthly
)
