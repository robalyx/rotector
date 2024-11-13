package worker

const (
	// FriendUsersToProcess sets how many users to process in each batch for the friend worker.
	FriendUsersToProcess = 100
	// GroupUsersToProcess sets how many users to process in each batch for the group worker.
	GroupUsersToProcess = 100
	// PurgeUsersToProcess sets how many users to process in each batch for the banned worker.
	PurgeUsersToProcess = 200
	// ClearedUsersToProcess sets how many users to process in each batch for the cleared worker.
	ClearedUsersToProcess = 200

	// FlaggedUsersThreshold is the maximum number of flagged users before pausing.
	FlaggedUsersThreshold = 1000
)
