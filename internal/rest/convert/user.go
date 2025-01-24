package convert

import (
	apiTypes "github.com/jaxron/roapi.go/pkg/api/types"
	"github.com/robalyx/rotector/internal/common/storage/database/types"
	"github.com/robalyx/rotector/internal/common/storage/database/types/enum"
	restTypes "github.com/robalyx/rotector/internal/rest/types"
)

// UserStatus converts a database user status to REST API user status.
func UserStatus(status enum.UserType) restTypes.UserStatus {
	switch status {
	case enum.UserTypeFlagged:
		return restTypes.UserStatusFlagged
	case enum.UserTypeConfirmed:
		return restTypes.UserStatusConfirmed
	case enum.UserTypeCleared:
		return restTypes.UserStatusCleared
	case enum.UserTypeUnflagged:
		return restTypes.UserStatusUnflagged
	default:
		return restTypes.UserStatusUnflagged
	}
}

// User converts a database user to REST API user.
func User(user *types.ReviewUser) *restTypes.User {
	if user == nil {
		return nil
	}

	return &restTypes.User{
		ID:             user.ID,
		Name:           user.Name,
		DisplayName:    user.DisplayName,
		Description:    user.Description,
		CreatedAt:      user.CreatedAt,
		Reason:         user.Reason,
		Groups:         UserGroups(user.Groups),
		Friends:        Friends(user.Friends),
		Games:          Games(user.Games),
		FlaggedContent: user.FlaggedContent,
		FollowerCount:  user.FollowerCount,
		FollowingCount: user.FollowingCount,
		Confidence:     user.Confidence,
		LastScanned:    user.LastScanned,
		LastUpdated:    user.LastUpdated,
		LastViewed:     user.LastViewed,
		ThumbnailURL:   user.ThumbnailURL,
		Upvotes:        user.Reputation.Upvotes,
		Downvotes:      user.Reputation.Downvotes,
		Reputation:     user.Reputation.Score,
	}
}

// UserGroups converts a slice of API user group roles to REST API user groups.
func UserGroups(groups []*apiTypes.UserGroupRoles) []restTypes.UserGroup {
	result := make([]restTypes.UserGroup, len(groups))
	for i, g := range groups {
		result[i] = restTypes.UserGroup{
			ID:   g.Group.ID,
			Name: g.Group.Name,
			Role: g.Role.Name,
		}
	}
	return result
}

// Friends converts a slice of database extended friends to REST API friends.
func Friends(friends []*apiTypes.ExtendedFriend) []restTypes.Friend {
	result := make([]restTypes.Friend, len(friends))
	for i, f := range friends {
		result[i] = restTypes.Friend{
			ID:          f.ID,
			Name:        f.Name,
			DisplayName: f.DisplayName,
		}
	}
	return result
}

// Games converts a slice of API games to REST API games.
func Games(games []*apiTypes.Game) []restTypes.Game {
	result := make([]restTypes.Game, len(games))
	for i, g := range games {
		result[i] = restTypes.Game{
			ID:   g.ID,
			Name: g.Name,
		}
	}
	return result
}
