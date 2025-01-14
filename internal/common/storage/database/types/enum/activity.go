package enum

// ActivityType represents different kinds of user actions in the system.
//
//go:generate enumer -type=ActivityType -trimprefix=ActivityType
type ActivityType int

const (
	// ActivityTypeAll matches any activity type in database queries.
	ActivityTypeAll ActivityType = iota

	// ActivityTypeUserViewed tracks when a moderator reviews a user's profile.
	ActivityTypeUserViewed
	// ActivityTypeUserLookup tracks when a moderator looks up a user's profile.
	ActivityTypeUserLookup
	// ActivityTypeUserConfirmed tracks when a moderator confirms a user as inappropriate.
	ActivityTypeUserConfirmed
	// ActivityTypeUserConfirmedCustom tracks bans with custom moderator-provided reasons.
	ActivityTypeUserConfirmedCustom
	// ActivityTypeUserCleared tracks when a moderator marks a user as appropriate.
	ActivityTypeUserCleared
	// ActivityTypeUserSkipped tracks when a moderator skips reviewing a user.
	ActivityTypeUserSkipped
	// ActivityTypeUserRechecked tracks when a moderator requests an AI recheck.
	ActivityTypeUserRechecked
	// ActivityTypeUserTrainingUpvote tracks when a moderator upvotes a user in training mode.
	ActivityTypeUserTrainingUpvote
	// ActivityTypeUserTrainingDownvote tracks when a moderator downvotes a user in training mode.
	ActivityTypeUserTrainingDownvote
	// ActivityTypeUserDeleted tracks when an admin deletes a user from the database.
	ActivityTypeUserDeleted

	// ActivityTypeGroupViewed tracks when a moderator reviews a group's profile.
	ActivityTypeGroupViewed
	// ActivityTypeGroupLookup tracks when a moderator looks up a group's profile.
	ActivityTypeGroupLookup
	// ActivityTypeGroupConfirmed tracks when a moderator confirms a group as inappropriate.
	ActivityTypeGroupConfirmed
	// ActivityTypeGroupConfirmedCustom tracks bans with custom moderator-provided reasons.
	ActivityTypeGroupConfirmedCustom
	// ActivityTypeGroupCleared tracks when a moderator marks a group as appropriate.
	ActivityTypeGroupCleared
	// ActivityTypeGroupSkipped tracks when a moderator skips reviewing a group.
	ActivityTypeGroupSkipped
	// ActivityTypeGroupTrainingUpvote tracks when a moderator upvotes a group in training mode.
	ActivityTypeGroupTrainingUpvote
	// ActivityTypeGroupTrainingDownvote tracks when a moderator downvotes a group in training mode.
	ActivityTypeGroupTrainingDownvote
	// ActivityTypeGroupDeleted tracks when an admin deletes a group from the database.
	ActivityTypeGroupDeleted

	// ActivityTypeAppealSubmitted tracks when a moderator submits an appeal.
	ActivityTypeAppealSubmitted
	// ActivityTypeAppealSkipped tracks when a moderator skips reviewing an appeal.
	ActivityTypeAppealSkipped
	// ActivityTypeAppealAccepted tracks when a moderator accepts an appeal.
	ActivityTypeAppealAccepted
	// ActivityTypeAppealRejected tracks when a moderator rejects an appeal.
	ActivityTypeAppealRejected
	// ActivityTypeAppealClosed tracks when a user closes an appeal.
	ActivityTypeAppealClosed

	// ActivityTypeDiscordUserBanned tracks when a Discord user is banned.
	ActivityTypeDiscordUserBanned
	// ActivityTypeDiscordUserUnbanned tracks when a Discord user is unbanned.
	ActivityTypeDiscordUserUnbanned
)
