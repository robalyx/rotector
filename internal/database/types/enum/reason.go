package enum

// UserReasonType represents the source of a flagging reason.
//
//go:generate go tool enumer -type=UserReasonType -trimprefix=UserReasonType
type UserReasonType int

const (
	// UserReasonTypeProfile indicates content analysis of user profile.
	UserReasonTypeProfile UserReasonType = iota
	// UserReasonTypeFriend indicates friend network analysis.
	UserReasonTypeFriend
	// UserReasonTypeOutfit indicates outfit analysis.
	UserReasonTypeOutfit
	// UserReasonTypeGroup indicates group membership analysis.
	UserReasonTypeGroup
	// UserReasonTypeCondo indicates condo game analysis.
	UserReasonTypeCondo
	// UserReasonTypeChat indicates analysis of a user's chat messages.
	UserReasonTypeChat
	// UserReasonTypeFavorites indicates analysis of a user's favorites.
	UserReasonTypeFavorites
	// UserReasonTypeBadges indicates analysis of a user's badges.
	UserReasonTypeBadges
)

// GroupReasonType represents the source of a flagging reason.
//
//go:generate go tool enumer -type=GroupReasonType -trimprefix=GroupReasonType
type GroupReasonType int

const (
	// GroupReasonTypeMember indicates member analysis.
	GroupReasonTypeMember GroupReasonType = iota
	// GroupReasonTypePurpose indicates a group's purpose.
	GroupReasonTypePurpose
	// GroupReasonTypeDescription indicates a group's description.
	GroupReasonTypeDescription
	// GroupReasonTypeShout indicates a group's shout.
	GroupReasonTypeShout
)
