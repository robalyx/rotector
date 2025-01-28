package enum

// SettingType represents the data type of a setting.
//
//go:generate enumer -type=SettingType -trimprefix=SettingType
type SettingType int

const (
	SettingTypeBool SettingType = iota
	SettingTypeEnum
	SettingTypeID
	SettingTypeNumber
	SettingTypeText
	SettingTypeAPIKey
)

// AnnouncementType is the type of announcement message.
//
//go:generate enumer -type=AnnouncementType -trimprefix=AnnouncementType
type AnnouncementType int

const (
	AnnouncementTypeNone AnnouncementType = iota
	AnnouncementTypeInfo
	AnnouncementTypeWarning
	AnnouncementTypeSuccess
	AnnouncementTypeError
)
