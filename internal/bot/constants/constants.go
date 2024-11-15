package constants

// Commands.
const (
	DashboardCommandName = "dashboard"
)

// Common.
const (
	NotApplicable            = "N/A"
	ActionSelectMenuCustomID = "action"
	RefreshButtonCustomID    = "refresh"
	BackButtonCustomID       = "back"
	DefaultEmbedColor        = 0x312D2B
	StreamerModeEmbedColor   = 0x3E3769
)

// Dashboard Menu.
const (
	StartReviewCustomID   = "start_review"
	UserSettingsCustomID  = "user_settings"
	GuildSettingsCustomID = "guild_settings"
)

// Review Menu.
const (
	SortOrderSelectMenuCustomID = "sort_order"

	ConfirmWithReasonModalCustomID = "confirm_with_reason_modal"
	ConfirmReasonInputCustomID     = "confirm_reason"
	RecheckReasonModalCustomID     = "recheck_reason_modal"
	RecheckReasonInputCustomID     = "recheck_reason"

	ConfirmWithReasonButtonCustomID = "confirm_with_reason_exception"
	RecheckButtonCustomID           = "recheck_exception"
	ViewUserLogsButtonCustomID      = "view_user_logs"
	OpenOutfitsMenuButtonCustomID   = "open_outfits_menu"
	OpenFriendsMenuButtonCustomID   = "open_friends_menu"
	OpenGroupsMenuButtonCustomID    = "open_groups_menu"
	SwitchReviewModeCustomID        = "switch_review_mode"

	ConfirmButtonCustomID = "confirm"
	ClearButtonCustomID   = "clear"
	SkipButtonCustomID    = "skip"
	AbortButtonCustomID   = "abort"
)

// Friends Menu.
const (
	FriendsPerPage     = 12
	FriendsGridColumns = 3
	FriendsGridRows    = 4
)

// Outfits Menu.
const (
	OutfitsPerPage    = 15
	OutfitGridColumns = 3
	OutfitGridRows    = 5
)

// Groups Menu.
const (
	GroupsPerPage     = 9
	GroupsGridColumns = 3
	GroupsGridRows    = 3
)

// User Settings.
const (
	UserSettingPrefix   = "user"
	UserSettingSelectID = "user_setting_select"
	StreamerModeOption  = "streamer_mode"
	DefaultSortOption   = "default_sort"
	ReviewModeOption    = "review_mode"
)

// Guild Settings.
const (
	GuildSettingPrefix     = "guild"
	GuildSettingSelectID   = "guild_setting_select"
	WhitelistedRolesOption = "whitelisted_roles"
)

// Logs Menu.
const (
	LogsPerPage                         = 10
	LogActivityBrowserCustomID          = "log_activity_browser"
	LogsQueryInputCustomID              = "query_input"
	LogsQueryUserIDOption               = "query_user_id_exception"
	LogsQueryReviewerIDOption           = "query_reviewer_id_exception"
	LogsQueryDateRangeOption            = "query_date_range_exception"
	LogsQueryActivityTypeFilterCustomID = "activity_type_filter"
	ClearFiltersButtonCustomID          = "clear_filters"
)

// Queue Menu.
const (
	QueueManagerCustomID        = "queue_manager"
	QueueHighPriorityCustomID   = "queue_high_priority_exception"
	QueueNormalPriorityCustomID = "queue_normal_priority_exception"
	QueueLowPriorityCustomID    = "queue_low_priority_exception"
	AddToQueueModalCustomID     = "add_to_queue_modal"
	UserIDInputCustomID         = "user_id_input"
	ReasonInputCustomID         = "reason_input"
)

// Session keys.
const (
	SessionKeyMessageID    = "messageID"
	SessionKeyTarget       = "target"
	SessionKeyCurrentPage  = "currentPage"
	SessionKeyPreviousPage = "previousPage"

	SessionKeyImageBuffer = "imageBuffer"

	SessionKeyPaginationPage = "paginationPage"
	SessionKeyStart          = "start"
	SessionKeyTotalItems     = "totalItems"

	SessionKeyConfirmedCount = "confirmedCount"
	SessionKeyFlaggedCount   = "flaggedCount"
	SessionKeyClearedCount   = "clearedCount"
	SessionKeyActiveUsers    = "activeUsers"
	SessionKeyWorkerStatuses = "workerStatuses"

	SessionKeySettingName   = "settingName"
	SessionKeySettingType   = "settingType"
	SessionKeyUserSettings  = "userSettings"
	SessionKeyGuildSettings = "guildSettings"
	SessionKeyCurrentValue  = "currentValue"
	SessionKeyCustomID      = "customID"
	SessionKeyOptions       = "options"
	SessionKeyRoles         = "roles"

	SessionKeyFriends        = "friends"
	SessionKeyPresences      = "presences"
	SessionKeyFlaggedFriends = "flaggedFriends"
	SessionKeyFriendTypes    = "friendTypes"

	SessionKeyGroups        = "groups"
	SessionKeyFlaggedGroups = "flaggedGroups"

	SessionKeyOutfits = "outfits"

	SessionKeyLogs                 = "logs"
	SessionKeyUserIDFilter         = "userID"
	SessionKeyReviewerIDFilter     = "reviewerID"
	SessionKeyActivityTypeFilter   = "activityTypeFilter"
	SessionKeyDateRangeStartFilter = "dateRangeStart"
	SessionKeyDateRangeEndFilter   = "dateRangeEnd"

	SessionKeyQueueUser        = "queueUser"
	SessionKeyQueuePriority    = "queuePriority"
	SessionKeyQueueHighCount   = "queueHighCount"
	SessionKeyQueueNormalCount = "queueNormalCount"
	SessionKeyQueueLowCount    = "queueLowCount"
)
