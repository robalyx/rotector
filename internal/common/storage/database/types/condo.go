package types

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	ErrGameNotFound  = errors.New("game not found")
	ErrInvalidGameID = errors.New("invalid game ID")
)

// CondoGame represents information about a Roblox condo game.
type CondoGame struct {
	ID             uint64    `bun:",pk"                    json:"id"`
	UUID           uuid.UUID `bun:",notnull"               json:"uuid"`
	UniverseID     uint64    `bun:",notnull"               json:"universeId"`
	Name           string    `bun:",notnull"               json:"name"`
	Description    string    `bun:",notnull"               json:"description"`
	CreatorID      uint64    `bun:",notnull"               json:"creatorId"`
	CreatorName    string    `bun:",notnull"               json:"creatorName"`
	Visits         uint64    `bun:",notnull,default:0"     json:"visits"`
	Created        time.Time `bun:",notnull"               json:"created"`
	Updated        time.Time `bun:",notnull"               json:"updated"`
	FavoritedCount uint64    `bun:",notnull,default:0"     json:"favoritedCount"`
	MentionCount   uint64    `bun:",notnull,default:0"     json:"mentionCount"`
	LastScanned    time.Time `bun:",notnull"               json:"lastScanned"`
	LastUpdated    time.Time `bun:",notnull"               json:"lastUpdated"`
	IsDeleted      bool      `bun:",notnull,default:false" json:"isDeleted"`
}

// CondoPlayer represents a player found in condo games.
type CondoPlayer struct {
	ThumbnailURL  string    `bun:",pk"            json:"thumbnailUrl"`
	UserID        *uint64   `bun:",nullzero"      json:"userId"`
	GameIDs       []uint64  `bun:"game_ids,array" json:"gameIds"`
	IsBlacklisted bool      `bun:",notnull"       json:"isBlacklisted"`
	LastUpdated   time.Time `bun:",notnull"       json:"lastUpdated"`
}

// GameField represents available fields as bit flags.
type GameField uint32

const (
	GameFieldNone GameField = 0

	GameFieldID             GameField = 1 << iota // Game ID
	GameFieldUUID                                 // Game UUID
	GameFieldUniverseID                           // Universe ID
	GameFieldName                                 // Game name
	GameFieldDescription                          // Game description
	GameFieldCreator                              // Creator information
	GameFieldVisits                               // Visit count
	GameFieldCreatedTime                          // Creation time
	GameFieldUpdatedTime                          // Update time
	GameFieldFavoritedCount                       // Favorited count
	GameFieldMentionCount                         // Mention count
	GameFieldLastScanned                          // Last scan time
	GameFieldLastUpdated                          // Last update time
	GameFieldIsDeleted                            // Deletion status

	// GameFieldBasic includes all basic fields.
	GameFieldBasic = GameFieldID |
		GameFieldUUID |
		GameFieldName |
		GameFieldDescription |
		GameFieldCreator |
		GameFieldUniverseID

	// GameFieldTimestamps includes all timestamp-related fields.
	GameFieldTimestamps = GameFieldLastScanned |
		GameFieldLastUpdated |
		GameFieldCreatedTime |
		GameFieldUpdatedTime

	// GameFieldStats includes all statistical fields.
	GameFieldStats = GameFieldVisits |
		GameFieldMentionCount |
		GameFieldFavoritedCount

	// GameFieldAll includes all available fields.
	GameFieldAll = GameFieldBasic |
		GameFieldStats |
		GameFieldTimestamps |
		GameFieldIsDeleted
)

// gameFieldToColumns maps GameField bits to their corresponding database columns.
var gameFieldToColumns = map[GameField][]string{
	GameFieldID:             {"id"},
	GameFieldUUID:           {"uuid"},
	GameFieldUniverseID:     {"universe_id"},
	GameFieldName:           {"name"},
	GameFieldDescription:    {"description"},
	GameFieldCreator:        {"creator_id", "creator_name"},
	GameFieldVisits:         {"visits"},
	GameFieldCreatedTime:    {"created"},
	GameFieldUpdatedTime:    {"updated"},
	GameFieldFavoritedCount: {"favorited_count"},
	GameFieldMentionCount:   {"mention_count"},
	GameFieldLastScanned:    {"last_scanned"},
	GameFieldLastUpdated:    {"last_updated"},
	GameFieldIsDeleted:      {"is_deleted"},
}

// Columns returns the list of database columns to fetch based on the selected fields.
func (f GameField) Columns() []string {
	if f == GameFieldNone {
		return []string{"*"}
	}

	var columns []string
	for field, cols := range gameFieldToColumns {
		if f&field != 0 {
			columns = append(columns, cols...)
		}
	}
	return columns
}

// Add adds the specified fields to the current selection.
func (f GameField) Add(fields GameField) GameField {
	return f | fields
}

// Remove removes the specified fields from the current selection.
func (f GameField) Remove(fields GameField) GameField {
	return f &^ fields
}

// Has checks if all specified fields are included.
func (f GameField) Has(fields GameField) bool {
	return f&fields == fields
}
