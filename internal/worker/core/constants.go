package core

const (
	// FriendUsersToProcess sets how many users to process in each batch for the friend worker.
	FriendUsersToProcess = 100
	// PurgeUsersToProcess sets how many users to process in each batch for the banned worker.
	PurgeUsersToProcess = 200

	// GroupUsersToProcess sets how many users to process in each batch for the group worker.
	GroupUsersToProcess = 100
	// PurgeGroupsToProcess sets how many groups to process in each batch for the locked worker.
	PurgeGroupsToProcess = 100

	// FlaggedUsersThreshold is the maximum number of flagged users before pausing.
	FlaggedUsersThreshold = 1000

	// MinFlaggedGroupUsersForFlag sets the threshold for how many flagged users
	// must be found before flagging a group.
	MinFlaggedGroupUsersForFlag = 50
)
