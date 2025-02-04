package enum

// ReasonType represents the source of a flagging reason.
//
//go:generate enumer -type=ReasonType -trimprefix=ReasonType
type ReasonType int

const (
	// ReasonTypeUser indicates content analysis of user profile.
	ReasonTypeUser ReasonType = iota
	// ReasonTypeFriend indicates friend network analysis.
	ReasonTypeFriend
	// ReasonTypeImage indicates profile image analysis.
	ReasonTypeImage
	// ReasonTypeOutfit indicates outfit analysis.
	ReasonTypeOutfit
	// ReasonTypeGroup indicates group membership analysis.
	ReasonTypeGroup
	// ReasonTypeMember indicates member analysis.
	ReasonTypeMember
	// ReasonTypeCustom indicates a custom reason.
	ReasonTypeCustom
)
