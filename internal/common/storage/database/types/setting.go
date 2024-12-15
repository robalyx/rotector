package types

// UserSetting stores user-specific preferences.
type UserSetting struct {
	UserID           uint64           `bun:",pk"`
	StreamerMode     bool             `bun:",notnull"`
	UserDefaultSort  SortBy           `bun:",notnull"`
	GroupDefaultSort SortBy           `bun:",notnull"`
	ChatModel        ChatModel        `bun:",notnull"`
	ReviewMode       ReviewMode       `bun:",notnull"`
	ReviewTargetMode ReviewTargetMode `bun:",notnull"`
}

// BotSetting stores bot-wide configuration options.
type BotSetting struct {
	ID             uint64   `bun:",pk,autoincrement"`
	ReviewerIDs    []uint64 `bun:"reviewer_ids,type:bigint[]"`
	AdminIDs       []uint64 `bun:"admin_ids,type:bigint[]"`
	SessionLimit   uint64   `bun:",notnull"`
	WelcomeMessage string   `bun:",notnull,default:''"`
}

// IsAdmin checks if the given user ID is in the admin list.
func (s *BotSetting) IsAdmin(userID uint64) bool {
	for _, adminID := range s.AdminIDs {
		if adminID == userID {
			return true
		}
	}
	return false
}

// IsReviewer checks if the given user ID is in the reviewer list.
func (s *BotSetting) IsReviewer(userID uint64) bool {
	for _, reviewerID := range s.ReviewerIDs {
		if reviewerID == userID {
			return true
		}
	}
	return false
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
