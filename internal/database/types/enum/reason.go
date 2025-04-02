package enum

// UserReasonType represents the source of a flagging reason.
//
//go:generate go tool enumer -type=UserReasonType -trimprefix=UserReasonType
type UserReasonType int

const (
	// UserReasonTypeDescription indicates content analysis of user profile.
	UserReasonTypeDescription UserReasonType = iota
	// UserReasonTypeFriend indicates friend network analysis.
	UserReasonTypeFriend
	// UserReasonTypeOutfit indicates outfit analysis.
	UserReasonTypeOutfit
	// UserReasonTypeGroup indicates group membership analysis.
	UserReasonTypeGroup
	// UserReasonTypeCondo indicates condo game analysis.
	UserReasonTypeCondo
)

// GroupReasonType represents the source of a flagging reason.
//
//go:generate go tool enumer -type=GroupReasonType -trimprefix=GroupReasonType
type GroupReasonType int

const (
	// GroupReasonTypeMember indicates member analysis.
	GroupReasonTypeMember GroupReasonType = iota
)
