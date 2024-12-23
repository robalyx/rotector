package types

import (
	"time"

	"github.com/disgoorg/snowflake/v2"
)

// ChatMessageUsage keeps track of chat message usage within a 24-hour period.
type ChatMessageUsage struct {
	FirstMessageTime time.Time `bun:",nullzero,notnull"`
	MessageCount     int       `bun:",notnull"`
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
}

// BotSetting stores bot-wide configuration options.
type BotSetting struct {
	ID             uint64              `bun:",pk,autoincrement"`
	ReviewerIDs    []uint64            `bun:"reviewer_ids,type:bigint[]"`
	AdminIDs       []uint64            `bun:"admin_ids,type:bigint[]"`
	SessionLimit   uint64              `bun:",notnull"`
	WelcomeMessage string              `bun:",notnull,default:''"`
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
