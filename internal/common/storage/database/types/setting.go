package types

import (
	"fmt"
	"time"

	"github.com/disgoorg/snowflake/v2"
	"github.com/robalyx/rotector/internal/common/storage/database/types/enum"
)

// ChatMessageUsage keeps track of chat message usage within a 24-hour period.
type ChatMessageUsage struct {
	FirstMessageTime time.Time `bun:",nullzero,notnull"`
	MessageCount     int       `bun:",notnull"`
}

// SkipUsage keeps track of skip usage and limits.
type SkipUsage struct {
	LastSkipTime     time.Time `bun:",nullzero,notnull"`
	ConsecutiveSkips int       `bun:",notnull"`
}

// CanSkip checks if skipping is allowed based on limits and cooldown.
// Returns a non-empty message if skipping is not allowed.
func (s *SkipUsage) CanSkip() string {
	if s.ConsecutiveSkips >= 3 {
		return "You have reached the maximum number of consecutive skips. Please use an action."
	}

	if !s.LastSkipTime.IsZero() && time.Since(s.LastSkipTime) < 30*time.Second {
		remainingTime := 30*time.Second - time.Since(s.LastSkipTime)
		return fmt.Sprintf("You are on skip cooldown. Please wait %d seconds.", int(remainingTime.Seconds()))
	}

	return ""
}

// IncrementSkips updates the skip tracking information.
func (s *SkipUsage) IncrementSkips() {
	s.LastSkipTime = time.Now()
	s.ConsecutiveSkips++
}

// ResetSkips resets the consecutive skips counter.
func (s *SkipUsage) ResetSkips() {
	s.ConsecutiveSkips = 0
}

// CaptchaUsage keeps track of reviews since last CAPTCHA verification.
type CaptchaUsage struct {
	ReviewCount int `bun:",notnull"`
}

// NeedsCaptcha checks if CAPTCHA verification is needed.
// Returns true if the user has reached the review limit.
func (c *CaptchaUsage) NeedsCaptcha() bool {
	return c.ReviewCount >= 10
}

// IncrementReviews increments the review counter.
func (c *CaptchaUsage) IncrementReviews(user *UserSetting, bot *BotSetting) {
	if !bot.IsReviewer(uint64(user.UserID)) && user.ReviewMode == enum.ReviewModeTraining {
		c.ReviewCount++
	}
}

// ResetReviews resets the review counter.
func (c *CaptchaUsage) ResetReviews() {
	c.ReviewCount = 0
}

// UserSetting stores user-specific preferences.
type UserSetting struct {
	UserID             snowflake.ID           `bun:",pk"`
	StreamerMode       bool                   `bun:",notnull"`
	UserDefaultSort    enum.ReviewSortBy      `bun:",notnull"`
	GroupDefaultSort   enum.ReviewSortBy      `bun:",notnull"`
	AppealDefaultSort  enum.AppealSortBy      `bun:",notnull"`
	AppealStatusFilter enum.AppealStatus      `bun:",notnull"`
	ChatModel          enum.ChatModel         `bun:",notnull"`
	ReviewMode         enum.ReviewMode        `bun:",notnull"`
	ReviewTargetMode   enum.ReviewTargetMode  `bun:",notnull"`
	ChatMessageUsage   ChatMessageUsage       `bun:",embed"`
	SkipUsage          SkipUsage              `bun:",embed"`
	CaptchaUsage       CaptchaUsage           `bun:",embed"`
	LeaderboardPeriod  enum.LeaderboardPeriod `bun:",notnull"`
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
