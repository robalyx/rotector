package enum

// AppealSortBy represents different ways to sort appeals.
//
//go:generate enumer -type=AppealSortBy -trimprefix=AppealSortBy
type AppealSortBy int

const (
	// AppealSortByNewest orders appeals by submission time, newest first.
	AppealSortByNewest AppealSortBy = iota
	// AppealSortByOldest orders appeals by submission time, oldest first.
	AppealSortByOldest
	// AppealSortByClaimed orders appeals by claimed status and last activity.
	AppealSortByClaimed
)

// AppealStatus represents the status of an appeal.
//
//go:generate enumer -type=AppealStatus -trimprefix=AppealStatus
type AppealStatus int

const (
	AppealStatusPending AppealStatus = iota
	AppealStatusAccepted
	AppealStatusRejected
)

// MessageRole represents the role of the message sender.
//
//go:generate enumer -type=MessageRole -trimprefix=MessageRole
type MessageRole int

const (
	MessageRoleUser MessageRole = iota
	MessageRoleModerator
)
