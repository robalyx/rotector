package constants

import "time"

// Commands.
const (
	RotectorCommandName = "rotector"
)

// Common.
const (
	NotApplicable            = "N/A"
	ActionSelectMenuCustomID = "action"
	RefreshButtonCustomID    = "refresh"
	BackButtonCustomID       = "back"
	ModalOpenSuffix          = "_exception"
	DefaultEmbedColor        = 0x312D2B
	ErrorEmbedColor          = 0xE74C3C
	StreamerModeEmbedColor   = 0x3E3769
)

// Page Names.
const (
	DashboardPageName = "Dashboard"

	GroupReviewPageName  = "Group Review"
	GroupMembersPageName = "Group Members"

	UserReviewPageName  = "User Review"
	UserFriendsPageName = "Friends Menu"
	UserGroupsPageName  = "Groups Menu"
	UserOutfitsPageName = "Outfits Menu"
	UserStatusPageName  = "Status Menu"

	AdminPageName              = "Admin Menu"
	AdminActionConfirmPageName = "Action Confirmation"

	AppealOverviewPageName = "Appeal Overview"
	AppealTicketPageName   = "Appeal Ticket"
	AppealVerifyPageName   = "Appeal Verification"

	BotSettingPageName    = "Bot Settings"
	UserSettingPageName   = "User Settings"
	SettingUpdatePageName = "Setting Update"

	BanInfoPageName     = "Ban Information"
	CaptchaMenuPageName = "CAPTCHA Verification"
	ChatMenuPageName    = "AI Chat"
	LeaderboardPageName = "Leaderboard"
	LogPageName         = "Activity Logs"
	QueuePageName       = "Queue Manager"
	StatusPageName      = "Status"
)

// Dashboard Menu.
const (
	StartUserReviewButtonCustomID  = "start_user_review"
	StartGroupReviewButtonCustomID = "start_group_review"
	UserSettingsButtonCustomID     = "user_settings"
	ActivityBrowserButtonCustomID  = "activity_browser"
	LeaderboardMenuButtonCustomID  = "leaderboard_menu"
	QueueManagerButtonCustomID     = "queue_manager"
	AdminMenuButtonCustomID        = "admin_menu"
	AppealMenuButtonCustomID       = "appeal_menu"
	ChatAssistantButtonCustomID    = "chat_assistant"
	WorkerStatusButtonCustomID     = "worker_status"
	LookupUserButtonCustomID       = "lookup_user" + ModalOpenSuffix
	LookupGroupButtonCustomID      = "lookup_group" + ModalOpenSuffix

	LookupUserModalCustomID  = "lookup_user_modal"
	LookupUserInputCustomID  = "lookup_user_input"
	LookupGroupModalCustomID = "lookup_group_modal"
	LookupGroupInputCustomID = "lookup_group_input"
)

// Common Review Menu.
const (
	VoteConsensusThreshold = 0.75 // 75% votes in one direction blocks the opposite action
	MinimumVotesRequired   = 10   // Minimum number of votes needed before consensus is enforced

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

	SortOrderSelectMenuCustomID = "sort_order"

	ConfirmWithReasonModalCustomID = "confirm_with_reason_modal"
	ConfirmConfidenceInputCustomID = "confirm_confidence"
	ConfirmReasonInputCustomID     = "confirm_reason"
	RecheckReasonModalCustomID     = "recheck_reason_modal"
	RecheckReasonInputCustomID     = "recheck_reason"

	ConfirmButtonCustomID = "confirm"
	ClearButtonCustomID   = "clear"
	SkipButtonCustomID    = "skip"
)

// User Review Menu.
const (
	OpenAIChatButtonCustomID        = "open_ai_chat"
	ConfirmWithReasonButtonCustomID = "confirm_with_reason" + ModalOpenSuffix
	RecheckButtonCustomID           = "recheck" + ModalOpenSuffix
	ViewUserLogsButtonCustomID      = "view_user_logs"
	OpenOutfitsMenuButtonCustomID   = "open_outfits_menu"
	OpenFriendsMenuButtonCustomID   = "open_friends_menu"
	OpenGroupsMenuButtonCustomID    = "open_groups_menu"
	AbortButtonCustomID             = "abort"
)

// User Review Menu - Friends Viewer.
const (
	FriendsPerPage     = 12
	FriendsGridColumns = 3
	FriendsGridRows    = 4
)

// User Review Menu - Outfits Viewer.
const (
	OutfitsPerPage    = 15
	OutfitGridColumns = 3
	OutfitGridRows    = 5
)

// User Review Menu - Groups Viewer.
const (
	GroupsPerPage     = 9
	GroupsGridColumns = 3
	GroupsGridRows    = 3
)

// Group Review Menu.
const (
	GroupConfirmWithReasonButtonCustomID = "group_confirm_with_reason" + ModalOpenSuffix
	GroupRecheckButtonCustomID           = "group_recheck" + ModalOpenSuffix
	GroupViewMembersButtonCustomID       = "group_view_members"
	GroupViewLogsButtonCustomID          = "group_view_logs"
)

// Group Review Menu - Members Viewer.
const (
	MembersPerPage     = 12
	MembersGridColumns = 3
	MembersGridRows    = 4
)

