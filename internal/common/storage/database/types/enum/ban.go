package enum

// BanReason represents the reason for a Discord user ban.
//
//go:generate enumer -type=BanReason -trimprefix=BanReason
type BanReason int

const (
	BanReasonAbuse BanReason = iota
	BanReasonInappropriate
	BanReasonOther
)

// BanSource indicates what triggered a ban.
//
//go:generate enumer -type=BanSource -trimprefix=BanSource
type BanSource int

const (
	BanSourceSystem BanSource = iota
	BanSourceAdmin
)
