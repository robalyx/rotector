package constants

import "time"

// RotectorCommandName is the main command name for the rotector bot.
const (
	RotectorCommandName = "rotector"
)

// Common.
const (
	UnknownServer            = "Unknown Server"
	NotApplicable            = "N/A"
	ActionSelectMenuCustomID = "action"
	RefreshButtonCustomID    = "refresh"
	BackButtonCustomID       = "back"
	ModalOpenSuffix          = "_exception"

	DefaultContainerColor      = 0x393A41
	ErrorContainerColor        = 0xE74C3C
	StreamerModeContainerColor = 0x3E3769
)

// Page Names.
const (
	SessionSelectorPageName = "Session Selector"
	DashboardPageName       = "Dashboard"
	ConsentPageName         = "Terms of Service"

	GroupReviewPageName   = "Group Review"
	GroupMembersPageName  = "Group Members"
	GroupCommentsPageName = "Group Comments"

	UserReviewPageName   = "User Review"
	UserFriendsPageName  = "Friends Menu"
	UserGroupsPageName   = "Groups Menu"
	UserOutfitsPageName  = "Outfits Menu"
	UserCaesarPageName   = "Caesar Cipher Menu"
	UserCommentsPageName = "User Comments"

	QueuePageName = "Queue Management"

	AdminPageName              = "Admin Menu"
	AdminActionConfirmPageName = "Action Confirmation"

	BotSettingsPageName   = "Bot Settings"
	UserSettingsPageName  = "User Settings"
	SettingUpdatePageName = "Setting Update"

	CaptchaPageName       = "CAPTCHA Verification"
	TimeoutPageName       = "Timeout"
	LogPageName           = "Activity Logs"
	StatusPageName        = "Status"
	ReviewerStatsPageName = "Reviewer Stats"

	GuildOwnerPageName    = "Guild Owner Menu"
	GuildScanPageName     = "Guild Scan Results"
	GuildLogsPageName     = "Guild Ban Logs"
	GuildLookupPageName   = "Guild User Lookup"
	GuildMessagesPageName = "Guild Messages"
)

// Selector Menu.
const (
	SelectorNewButtonCustomID  = "new_session"
	SelectorSelectMenuCustomID = "select_session"
)

// Dashboard Menu.
const (
	StartUserReviewButtonCustomID   = "start_user_review"
	StartGroupReviewButtonCustomID  = "start_group_review"
	UserSettingsButtonCustomID      = "user_settings"
	ActivityBrowserButtonCustomID   = "activity_browser"
	AdminMenuButtonCustomID         = "admin_menu"
	QueueManagementButtonCustomID   = "queue_management"
	WorkerStatusButtonCustomID      = "worker_status"
	LookupRobloxUserButtonCustomID  = "lookup_roblox_user" + ModalOpenSuffix
	LookupRobloxGroupButtonCustomID = "lookup_roblox_group" + ModalOpenSuffix
	LookupDiscordUserButtonCustomID = "lookup_discord_user" + ModalOpenSuffix
	ReviewerStatsButtonCustomID     = "reviewer_stats"

	LookupRobloxUserModalCustomID = "lookup_roblox_user_modal"
	LookupRobloxUserInputCustomID = "lookup_roblox_user_input"

	LookupRobloxGroupModalCustomID = "lookup_roblox_group_modal"
	LookupRobloxGroupInputCustomID = "lookup_roblox_group_input"

	LookupDiscordUserModalCustomID = "lookup_discord_user_modal"
	LookupDiscordUserInputCustomID = "lookup_discord_user_input"
)

// Consent Menu.
const (
	ConsentAcceptButtonCustomID = "consent_accept"
	ConsentRejectButtonCustomID = "consent_reject"

	TermsOfServiceText = `Before using Rotector, please read and accept our Terms of Service:

1. Purpose & Eligibility
   - Rotector is designed to help moderate Roblox content for child safety
   - You confirm that you are at least 18 years old
   - You agree to provide accurate information about your identity
   - You understand this is a serious moderation tool, not a game

2. User Responsibilities
   - You will use the bot responsibly and ethically
   - You will maintain strict confidentiality of sensitive information
   - You will not share access or information with unauthorized users
   - You will report bugs, security issues, and suspicious content immediately
   - You will make moderation decisions carefully and impartially
   - You will not use the bot for harassment or personal gain
   - You understand the critical nature of child safety moderation

3. Data Collection & Privacy
   - We collect and store Discord user IDs
   - We track all user actions for accountability
   - We may collect age verification information
   - We may share violation data with relevant authorities
   - You agree to our data collection and monitoring practices
   - You have the right to request deletion of your personal data
   - Some data may be retained if required by law or legitimate business purposes

4. Liability & Disclaimer
   - The bot is provided "as is" without warranty
   - We are not liable for any damages or losses
   - You accept full responsibility for your moderation decisions
   - You agree to indemnify us against claims arising from your actions
   - Technical issues may impact service availability

5. Termination & Enforcement
   - We reserve the right to terminate access at any time
   - Violations may result in permanent ban without notice
   - False age verification will result in immediate ban
   - Abuse of the system will result in permanent ban
   - We may report serious violations to relevant authorities

By clicking Accept, you:
- Confirm you are at least 18 years old
- Understand the serious responsibility of child safety moderation
- Accept all terms and conditions listed above
- Agree to be bound by these terms of service`
)

