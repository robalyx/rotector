package types

import (
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jaxron/roapi.go/pkg/api/types"
	"github.com/robalyx/rotector/internal/database/types/enum"
)

var (
	ErrGroupNotFound    = errors.New("group not found")
	ErrInvalidGroupID   = errors.New("invalid group ID")
	ErrNoGroupsToReview = errors.New("no groups available to review")
)

// Group represents a group in any state (flagged, confirmed, or cleared).
type Group struct {
	ID                  uint64                        `bun:",pk"                    json:"id"`
	UUID                uuid.UUID                     `bun:",notnull"               json:"uuid"`
	Name                string                        `bun:",notnull"               json:"name"`
	Description         string                        `bun:",notnull"               json:"description"`
	Owner               *types.GroupUser              `bun:"type:jsonb"             json:"owner"`
	Shout               *types.GroupShout             `bun:"type:jsonb"             json:"shout"`
	Reasons             Reasons[enum.GroupReasonType] `bun:"type:jsonb,notnull"     json:"reasons"`
	Confidence          float64                       `bun:",notnull"               json:"confidence"`
	Status              enum.GroupType                `bun:",notnull"               json:"status"`
	LastScanned         time.Time                     `bun:",notnull"               json:"lastScanned"`
	LastUpdated         time.Time                     `bun:",notnull"               json:"lastUpdated"`
	LastViewed          time.Time                     `bun:",notnull"               json:"lastViewed"`
	LastLockCheck       time.Time                     `bun:",notnull"               json:"lastLockCheck"`
	IsLocked            bool                          `bun:",notnull,default:false" json:"isLocked"`
	IsDeleted           bool                          `bun:",notnull,default:false" json:"isDeleted"`
	ThumbnailURL        string                        `bun:",notnull"               json:"thumbnailUrl"`
	LastThumbnailUpdate time.Time                     `bun:",notnull"               json:"lastThumbnailUpdate"`
}

// GroupVerification stores verification data for confirmed groups.
type GroupVerification struct {
	GroupID    uint64    `bun:",pk"      json:"groupId"`
	ReviewerID uint64    `bun:",notnull" json:"reviewerId"`
	VerifiedAt time.Time `bun:",notnull" json:"verifiedAt"`
}

// GroupClearance stores clearance data for cleared groups.
type GroupClearance struct {
	GroupID    uint64    `bun:",pk"      json:"groupId"`
	ReviewerID uint64    `bun:",notnull" json:"reviewerId"`
	ClearedAt  time.Time `bun:",notnull" json:"clearedAt"`
}

// ReviewGroup combines group data with verification/clearance info for review.
type ReviewGroup struct {
	*Group
	ReviewerID uint64     `json:"reviewerId,omitempty"`
	VerifiedAt time.Time  `json:"verifiedAt"`
	ClearedAt  time.Time  `json:"clearedAt"`
	Reputation Reputation `json:"reputation"`
}

// GroupField represents available fields as bit flags.
type GroupField uint32

const (
	GroupFieldNone GroupField = 0

	GroupFieldID          GroupField = 1 << iota // Group ID
	GroupFieldUUID                               // Group UUID
	GroupFieldName                               // Group name
	GroupFieldDescription                        // Group description
	GroupFieldOwner                              // Owner information
	GroupFieldShout                              // Group shout
	GroupFieldStatus                             // Group status
	GroupFieldReasons                            // Reasons for flagging
	GroupFieldThumbnail                          // ThumbnailURL

	GroupFieldConfidence // AI confidence score

	GroupFieldReputation // Reputation fields (upvotes, downvotes, score)

	GroupFieldLastScanned         // Last scan time
	GroupFieldLastUpdated         // Last update time
	GroupFieldLastViewed          // Last view time
	GroupFieldLastLockCheck       // Last lock check time
	GroupFieldIsLocked            // Lock status
	GroupFieldIsDeleted           // Deletion status
	GroupFieldLastThumbnailUpdate // Last thumbnail update

	// GroupFieldBasic includes the essential group identification fields.
	GroupFieldBasic = GroupFieldID |
		GroupFieldUUID |
		GroupFieldName |
		GroupFieldDescription |
		GroupFieldStatus

	// GroupFieldTimestamps includes all timestamp-related fields.
	GroupFieldTimestamps = GroupFieldLastScanned |
		GroupFieldLastUpdated |
		GroupFieldLastViewed |
		GroupFieldLastLockCheck |
		GroupFieldLastThumbnailUpdate

	// GroupFieldAll includes all available fields.
	GroupFieldAll = GroupFieldBasic |
		GroupFieldOwner |
		GroupFieldShout |
		GroupFieldReasons |
		GroupFieldThumbnail |
		GroupFieldConfidence |
		GroupFieldReputation |
		GroupFieldTimestamps |
		GroupFieldIsLocked |
		GroupFieldIsDeleted
)

// fieldToColumns maps GroupField bits to their corresponding database columns.
var groupFieldToColumns = map[GroupField][]string{
	GroupFieldID:                  {"id"},
	GroupFieldUUID:                {"uuid"},
	GroupFieldName:                {"name"},
	GroupFieldDescription:         {"description"},
	GroupFieldOwner:               {"owner"},
	GroupFieldShout:               {"shout"},
	GroupFieldStatus:              {"status"},
	GroupFieldReasons:             {"reasons"},
	GroupFieldThumbnail:           {"thumbnail_url"},
	GroupFieldConfidence:          {"confidence"},
	GroupFieldLastScanned:         {"last_scanned"},
	GroupFieldLastUpdated:         {"last_updated"},
	GroupFieldLastViewed:          {"last_viewed"},
	GroupFieldLastLockCheck:       {"last_lock_check"},
	GroupFieldIsLocked:            {"is_locked"},
	GroupFieldIsDeleted:           {"is_deleted"},
	GroupFieldLastThumbnailUpdate: {"last_thumbnail_update"},
}

// HasReputation returns true if the reputation fields should be included.
func (f GroupField) HasReputation() bool {
	return f&GroupFieldReputation != 0
}

// Columns returns the list of database columns to fetch based on the selected fields.
func (f GroupField) Columns() []string {
	if f == GroupFieldNone {
		return []string{"*"}
	}

	var columns []string
	for field, cols := range groupFieldToColumns {
		if f&field != 0 {
			columns = append(columns, cols...)
		}
	}
	return columns
}

// Add adds the specified fields to the current selection.
func (f GroupField) Add(fields GroupField) GroupField {
	return f | fields
}

// Remove removes the specified fields from the current selection.
func (f GroupField) Remove(fields GroupField) GroupField {
	return f &^ fields
}

// Has checks if all specified fields are included.
func (f GroupField) Has(fields GroupField) bool {
	return f&fields == fields
}
