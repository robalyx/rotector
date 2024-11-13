package constants

const (
	// Commands.
	DashboardCommandName = "dashboard"

	// Common.
	NotApplicable            = "N/A"
	ActionSelectMenuCustomID = "action"
	RefreshButtonCustomID    = "refresh"
	BackButtonCustomID       = "back"
	DefaultEmbedColor        = 0x312D2B
	StreamerModeEmbedColor   = 0x3E3769

	// Dashboard Menu.
	StartReviewCustomID   = "start_review"
	UserSettingsCustomID  = "user_settings"
	GuildSettingsCustomID = "guild_settings"

	// Review Menu.
	SortOrderSelectMenuCustomID = "sort_order"

	ConfirmWithReasonModalCustomID = "confirm_with_reason_modal"
	ConfirmReasonInputCustomID     = "confirm_reason"

	ConfirmWithReasonButtonCustomID = "confirm_with_reason_exception"
	RecheckButtonCustomID           = "recheck"
	OpenOutfitsMenuButtonCustomID   = "open_outfits_menu"
	OpenFriendsMenuButtonCustomID   = "open_friends_menu"
	OpenGroupsMenuButtonCustomID    = "open_groups_menu"

	ConfirmButtonCustomID = "confirm"
	ClearButtonCustomID   = "clear"
	SkipButtonCustomID    = "skip"
	AbortButtonCustomID   = "abort"

	// Friends Menu.
	FriendsPerPage     = 12
	FriendsGridColumns = 3
	FriendsGridRows    = 4

	// Outfits Menu.
	OutfitsPerPage    = 15
	OutfitGridColumns = 3
	OutfitGridRows    = 5

	// Groups Menu.
	GroupsPerPage     = 15
	GroupsGridColumns = 3
	GroupsGridRows    = 5

	// User Settings.
	UserSettingPrefix   = "user"
	UserSettingSelectID = "user_setting_select"
	StreamerModeOption  = "streamer_mode"
	DefaultSortOption   = "default_sort"

	// Guild Settings.
	GuildSettingPrefix     = "guild"
	GuildSettingSelectID   = "guild_setting_select"
	WhitelistedRolesOption = "whitelisted_roles"

	// Logs Menu.
	LogsPerPage                         = 10
	LogActivityBrowserCustomID          = "log_activity_browser"
	LogsQueryInputCustomID              = "query_input"
	LogsQueryUserIDOption               = "query_user_id_exception"
	LogsQueryReviewerIDOption           = "query_reviewer_id_exception"
	LogsQueryDateRangeOption            = "query_date_range_exception"
	LogsQueryActivityTypeFilterCustomID = "activity_type_filter"
	ClearFiltersButtonCustomID          = "clear_filters"

	// Queue Menu.
	QueueManagerCustomID        = "queue_manager"
	QueueHighPriorityCustomID   = "queue_high_priority_exception"
	QueueNormalPriorityCustomID = "queue_normal_priority_exception"
	QueueLowPriorityCustomID    = "queue_low_priority_exception"
	AddToQueueModalCustomID     = "add_to_queue_modal"
	UserIDInputCustomID         = "user_id_input"
	ReasonInputCustomID         = "reason_input"

	// Session keys.
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
