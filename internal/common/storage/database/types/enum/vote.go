package enum

// VoteType represents the type of entity being voted on.
//
//go:generate enumer -type=VoteType -trimprefix=VoteType
type VoteType int

const (
	VoteTypeUser VoteType = iota
	VoteTypeGroup
)
