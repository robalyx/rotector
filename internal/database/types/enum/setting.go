package enum

// SettingType represents the data type of a setting.
//
//go:generate go tool enumer -type=SettingType -trimprefix=SettingType
type SettingType int

const (
	SettingTypeBool SettingType = iota
	SettingTypeEnum
	SettingTypeID
	SettingTypeNumber
	SettingTypeText
)

// AnnouncementType is the type of announcement message.
//
//go:generate go tool enumer -type=AnnouncementType -trimprefix=AnnouncementType
type AnnouncementType int

const (
	AnnouncementTypeNone AnnouncementType = iota
	AnnouncementTypeInfo
	AnnouncementTypeWarning
	AnnouncementTypeSuccess
	AnnouncementTypeError
	AnnouncementTypeMaintenance
)