// Common Review Menu.
const (
	// ReviewLogsLimit caps the number of review history entries shown.
	ReviewLogsLimit = 5

	// MaxReviewHistorySize caps the number of review history entries shown.
	MaxReviewHistorySize = 10

	// ReviewFriendsLimit caps the number of friends shown in the main review container.
	ReviewFriendsLimit = 8

	// ReviewGroupsLimit caps the number of groups shown in the main review container.
	ReviewGroupsLimit = 8

	// ReviewGamesLimit caps the number of games shown in the main review container.
	ReviewGamesLimit = 6

	// ReviewOutfitsLimit caps the number of outfits shown in the main review container.
	ReviewOutfitsLimit = 6

	SortOrderSelectMenuCustomID = "sort_order"
	ReasonSelectMenuCustomID    = "reason_select"
	AIReasonSelectMenuCustomID  = "ai_reason_select"

	AddReasonModalCustomID           = "add_reason_modal"
	AddReasonInputCustomID           = "add_reason"
	AddReasonConfidenceInputCustomID = "add_reason_confidence"
	AddReasonEvidenceInputCustomID   = "add_reason_evidence"

	GenerateFriendReasonButtonCustomID  = "generate_friend_reason"
	GenerateGroupReasonButtonCustomID   = "generate_group_reason"
	GenerateProfileReasonButtonCustomID = "generate_profile_reason" + ModalOpenSuffix

	GenerateProfileReasonModalCustomID      = "generate_profile_reason_modal"
	ProfileReasonHintInputCustomID          = "profile_reason_hint_input"
	ProfileReasonFlaggedFieldsInputCustomID = "profile_reason_flagged_fields_input"
	ProfileReasonLanguageInputCustomID      = "profile_reason_language_input"
	ProfileReasonLanguageUsedInputCustomID  = "profile_reason_language_used_input"

	PrevReviewButtonCustomID = "prev_review"
	NextReviewButtonCustomID = "next_review"
	ConfirmButtonCustomID    = "confirm"
	ClearButtonCustomID      = "clear"
)

// Common Review Menu - Comment Menu.
const (
	CommentsPerPage = 5
	CommentLimit    = 25

	AddCommentButtonCustomID    = "add_comment" + ModalOpenSuffix
	DeleteCommentButtonCustomID = "delete_comment"

	AddCommentModalCustomID     = "add_comment_modal"
	CommentMessageInputCustomID = "comment_message_input"
)

// User Review Menu.
const (
	CaesarCipherButtonCustomID    = "caesar_cipher"
	ViewCommentsButtonCustomID    = "view_comments"
	ViewUserLogsButtonCustomID    = "view_user_logs"
	OpenOutfitsMenuButtonCustomID = "open_outfits_menu"
	OpenFriendsMenuButtonCustomID = "open_friends_menu"
	OpenGroupsMenuButtonCustomID  = "open_groups_menu"
	EditReasonButtonCustomID      = "edit_reason" + ModalOpenSuffix
	AbortButtonCustomID           = "abort"
)

// User Review Menu - Friends Viewer.
const (
	FriendsPerPage     = 20
	FriendsGridColumns = 4
	FriendsGridRows    = 5
)

// User Review Menu - Outfits Viewer.
const (
	OutfitsPerPage    = 20
	OutfitGridColumns = 4
	OutfitGridRows    = 5
)

// User Review Menu - Groups Viewer.
const (
	GroupsPerPage     = 6
	GroupsGridColumns = 3
	GroupsGridRows    = 2
)

// User Review Menu - Caesar Cipher Menu.
const (
	CaesarTotalTranslations   = 25
	CaesarTranslationsPerPage = 5
)

// Group Review Menu.
const (
	GroupViewMembersButtonCustomID = "group_view_members"
	GroupViewLogsButtonCustomID    = "group_view_logs"
	GroupDeleteButtonCustomID      = "group_delete"
)

// Group Review Menu - Members Viewer.
const (
	MembersPerPage     = 12
	MembersGridColumns = 3
	MembersGridRows    = 4
)

// Queue Menu.
const (
	QueueUserButtonCustomID = "queue_user" + ModalOpenSuffix
	QueueUserModalCustomID  = "queue_user_modal"
	QueueUserInputCustomID  = "queue_user_input"

	ManualUserReviewButtonCustomID = "manual_user_review" + ModalOpenSuffix
	ManualUserReviewModalCustomID  = "manual_user_review_modal"
	ManualUserReviewInputCustomID  = "manual_user_review_input"

	ManualGroupReviewButtonCustomID = "manual_group_review" + ModalOpenSuffix
	ManualGroupReviewModalCustomID  = "manual_group_review_modal"
	ManualGroupReviewInputCustomID  = "manual_group_review_input"

	ReviewQueuedUserButtonCustomID = "review_queued_user"
)

