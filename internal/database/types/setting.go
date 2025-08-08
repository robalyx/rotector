package types

import (
	"time"

	"github.com/disgoorg/snowflake/v2"
	"github.com/robalyx/rotector/internal/database/types/enum"
)

// ChatMessageUsage keeps track of chat message usage within a 24-hour period.
type ChatMessageUsage struct {
	FirstMessageTime time.Time `bun:",nullzero,notnull"`
	MessageCount     int       `bun:",notnull"`
}

// CaptchaUsage keeps track of reviews since last CAPTCHA verification.
type CaptchaUsage struct {
	CaptchaReviewCount int `bun:",notnull"`
}

// ReviewBreak stores information about the user's review activity.
type ReviewBreak struct {
	NextReviewTime  time.Time `bun:",notnull"`
	ReviewCount     int       `bun:",notnull"`
	WindowStartTime time.Time `bun:",notnull"`
}

// UserSetting stores user-specific preferences.
type UserSetting struct {
	UserID              snowflake.ID             `bun:",pk"`
	StreamerMode        bool                     `bun:",notnull"`
	UserDefaultSort     enum.ReviewSortBy        `bun:",notnull"`
	GroupDefaultSort    enum.ReviewSortBy        `bun:",notnull"`
	ChatModel           enum.ChatModel           `bun:",notnull"`
	ReviewMode          enum.ReviewMode          `bun:",notnull"`
	ReviewTargetMode    enum.ReviewTargetMode    `bun:",notnull"`
	ChatMessageUsage    ChatMessageUsage         `bun:",embed"`
	CaptchaUsage        CaptchaUsage             `bun:",embed"`
	ReviewBreak         ReviewBreak              `bun:",embed"`
	ReviewerStatsPeriod enum.ReviewerStatsPeriod `bun:",notnull"`
}

// Announcement stores the dashboard announcement configuration.
type Announcement struct {
	Type    enum.AnnouncementType `bun:"announcement_type,notnull"`
	Message string                `bun:"announcement_message,notnull,default:''"`
}

// BotSetting stores bot-wide configuration options.
type BotSetting struct {
	ID             uint64              `bun:",pk,autoincrement"`
	ReviewerIDs    []uint64            `bun:"reviewer_ids,type:bigint[]"`
	AdminIDs       []uint64            `bun:"admin_ids,type:bigint[]"`
	SessionLimit   uint64              `bun:",notnull"`
	WelcomeMessage string              `bun:",notnull,default:''"`
	Announcement   Announcement        `bun:",embed"`
	ReviewerMap    map[uint64]struct{} `bun:"-"` // In-memory map for O(1) lookups
	AdminMap       map[uint64]struct{} `bun:"-"` // In-memory map for O(1) lookups
	lastRefresh    time.Time           `bun:"-"` // In-memory cache control
}

// IsAdmin checks if the given user ID is in the admin list.
func (s *BotSetting) IsAdmin(userID uint64) bool {
	if s.AdminMap == nil || len(s.AdminIDs) != len(s.AdminMap) {
		s.AdminMap = make(map[uint64]struct{}, len(s.AdminIDs))
		for _, id := range s.AdminIDs {
			s.AdminMap[id] = struct{}{}
		}
	}

	_, exists := s.AdminMap[userID]

	return exists
}

// IsReviewer checks if the given user ID is in the reviewer list.
func (s *BotSetting) IsReviewer(userID uint64) bool {
	if s.ReviewerMap == nil || len(s.ReviewerIDs) != len(s.ReviewerMap) {
		s.ReviewerMap = make(map[uint64]struct{}, len(s.ReviewerIDs))
		for _, id := range s.ReviewerIDs {
			s.ReviewerMap[id] = struct{}{}
		}
	}

	_, exists := s.ReviewerMap[userID]

	return exists
}

// NeedsRefresh checks if the settings need to be refreshed.
func (s *BotSetting) NeedsRefresh() bool {
	return time.Since(s.lastRefresh) > 5*time.Minute
}

// UpdateRefreshTime updates the last refresh time to now.
func (s *BotSetting) UpdateRefreshTime() {
	s.lastRefresh = time.Now()
}

// SettingOption represents a single option for enum-type settings.
type SettingOption struct {
	Value       string `json:"value"`       // Internal value used by the system
	Label       string `json:"label"`       // Display name shown to users
	Description string `json:"description"` // Help text explaining the option
	Emoji       string `json:"emoji"`       // Optional emoji to display with the option
}