// Chat Menu.
const (
	MaxChatMessagesPerDay = 50
	ChatMessageResetLimit = 24 * time.Hour

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
	UserSettingPrefix        = "user"
	UserSettingSelectID      = "user_setting_select"
	StreamerModeOption       = "streamer_mode"
	UserDefaultSortOption    = "user_default_sort"
	GroupDefaultSortOption   = "group_default_sort"
	AppealDefaultSortOption  = "appeal_default_sort"
	AppealStatusFilterOption = "appeal_status_filter"
	ChatModelOption          = "chat_model"
	ReviewModeOption         = "review_mode"
	ReviewTargetModeOption   = "review_target_mode"
)

// Bot Settings.
const (
	SettingsIDsPerPage  = 15
	SettingsKeysPerPage = 9

	BotSettingPrefix          = "bot"
	BotSettingSelectID        = "bot_setting_select"
	ReviewerIDsOption         = "reviewer_ids"
	AdminIDsOption            = "admin_ids"
	SessionLimitOption        = "session_limit"
	WelcomeMessageOption      = "welcome_message"
	AnnouncementTypeOption    = "announcement_type"
	AnnouncementMessageOption = "announcement_message"

	APIKeysOption        = "api_keys"
	APIKeyCreateIDOption = "api_key_create" + ModalOpenSuffix
	APIKeyDeleteIDOption = "api_key_delete" + ModalOpenSuffix
	APIKeyUpdateIDOption = "api_key_update" + ModalOpenSuffix
)

// Logs Menu.
const (
	LogsPerPage                         = 10
	LogsQueryInputCustomID              = "query_input"
	LogsQueryDiscordIDOption            = "query_discord_id" + ModalOpenSuffix
	LogsQueryUserIDOption               = "query_user_id" + ModalOpenSuffix
	LogsQueryGroupIDOption              = "query_group_id" + ModalOpenSuffix
	LogsQueryReviewerIDOption           = "query_reviewer_id" + ModalOpenSuffix
	LogsQueryDateRangeOption            = "query_date_range" + ModalOpenSuffix
	LogsQueryActivityTypeFilterCustomID = "activity_type_filter"
	ClearFiltersButtonCustomID          = "clear_filters"
)

// Queue Menu.
const (
	QueueHighPriorityCustomID   = "queue_high_priority" + ModalOpenSuffix
	QueueNormalPriorityCustomID = "queue_normal_priority" + ModalOpenSuffix
	QueueLowPriorityCustomID    = "queue_low_priority" + ModalOpenSuffix
	AddToQueueModalCustomID     = "add_to_queue_modal"
	UserIDInputCustomID         = "user_id_input"
	ReasonInputCustomID         = "reason_input"
)

// Appeal Menu.
const (
	AppealModalCustomID       = "appeal_modal"
	AppealUserInputCustomID   = "appeal_user_input"
	AppealReasonInputCustomID = "appeal_reason_input"

	AppealLookupUserButtonCustomID = "appeal_lookup_user"
	AcceptAppealButtonCustomID     = "accept_appeal" + ModalOpenSuffix
	RejectAppealButtonCustomID     = "reject_appeal" + ModalOpenSuffix
	AppealCloseButtonCustomID      = "appeal_close"

	AcceptAppealModalCustomID  = "accept_appeal_modal"
	RejectAppealModalCustomID  = "reject_appeal_modal"
	AppealRespondModalCustomID = "appeal_respond_modal"

	AppealsPerPage              = 5
	AppealMessagesPerPage       = 5
	AppealSelectID              = "appeal_select"
	AppealStatusSelectID        = "appeal_status"
	AppealSortSelectID          = "appeal_sort"
	AppealCreateButtonCustomID  = "appeal_create" + ModalOpenSuffix
	AppealRespondButtonCustomID = "appeal_respond" + ModalOpenSuffix

	VerifyDescriptionButtonID = "verify_description"
)

// CAPTCHA Menu.
const (
	CaptchaTimeout = 5 * time.Minute

	CaptchaAnswerButtonCustomID  = "captcha_answer" + ModalOpenSuffix
	CaptchaRefreshButtonCustomID = "captcha_refresh"
	CaptchaAnswerModalCustomID   = "captcha_answer_modal"
	CaptchaAnswerInputCustomID   = "captcha_answer_input"
)

// Admin Menu.
const (
	BotSettingsButtonCustomID = "bot_settings"
	BanUserButtonCustomID     = "ban_user" + ModalOpenSuffix
	UnbanUserButtonCustomID   = "unban_user" + ModalOpenSuffix
	DeleteUserButtonCustomID  = "delete_user" + ModalOpenSuffix
	DeleteGroupButtonCustomID = "delete_group" + ModalOpenSuffix

	BanUserModalCustomID     = "ban_user_modal"
	UnbanUserModalCustomID   = "unban_user_modal"
	DeleteUserModalCustomID  = "delete_user_modal"
	DeleteGroupModalCustomID = "delete_group_modal"

	BanUserInputCustomID     = "ban_user_input"
	BanTypeInputCustomID     = "ban_type_input"
	BanDurationInputCustomID = "ban_duration_input"
	UnbanUserInputCustomID   = "unban_user_input"
	DeleteUserInputCustomID  = "delete_user_input"
	DeleteGroupInputCustomID = "delete_group_input"
	AdminReasonInputCustomID = "admin_reason_input"

	ActionButtonCustomID = "delete_confirm"

	BanUserAction     = "ban_user"
	UnbanUserAction   = "unban_user"
	DeleteUserAction  = "delete_user"
	DeleteGroupAction = "delete_group"
)

// Leaderboard Menu
const (
	LeaderboardEntriesPerPage           = 10
	LeaderboardPeriodSelectMenuCustomID = "leaderboard_period"
)