// User Settings.
const (
	UserSettingPrefix      = "user"
	UserSettingSelectID    = "user_setting_select"
	StreamerModeOption     = "streamer_mode"
	ReviewModeOption       = "review_mode"
	ReviewTargetModeOption = "review_target_mode"
)

// Bot Settings.
const (
	SettingsIDsPerPage = 15

	BotSettingPrefix          = "bot"
	BotSettingSelectID        = "bot_setting_select"
	ReviewerIDsOption         = "reviewer_ids"
	AdminIDsOption            = "admin_ids"
	SessionLimitOption        = "session_limit"
	WelcomeMessageOption      = "welcome_message"
	AnnouncementTypeOption    = "announcement_type"
	AnnouncementMessageOption = "announcement_message"
)

// Logs Menu.
const (
	LogsPerPage                         = 10
	LogsQueryInputCustomID              = "query_input"
	LogsQueryGuildIDOption              = "query_guild_id" + ModalOpenSuffix
	LogsQueryDiscordIDOption            = "query_discord_id" + ModalOpenSuffix
	LogsQueryUserIDOption               = "query_user_id" + ModalOpenSuffix
	LogsQueryGroupIDOption              = "query_group_id" + ModalOpenSuffix
	LogsQueryReviewerIDOption           = "query_reviewer_id" + ModalOpenSuffix
	LogsQueryDateRangeOption            = "query_date_range" + ModalOpenSuffix
	LogsQueryActivityTypeFilterCustomID = "activity_type_filter"
	ClearFiltersButtonCustomID          = "clear_filters"

	LogsUserActivityCategoryOption  = "category_user"
	LogsGroupActivityCategoryOption = "category_group"
	LogsOtherActivityCategoryOption = "category_other"
)

// CAPTCHA Menu.
const (
	CaptchaAnswerButtonCustomID  = "captcha_answer" + ModalOpenSuffix
	CaptchaRefreshButtonCustomID = "captcha_refresh"
	CaptchaAnswerModalCustomID   = "captcha_answer_modal"
	CaptchaAnswerInputCustomID   = "captcha_answer_input"
)

// Timeout Menu.
const (
	MaxReviewsBeforeBreak = 40               // Maximum reviews before requiring a break
	MinBreakDuration      = 10 * time.Minute // Minimum break duration
	ReviewSessionWindow   = 1 * time.Hour    // Window to track review count
)

// Admin Menu.
const (
	BotSettingsButtonCustomID = "bot_settings"
	DeleteUserButtonCustomID  = "delete_user" + ModalOpenSuffix
	DeleteGroupButtonCustomID = "delete_group" + ModalOpenSuffix

	DeleteUserModalCustomID  = "delete_user_modal"
	DeleteGroupModalCustomID = "delete_group_modal"

	DeleteUserInputCustomID  = "delete_user_input"
	DeleteGroupInputCustomID = "delete_group_input"
	AdminReasonInputCustomID = "admin_reason_input"

	ActionButtonCustomID = "delete_confirm"

	DeleteUserAction  = "delete_user"
	DeleteGroupAction = "delete_group"
)

// Reviewer Stats Menu.
const (
	ReviewerStatsPerPage                  = 5
	ReviewerStatsPeriodSelectMenuCustomID = "reviewer_stats_period"
)

// Guild Owner Menu.
const (
	GuildMembershipsPerPage = 5
	GuildScanUsersPerPage   = 6
	GuildMessagesPerPage    = 10
	GuildBanLogsPerPage     = 3

	GuildScanTypeCondo    = "condo"
	GuildScanTypeMessages = "messages"

	GuildOwnerMenuButtonCustomID   = "guild_owner_menu"
	StartGuildScanButtonCustomID   = "start_guild_scan"
	StartMessageScanButtonCustomID = "start_message_scan"
	ViewGuildBanLogsButtonCustomID = "view_guild_ban_logs"
	ConfirmGuildBansButtonCustomID = "confirm_guild_bans" + ModalOpenSuffix

	GuildBanConfirmModalCustomID = "guild_ban_confirm_modal"
	GuildBanReasonInputCustomID  = "guild_ban_reason_input"

	GuildScanFilterSelectMenuCustomID  = "guild_scan_filter_select"
	GuildScanMinGuildsOption           = "guild_scan_min_guilds" + ModalOpenSuffix
	GuildScanMinGuildsModalCustomID    = "guild_scan_min_guilds_modal"
	GuildScanMinGuildsInputCustomID    = "guild_scan_min_guilds_input"
	GuildScanJoinDurationOption        = "guild_scan_join_duration" + ModalOpenSuffix
	GuildScanJoinDurationModalCustomID = "guild_scan_join_duration_modal"
	GuildScanJoinDurationInputCustomID = "guild_scan_join_duration_input"
)
