package types

import (
	"time"

	"github.com/disgoorg/snowflake/v2"
	"github.com/robalyx/rotector/internal/common/storage/database/types/enum"
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

// ReviewBreak stores information about the session review.
type ReviewBreak struct {
	NextReviewTime   time.Time `bun:",notnull"`
	SessionReviews   int       `bun:",notnull"`
	SessionStartTime time.Time `bun:",notnull"`
}

// UserSetting stores user-specific preferences.
type UserSetting struct {
	UserID              snowflake.ID             `bun:",pk"`
	StreamerMode        bool                     `bun:",notnull"`
	UserDefaultSort     enum.ReviewSortBy        `bun:",notnull"`
	GroupDefaultSort    enum.ReviewSortBy        `bun:",notnull"`
	AppealDefaultSort   enum.AppealSortBy        `bun:",notnull"`
	AppealStatusFilter  enum.AppealStatus        `bun:",notnull"`
	ChatModel           enum.ChatModel           `bun:",notnull"`
	ReviewMode          enum.ReviewMode          `bun:",notnull"`
	ReviewTargetMode    enum.ReviewTargetMode    `bun:",notnull"`
	ChatMessageUsage    ChatMessageUsage         `bun:",embed"`
	CaptchaUsage        CaptchaUsage             `bun:",embed"`
	ReviewBreak         ReviewBreak              `bun:",embed"`
	LeaderboardPeriod   enum.LeaderboardPeriod   `bun:",notnull"`
	ReviewerStatsPeriod enum.ReviewerStatsPeriod `bun:",notnull"`
}

// Announcement stores the dashboard announcement configuration.
type Announcement struct {
	Type    enum.AnnouncementType `bun:"announcement_type,notnull"`
	Message string                `bun:"announcement_message,notnull,default:''"`
}

// APIKeyInfo stores information about an API key
type APIKeyInfo struct {
	Key         string    `json:"key"`         // The API key
	Description string    `json:"description"` // Description of what the key is used for
	CreatedAt   time.Time `json:"createdAt"`   // When the key was created
}

// BotSetting stores bot-wide configuration options.
type BotSetting struct {
	ID             uint64                 `bun:",pk,autoincrement"`
	ReviewerIDs    []uint64               `bun:"reviewer_ids,type:bigint[]"`
	AdminIDs       []uint64               `bun:"admin_ids,type:bigint[]"`
	SessionLimit   uint64                 `bun:",notnull"`
	WelcomeMessage string                 `bun:",notnull,default:''"`
	Announcement   Announcement           `bun:",embed"`
	APIKeys        []APIKeyInfo           `bun:"api_keys,type:jsonb"`
	reviewerMap    map[uint64]struct{}    // In-memory map for O(1) lookups
	adminMap       map[uint64]struct{}    // In-memory map for O(1) lookups
	apiKeyMap      map[string]*APIKeyInfo // In-memory map for O(1) lookups
	lastRefresh    time.Time
}

// IsAdmin checks if the given user ID is in the admin list.
func (s *BotSetting) IsAdmin(userID uint64) bool {
	if s.adminMap == nil || len(s.AdminIDs) != len(s.adminMap) {
		s.adminMap = make(map[uint64]struct{}, len(s.AdminIDs))
		for _, id := range s.AdminIDs {
			s.adminMap[id] = struct{}{}
		}
	}

	_, exists := s.adminMap[userID]
	return exists
}

// IsReviewer checks if the given user ID is in the reviewer list.
func (s *BotSetting) IsReviewer(userID uint64) bool {
	if s.reviewerMap == nil || len(s.ReviewerIDs) != len(s.reviewerMap) {
		s.reviewerMap = make(map[uint64]struct{}, len(s.ReviewerIDs))
		for _, id := range s.ReviewerIDs {
			s.reviewerMap[id] = struct{}{}
		}
	}

	_, exists := s.reviewerMap[userID]
	return exists
}

// IsAPIKey checks if the given key is valid.
func (s *BotSetting) IsAPIKey(key string) (*APIKeyInfo, bool) {
	if s.apiKeyMap == nil || len(s.APIKeys) != len(s.apiKeyMap) {
		s.apiKeyMap = make(map[string]*APIKeyInfo, len(s.APIKeys))
		for i := range s.APIKeys {
			s.apiKeyMap[s.APIKeys[i].Key] = &s.APIKeys[i]
		}
	}

	info, exists := s.apiKeyMap[key]
	return info, exists
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
