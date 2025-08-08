package types

import "time"

// IvanMessage represents a message from the ivan_messages table.
type IvanMessage struct {
	ID         int64     `bun:",pk,autoincrement" json:"id"`
	DateTime   time.Time `bun:",notnull"          json:"dateTime"`
	UserID     int64     `bun:",notnull"          json:"userId"`
	Username   string    `bun:",notnull"          json:"username"`
	Message    string    `bun:",notnull"          json:"message"`
	WasChecked bool      `bun:",notnull"          json:"-"`
}
