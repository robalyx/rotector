package types

import (
	"time"
)

// ActivityType represents different kinds of user actions in the system.
//
//go:generate stringer -type=ActivityType -linecomment
type ActivityType int

const (
	// ActivityTypeAll matches any activity type in database queries.
	ActivityTypeAll ActivityType = iota // ALL

	// ActivityTypeUserViewed tracks when a moderator reviews a user's profile.
	ActivityTypeUserViewed // USER_VIEWED
	// ActivityTypeUserLookup tracks when a moderator looks up a user's profile.
	ActivityTypeUserLookup // USER_LOOKUP
	// ActivityTypeUserConfirmed tracks when a moderator confirms a user as inappropriate.
	ActivityTypeUserConfirmed // USER_CONFIRMED
	// ActivityTypeUserConfirmedCustom tracks bans with custom moderator-provided reasons.
	ActivityTypeUserConfirmedCustom // USER_CONFIRMED_CUSTOM
	// ActivityTypeUserCleared tracks when a moderator marks a user as appropriate.
	ActivityTypeUserCleared // USER_CLEARED
	// ActivityTypeUserSkipped tracks when a moderator skips reviewing a user.
	ActivityTypeUserSkipped // USER_SKIPPED
	// ActivityTypeUserRechecked tracks when a moderator requests an AI recheck.
	ActivityTypeUserRechecked // USER_RECHECKED
	// ActivityTypeUserTrainingUpvote tracks when a moderator upvotes a user in training mode.
	ActivityTypeUserTrainingUpvote // USER_TRAINING_UPVOTE
	// ActivityTypeUserTrainingDownvote tracks when a moderator downvotes a user in training mode.
	ActivityTypeUserTrainingDownvote // USER_TRAINING_DOWNVOTE
	// ActivityTypeUserDeleted tracks when an admin deletes a user from the database.
	ActivityTypeUserDeleted // USER_DELETED

	// ActivityTypeGroupViewed tracks when a moderator reviews a group's profile.
	ActivityTypeGroupViewed // GROUP_VIEWED
	// ActivityTypeGroupLookup tracks when a moderator looks up a group's profile.
	ActivityTypeGroupLookup // GROUP_LOOKUP
	// ActivityTypeGroupConfirmed tracks when a moderator confirms a group as inappropriate.
	ActivityTypeGroupConfirmed // GROUP_CONFIRMED
	// ActivityTypeGroupConfirmedCustom tracks bans with custom moderator-provided reasons.
	ActivityTypeGroupConfirmedCustom // GROUP_CONFIRMED_CUSTOM
	// ActivityTypeGroupCleared tracks when a moderator marks a group as appropriate.
	ActivityTypeGroupCleared // GROUP_CLEARED
	// ActivityTypeGroupSkipped tracks when a moderator skips reviewing a group.
	ActivityTypeGroupSkipped // GROUP_SKIPPED
	// ActivityTypeGroupTrainingUpvote tracks when a moderator upvotes a group in training mode.
	ActivityTypeGroupTrainingUpvote // GROUP_TRAINING_UPVOTE
	// ActivityTypeGroupTrainingDownvote tracks when a moderator downvotes a group in training mode.
	ActivityTypeGroupTrainingDownvote // GROUP_TRAINING_DOWNVOTE
	// ActivityTypeGroupDeleted tracks when an admin deletes a group from the database.
	ActivityTypeGroupDeleted // GROUP_DELETED

	// ActivityTypeAppealSubmitted tracks when a moderator submits an appeal.
	ActivityTypeAppealSubmitted // APPEAL_SUBMITTED
	// ActivityTypeAppealSkipped tracks when a moderator skips reviewing an appeal.
	ActivityTypeAppealSkipped // APPEAL_SKIPPED
	// ActivityTypeAppealAccepted tracks when a moderator accepts an appeal.
	ActivityTypeAppealAccepted // APPEAL_ACCEPTED
	// ActivityTypeAppealRejected tracks when a moderator rejects an appeal.
	ActivityTypeAppealRejected // APPEAL_REJECTED
	// ActivityTypeAppealClosed tracks when a user closes an appeal.
	ActivityTypeAppealClosed // APPEAL_CLOSED

	// ActivityTypeDiscordUserBanned tracks when a Discord user is banned.
	ActivityTypeDiscordUserBanned // DISCORD_USER_BANNED
	// ActivityTypeDiscordUserUnbanned tracks when a Discord user is unbanned.
	ActivityTypeDiscordUserUnbanned // DISCORD_USER_UNBANNED
)

// ActivityTarget identifies the target of an activity log entry.
// Only one of the fields should be set.
type ActivityTarget struct {
	DiscordID uint64 `bun:",nullzero"`
	UserID    uint64 `bun:",nullzero"`
	GroupID   uint64 `bun:",nullzero"`
}

// ActivityFilter is used to provide a filter criteria for retrieving activity logs.
type ActivityFilter struct {
	DiscordID    uint64
	UserID       uint64
	GroupID      uint64
	ReviewerID   uint64
	ActivityType ActivityType
	StartDate    time.Time
	EndDate      time.Time
}

// LogCursor represents a pagination cursor for activity logs.
type LogCursor struct {
	Timestamp time.Time
	Sequence  int64
}

// ActivityLog stores information about moderator actions.
type ActivityLog struct {
	Sequence          int64                  `bun:",pk,autoincrement"`
	ReviewerID        uint64                 `bun:",notnull"`
	ActivityTarget    ActivityTarget         `bun:",embed"`
	ActivityType      ActivityType           `bun:",notnull"`
	ActivityTimestamp time.Time              `bun:",notnull,pk"`
	Details           map[string]interface{} `bun:"type:jsonb"`
}
