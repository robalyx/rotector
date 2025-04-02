package enum

// AppealSortBy represents different ways to sort appeals.
//
//go:generate go tool enumer -type=AppealSortBy -trimprefix=AppealSortBy
type AppealSortBy int

const (
	// AppealSortByNewest orders appeals by submission time, newest first.
	AppealSortByNewest AppealSortBy = iota
	// AppealSortByOldest orders appeals by submission time, oldest first.
	AppealSortByOldest
	// AppealSortByClaimed orders appeals by claimed status and last activity.
	AppealSortByClaimed
)

// AppealType represents the type of user being appealed.
//
//go:generate go tool enumer -type=AppealType -trimprefix=AppealType
type AppealType int

const (
	// AppealTypeRoblox indicates a Roblox user ID appeal.
	AppealTypeRoblox AppealType = iota
	// AppealTypeDiscord indicates a Discord user ID appeal.
	AppealTypeDiscord
)

// Emoji returns the appropriate emoji for an appeal type.
func (a AppealType) Emoji() string {
	switch a {
	case AppealTypeRoblox:
		return "🎮"
	case AppealTypeDiscord:
		return "💬"
	default:
		return "❔"
	}
}

// AppealStatus represents the status of an appeal.
//
//go:generate go tool enumer -type=AppealStatus -trimprefix=AppealStatus
type AppealStatus int

const (
	AppealStatusPending AppealStatus = iota
	AppealStatusAccepted
	AppealStatusRejected
)

// Emoji returns the appropriate emoji for an appeal status.
func (a AppealStatus) Emoji() string {
	switch a {
	case AppealStatusPending:
		return "⏳"
	case AppealStatusAccepted:
		return "✅"
	case AppealStatusRejected:
		return "❌"
	default:
		return "❔"
	}
}

// MessageRole represents the role of the message sender.
//
//go:generate go tool enumer -type=MessageRole -trimprefix=MessageRole
type MessageRole int

const (
	MessageRoleUser MessageRole = iota
	MessageRoleModerator
)
