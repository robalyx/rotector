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

	BanWithReasonModalCustomID = "ban_with_reason_modal"

	BanWithReasonButtonCustomID   = "ban_with_reason_modal"
	OpenOutfitsMenuButtonCustomID = "open_outfits_menu"
	OpenFriendsMenuButtonCustomID = "open_friends_menu"
	OpenGroupsMenuButtonCustomID  = "open_groups_menu"

	BanButtonCustomID     = "ban"
	ClearButtonCustomID   = "clear"
	SkipButtonCustomID    = "skip"
	RecheckButtonCustomID = "recheck"
	AbortButtonCustomID   = "abort"

	// Friends Menu.
	FriendsPerPage     = 21
	FriendsGridColumns = 3
	FriendsGridRows    = 7

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
	LogQueryBrowserCustomID             = "log_query_browser"
	LogsQueryInputCustomID              = "query_input"
	LogsQueryUserIDOption               = "query_user_id_modal"
	LogsQueryReviewerIDOption           = "query_reviewer_id_modal"
	LogsQueryDateRangeOption            = "query_date_range_modal"
	LogsQueryActivityTypeFilterCustomID = "activity_type_filter"

	// Queue Menu.
	QueueManagerCustomID        = "queue_manager"
	QueueHighPriorityCustomID   = "queue_high_priority_modal"
	QueueNormalPriorityCustomID = "queue_normal_priority_modal"
	QueueLowPriorityCustomID    = "queue_low_priority_modal"
	AddToQueueModalCustomID     = "add_to_queue_modal"
	UserIDInputCustomID         = "user_id_input"
	ReasonInputCustomID         = "reason_input"

	// Session keys.
	SessionKeyMessageID    = "messageID"
	SessionKeyTarget       = "target"
	SessionKeySortBy       = "sortBy"
	SessionKeyCurrentPage  = "currentPage"
	SessionKeyPreviousPage = "previousPage"

	SessionKeyFile         = "file"
	SessionKeyStreamerMode = "streamerMode"

	SessionKeyPaginationPage = "paginationPage"
	SessionKeyStart          = "start"
	SessionKeyTotalItems     = "totalItems"

	SessionKeyConfirmedCount = "confirmedCount"
	SessionKeyFlaggedCount   = "flaggedCount"
	SessionKeyClearedCount   = "clearedCount"
	SessionKeyStatsChart     = "statsChart"
	SessionKeyActiveUsers    = "activeUsers"

	SessionKeySettingName   = "settingName"
	SessionKeySettingType   = "settingType"
	SessionKeyUserSettings  = "userSettings"
	SessionKeyGuildSettings = "guildSettings"
	SessionKeyCurrentValue  = "currentValue"
	SessionKeyCustomID      = "customID"
	SessionKeyOptions       = "options"
	SessionKeyRoles         = "roles"

	SessionKeyFriends        = "friends"
	SessionKeyFlaggedFriends = "flaggedFriends"

	SessionKeyGroups        = "groups"
	SessionKeyFlaggedGroups = "flaggedGroups"

	SessionKeyOutfits = "outfits"

	SessionKeyLogs               = "logs"
	SessionKeyUserID             = "userID"
	SessionKeyReviewerID         = "reviewerID"
	SessionKeyActivityTypeFilter = "activityTypeFilter"
	SessionKeyDateRangeStart     = "dateRangeStart"
	SessionKeyDateRangeEnd       = "dateRangeEnd"

	SessionKeyQueueUser        = "queueUser"
	SessionKeyQueuePriority    = "queuePriority"
	SessionKeyQueueHighCount   = "queueHighCount"
	SessionKeyQueueNormalCount = "queueNormalCount"
	SessionKeyQueueLowCount    = "queueLowCount"
)
