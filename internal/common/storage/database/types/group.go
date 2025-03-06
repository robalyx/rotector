package types

import (
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jaxron/roapi.go/pkg/api/types"
	"github.com/robalyx/rotector/internal/common/storage/database/types/enum"
)

var (
	ErrGroupNotFound    = errors.New("group not found")
	ErrInvalidGroupID   = errors.New("invalid group ID")
	ErrNoGroupsToReview = errors.New("no groups available to review")
)

// Group combines all the information needed to review a group.
type Group struct {
	ID                  uint64                        `bun:",pk"                    json:"id"`
	UUID                uuid.UUID                     `bun:",notnull"               json:"uuid"`
	Name                string                        `bun:",notnull"               json:"name"`
	Description         string                        `bun:",notnull"               json:"description"`
	Owner               *types.GroupUser              `bun:"type:jsonb"             json:"owner"`
	Shout               *types.GroupShout             `bun:"type:jsonb"             json:"shout"`
	Reasons             Reasons[enum.GroupReasonType] `bun:"type:jsonb"             json:"reasons"`
	Confidence          float64                       `bun:",notnull"               json:"confidence"`
	LastScanned         time.Time                     `bun:",notnull"               json:"lastScanned"`
	LastUpdated         time.Time                     `bun:",notnull"               json:"lastUpdated"`
	LastViewed          time.Time                     `bun:",notnull"               json:"lastViewed"`
	LastLockCheck       time.Time                     `bun:",notnull"               json:"lastLockCheck"`
	IsLocked            bool                          `bun:",notnull,default:false" json:"isLocked"`
	IsDeleted           bool                          `bun:",notnull,default:false" json:"isDeleted"`
	ThumbnailURL        string                        `bun:",notnull"               json:"thumbnailUrl"`
	LastThumbnailUpdate time.Time                     `bun:",notnull"               json:"lastThumbnailUpdate"`
}

// FlaggedGroup extends Group to track groups that need review.
type FlaggedGroup struct {
	Group
}

// ConfirmedGroup extends Group to track groups that have been reviewed and confirmed.
type ConfirmedGroup struct {
	Group
	VerifiedAt time.Time `bun:",notnull" json:"verifiedAt"`
}

// ClearedGroup extends Group to track groups that were cleared during review.
// The ClearedAt field shows when the group was cleared by a moderator.
type ClearedGroup struct {
	Group
	ClearedAt time.Time `bun:",notnull" json:"clearedAt"`
}

// ReviewGroup combines all possible group states into a single structure for review.
type ReviewGroup struct {
	Group      `json:"group"`
	VerifiedAt time.Time      `json:"verifiedAt"`
	ClearedAt  time.Time      `json:"clearedAt"`
	Status     enum.GroupType `json:"status"`
	Reputation *Reputation    `json:"reputation"`
}

// GroupField represents available fields as bit flags.
type GroupField uint32

const (
	GroupFieldNone GroupField = 0

	GroupFieldID           GroupField = 1 << iota // Group ID
	GroupFieldUUID                                // Group UUID
	GroupFieldName                                // Group name
	GroupFieldDescription                         // Group description
	GroupFieldOwner                               // Owner information
	GroupFieldShout                               // Group shout
	GroupFieldReasons                             // Reasons for flagging
	GroupFieldThumbnail                           // ThumbnailURL
	GroupFieldFlaggedUsers                        // FlaggedUsers list

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
		GroupFieldDescription

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
		GroupFieldFlaggedUsers |
		GroupFieldConfidence |
		GroupFieldReputation |
		GroupFieldTimestamps |
		GroupFieldIsLocked |
		GroupFieldIsDeleted
)

// fieldToColumns maps GroupField bits to their corresponding database columns.
var groupFieldToColumns = map[GroupField][]string{ //nolint:gochecknoglobals // -
	GroupFieldID:                  {"id"},
	GroupFieldUUID:                {"uuid"},
	GroupFieldName:                {"name"},
	GroupFieldDescription:         {"description"},
	GroupFieldOwner:               {"owner"},
	GroupFieldShout:               {"shout"},
	GroupFieldReasons:             {"reasons"},
	GroupFieldThumbnail:           {"thumbnail_url"},
	GroupFieldFlaggedUsers:        {"flagged_users"},
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
