package enum

// ActivityType represents different kinds of user actions in the system.
//
//go:generate go tool enumer -type=ActivityType -trimprefix=ActivityType
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
	// ActivityTypeUserCleared tracks when a moderator marks a user as appropriate.
	ActivityTypeUserCleared
	// ActivityTypeUserSkipped tracks when a moderator skips reviewing a user.
	ActivityTypeUserSkipped
	// ActivityTypeUserQueued tracks when a moderator queues a user for review.
	ActivityTypeUserQueued
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

	// ActivityTypeUserLookupDiscord tracks when a moderator looks up a Discord user's profile.
	ActivityTypeUserLookupDiscord

	// ActivityTypeAppealSubmitted tracks when a moderator submits an appeal.
	ActivityTypeAppealSubmitted
	// ActivityTypeAppealClaimed tracks when a moderator claims an appeal.
	ActivityTypeAppealClaimed
	// ActivityTypeAppealAccepted tracks when a moderator accepts an appeal.
	ActivityTypeAppealAccepted
	// ActivityTypeAppealRejected tracks when a moderator rejects an appeal.
	ActivityTypeAppealRejected
	// ActivityTypeAppealClosed tracks when a user closes an appeal.
	ActivityTypeAppealClosed
	// ActivityTypeAppealReopened tracks when a user reopens an appeal.
	ActivityTypeAppealReopened
	// ActivityTypeUserDataDeleted tracks when user data is deleted.
	ActivityTypeUserDataDeleted
	// ActivityTypeUserBlacklisted tracks when a user is blacklisted from submitting appeals.
	ActivityTypeUserBlacklisted

	// ActivityTypeDiscordUserBanned tracks when a Discord user is banned.
	ActivityTypeDiscordUserBanned
	// ActivityTypeDiscordUserUnbanned tracks when a Discord user is unbanned.
	ActivityTypeDiscordUserUnbanned

	// ActivityTypeBotSettingUpdated tracks when a bot setting is updated.
	ActivityTypeBotSettingUpdated

	// ActivityTypeGuildBans tracks when a guild bans a set of users.
	ActivityTypeGuildBans
)
