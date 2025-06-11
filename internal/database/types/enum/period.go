package enum

// ReviewerStatsPeriod represents different time periods for reviewer statistics.
//
//go:generate go tool enumer -type=ReviewerStatsPeriod -trimprefix=ReviewerStatsPeriod
type ReviewerStatsPeriod int

const (
	ReviewerStatsPeriodDaily ReviewerStatsPeriod = iota
	ReviewerStatsPeriodWeekly
	ReviewerStatsPeriodMonthly
)
