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
	ModalOpenSuffix          = "_exception"
	DefaultEmbedColor        = 0x312D2B
	StreamerModeEmbedColor   = 0x3E3769
)

// Dashboard Menu.
const (
	StartUserReviewCustomID    = "start_user_review"
	StartGroupReviewCustomID   = "start_group_review"
	UserSettingsCustomID       = "user_settings"
	BotSettingsCustomID        = "bot_settings"
	ChatAssistantCustomID      = "chat_assistant"
	SessionKeyUserStatsBuffer  = "userStatsBuffer"
	SessionKeyGroupStatsBuffer = "groupStatsBuffer"
	SessionKeyUserCounts       = "userCounts"
	SessionKeyGroupCounts      = "groupCounts"
)

// Review Menu.
const (
	SortOrderSelectMenuCustomID = "sort_order"

	ConfirmWithReasonModalCustomID = "confirm_with_reason_modal"
	ConfirmReasonInputCustomID     = "confirm_reason"
	RecheckReasonModalCustomID     = "recheck_reason_modal"
	RecheckReasonInputCustomID     = "recheck_reason"

	OpenAIChatButtonCustomID        = "open_ai_chat"
	ConfirmWithReasonButtonCustomID = "confirm_with_reason" + ModalOpenSuffix
	RecheckButtonCustomID           = "recheck" + ModalOpenSuffix
	ViewUserLogsButtonCustomID      = "view_user_logs"
	OpenOutfitsMenuButtonCustomID   = "open_outfits_menu"
	OpenFriendsMenuButtonCustomID   = "open_friends_menu"
	OpenGroupsMenuButtonCustomID    = "open_groups_menu"
	SwitchReviewModeCustomID        = "switch_review_mode"
	SwitchTargetModeCustomID        = "switch_target_mode"

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

// Chat Menu.
const (
	ChatMessagesPerPage      = 2
	ChatSendButtonID         = "chat_send" + ModalOpenSuffix
	ChatInputModalID         = "chat_input_modal"
	ChatInputCustomID        = "chat_input"
	ChatClearHistoryButtonID = "chat_clear_history"
	ChatClearContextButtonID = "chat_clear_context"
	ChatModelSelectID        = "chat_model_select"
	ChatAnalyzeUserID        = "chat_analyze_user"
)

// User Settings.
const (
	UserSettingPrefix      = "user"
	UserSettingSelectID    = "user_setting_select"
	StreamerModeOption     = "streamer_mode"
	UserDefaultSortOption  = "user_default_sort"
	GroupDefaultSortOption = "group_default_sort"
	ChatModelOption        = "chat_model"
	ReviewModeOption       = "review_mode"
	ReviewTargetModeOption = "review_target_mode"
)

// Bot Settings.
const (
	BotSettingPrefix     = "bot"
	BotSettingSelectID   = "bot_setting_select"
	ReviewerIDsOption    = "reviewer_ids"
	AdminIDsOption       = "admin_ids"
	SessionLimitOption   = "session_limit"
	WelcomeMessageOption = "welcome_message"
)

// Logs Menu.
const (
	LogsPerPage                         = 10
	LogActivityBrowserCustomID          = "log_activity_browser"
	LogsQueryInputCustomID              = "query_input"
	LogsQueryUserIDOption               = "query_user_id" + ModalOpenSuffix
	LogsQueryGroupIDOption              = "query_group_id" + ModalOpenSuffix
	LogsQueryReviewerIDOption           = "query_reviewer_id" + ModalOpenSuffix
	LogsQueryDateRangeOption            = "query_date_range" + ModalOpenSuffix
	LogsQueryActivityTypeFilterCustomID = "activity_type_filter"
	ClearFiltersButtonCustomID          = "clear_filters"
)

// Queue Menu.
const (
	QueueManagerCustomID        = "queue_manager"
	QueueHighPriorityCustomID   = "queue_high_priority" + ModalOpenSuffix
	QueueNormalPriorityCustomID = "queue_normal_priority" + ModalOpenSuffix
	QueueLowPriorityCustomID    = "queue_low_priority" + ModalOpenSuffix
	AddToQueueModalCustomID     = "add_to_queue_modal"
	UserIDInputCustomID         = "user_id_input"
	ReasonInputCustomID         = "reason_input"
)

// Session keys.
const (
	SessionKeyUserID       = "userID"
	SessionKeyMessageID    = "messageID"
	SessionKeyTarget       = "target"
	SessionKeyCurrentPage  = "currentPage"
	SessionKeyPreviousPage = "previousPage"

	SessionKeyImageBuffer = "imageBuffer"

	SessionKeyCursor         = "cursor"
	SessionKeyNextCursor     = "nextCursor"
	SessionKeyPrevCursors    = "prevCursors"
	SessionKeyHasNextPage    = "hasNextPage"
	SessionKeyHasPrevPage    = "hasPrevPage"
	SessionKeyPaginationPage = "paginationPage"
	SessionKeyStart          = "start"
	SessionKeyTotalItems     = "totalItems"
	SessionKeyIsStreaming    = "isStreaming"

	SessionKeyConfirmedCount = "confirmedCount"
	SessionKeyFlaggedCount   = "flaggedCount"
	SessionKeyClearedCount   = "clearedCount"
	SessionKeyActiveUsers    = "activeUsers"
	SessionKeyWorkerStatuses = "workerStatuses"

	SessionKeySettingName  = "settingName"
	SessionKeySettingType  = "settingType"
	SessionKeySetting      = "setting"
	SessionKeyUserSettings = "userSettings"
	SessionKeyBotSettings  = "botSettings"
	SessionKeyCurrentValue = "currentValue"
	SessionKeyCustomID     = "customID"
	SessionKeyOptions      = "options"
	SessionKeyRoles        = "roles"

	SessionKeyFriends        = "friends"
	SessionKeyPresences      = "presences"
	SessionKeyFlaggedFriends = "flaggedFriends"
	SessionKeyFriendTypes    = "friendTypes"

	SessionKeyGroups        = "groups"
	SessionKeyFlaggedGroups = "flaggedGroups"
	SessionKeyGroupTypes    = "groupTypes"

	SessionKeyOutfits = "outfits"

	SessionKeyChatHistory = "chatHistory"
	SessionKeyChatContext = "chatContext"

	SessionKeyLogs                 = "logs"
	SessionKeyUserIDFilter         = "userIDFilter"
	SessionKeyGroupIDFilter        = "groupIDFilter"
	SessionKeyReviewerIDFilter     = "reviewerIDFilter"
	SessionKeyActivityTypeFilter   = "activityTypeFilter"
	SessionKeyDateRangeStartFilter = "dateRangeStartFilter"
	SessionKeyDateRangeEndFilter   = "dateRangeEndFilter"

	SessionKeyQueueUser        = "queueUser"
	SessionKeyQueueStatus      = "queueStatus"
	SessionKeyQueuePriority    = "queuePriority"
	SessionKeyQueuePosition    = "queuePosition"
	SessionKeyQueueHighCount   = "queueHighCount"
	SessionKeyQueueNormalCount = "queueNormalCount"
	SessionKeyQueueLowCount    = "queueLowCount"

	SessionKeyGroupTarget       = "groupTarget"
	SessionKeyGroupFlaggedUsers = "groupFlaggedUsers"

	GroupConfirmWithReasonModalCustomID = "group_confirm_with_reason_modal"
	GroupConfirmReasonInputCustomID     = "group_confirm_reason"
	GroupRecheckReasonModalCustomID     = "group_recheck_reason_modal"
	GroupRecheckReasonInputCustomID     = "group_recheck_reason"

	GroupConfirmWithReasonButtonCustomID = "group_confirm_with_reason" + ModalOpenSuffix
	GroupRecheckButtonCustomID           = "group_recheck" + ModalOpenSuffix
	GroupViewLogsButtonCustomID          = "group_view_logs"

	GroupConfirmButtonCustomID = "group_confirm"
	GroupClearButtonCustomID   = "group_clear"
	GroupSkipButtonCustomID    = "group_skip"
)

const (
	// ReviewHistoryLimit caps the number of review history entries shown.
	ReviewHistoryLimit = 5

	// ReviewFriendsLimit caps the number of friends shown in the main review embed
	// to prevent the embed from becoming too long.
	ReviewFriendsLimit = 10

	// ReviewGroupsLimit caps the number of groups shown in the main review embed
	// to prevent the embed from becoming too long.
	ReviewGroupsLimit = 10

	// ReviewGamesLimit caps the number of games shown in the main review embed
	// to prevent the embed from becoming too long.
	ReviewGamesLimit = 10

	// ReviewOutfitsLimit caps the number of outfits shown in the main review embed
	// to prevent the embed from becoming too long.
	ReviewOutfitsLimit = 10
)
