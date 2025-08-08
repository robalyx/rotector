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
	// ActivityTypeRemoved1 is a deleted activity type.
	ActivityTypeRemoved1
	// ActivityTypeRemoved2 is a deleted activity type.
	ActivityTypeRemoved2
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
	// ActivityTypeRemoved3 is a deleted activity type.
	ActivityTypeRemoved3
	// ActivityTypeRemoved4 is a deleted activity type.
	ActivityTypeRemoved4
	// ActivityTypeGroupDeleted tracks when an admin deletes a group from the database.
	ActivityTypeGroupDeleted

	// ActivityTypeUserLookupDiscord tracks when a moderator looks up a Discord user's profile.
	ActivityTypeUserLookupDiscord

	ActivityTypeAppealSubmitted // Deprecated: kept for backwards compatibility
	ActivityTypeAppealClaimed   // Deprecated: kept for backwards compatibility
	ActivityTypeAppealAccepted  // Deprecated: kept for backwards compatibility
	ActivityTypeAppealRejected  // Deprecated: kept for backwards compatibility
	ActivityTypeAppealClosed    // Deprecated: kept for backwards compatibility
	ActivityTypeAppealReopened  // Deprecated: kept for backwards compatibility

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

	// LOGS ADDED AFTER RELEASE BELOW.

	// ActivityTypeGroupQueued tracks when a group is queued for review.
	ActivityTypeGroupQueued
)
