package types

import (
	"fmt"
	"time"

	"github.com/disgoorg/snowflake/v2"
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
	if !bot.IsReviewer(uint64(user.UserID)) && user.ReviewMode == TrainingReviewMode {
		c.ReviewCount++
	}
}

// ResetReviews resets the review counter.
func (c *CaptchaUsage) ResetReviews() {
	c.ReviewCount = 0
}

// UserSetting stores user-specific preferences.
type UserSetting struct {
	UserID             snowflake.ID     `bun:",pk"`
	StreamerMode       bool             `bun:",notnull"`
	UserDefaultSort    ReviewSortBy     `bun:",notnull"`
	GroupDefaultSort   ReviewSortBy     `bun:",notnull"`
	AppealDefaultSort  AppealSortBy     `bun:",notnull"`
	AppealStatusFilter AppealFilterBy   `bun:",notnull"`
	ChatModel          ChatModel        `bun:",notnull"`
	ReviewMode         ReviewMode       `bun:",notnull"`
	ReviewTargetMode   ReviewTargetMode `bun:",notnull"`
	ChatMessageUsage   ChatMessageUsage `bun:",embed"`
	SkipUsage          SkipUsage        `bun:",embed"`
	CaptchaUsage       CaptchaUsage     `bun:",embed"`
}

// AnnouncementType is the type of announcement message.
type AnnouncementType string

const (
	AnnouncementTypeNone    AnnouncementType = "none"
	AnnouncementTypeInfo    AnnouncementType = "info"
	AnnouncementTypeWarning AnnouncementType = "warning"
	AnnouncementTypeSuccess AnnouncementType = "success"
	AnnouncementTypeError   AnnouncementType = "error"
)

// Announcement stores the dashboard announcement configuration.
type Announcement struct {
	Type    AnnouncementType `bun:"announcement_type,notnull,default:'none'"`
	Message string           `bun:"announcement_message,notnull,default:''"`
}

// BotSetting stores bot-wide configuration options.
type BotSetting struct {
	ID             uint64              `bun:",pk,autoincrement"`
	ReviewerIDs    []uint64            `bun:"reviewer_ids,type:bigint[]"`
	AdminIDs       []uint64            `bun:"admin_ids,type:bigint[]"`
	SessionLimit   uint64              `bun:",notnull"`
	WelcomeMessage string              `bun:",notnull,default:''"`
	Announcement   Announcement        `bun:",embed"`
	reviewerMap    map[uint64]struct{} // In-memory map for O(1) lookups
	adminMap       map[uint64]struct{} // In-memory map for O(1) lookups
}

// IsAdmin checks if the given user ID is in the admin list.
func (s *BotSetting) IsAdmin(userID uint64) bool {
	if s.adminMap == nil {
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
	if s.reviewerMap == nil {
		s.reviewerMap = make(map[uint64]struct{}, len(s.ReviewerIDs))
		for _, id := range s.ReviewerIDs {
			s.reviewerMap[id] = struct{}{}
		}
	}

	_, exists := s.reviewerMap[userID]
	return exists
}

// SettingType represents the data type of a setting.
type SettingType string

// Available setting types.
const (
	SettingTypeBool   SettingType = "bool"
	SettingTypeEnum   SettingType = "enum"
	SettingTypeID     SettingType = "id"
	SettingTypeNumber SettingType = "number"
	SettingTypeText   SettingType = "text"
)

// SettingOption represents a single option for enum-type settings.
type SettingOption struct {
	Value       string `json:"value"`       // Internal value used by the system
	Label       string `json:"label"`       // Display name shown to users
	Description string `json:"description"` // Help text explaining the option
	Emoji       string `json:"emoji"`       // Optional emoji to display with the option
}
