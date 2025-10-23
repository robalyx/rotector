package enum

// UserCategoryType represents the primary violation category for a flagged user.
//
//go:generate go tool enumer -type=UserCategoryType -trimprefix=UserCategoryType
type UserCategoryType int

const (
	// UserCategoryTypePredatory indicates grooming, seeking private interactions, or targeting specific groups.
	UserCategoryTypePredatory UserCategoryType = iota
	// UserCategoryTypeCSAM indicates CSAM trading/distribution or child exploitation material references.
	UserCategoryTypeCSAM
	// UserCategoryTypeSexual indicates direct sexual content, ERP, or adult-themed violations without active targeting.
	UserCategoryTypeSexual
	// UserCategoryTypeKink indicates fetish content including BDSM, body modification, or other non-racial fetishes.
	UserCategoryTypeKink
	// UserCategoryTypeRaceplay indicates racial fetish content or stereotypes.
	UserCategoryTypeRaceplay
	// UserCategoryTypeCondo indicates condo game references or platform abuse for content access (excluding CSAM).
	UserCategoryTypeCondo
	// UserCategoryTypeOther indicates miscellaneous violations not fitting other categories.
	UserCategoryTypeOther
)
