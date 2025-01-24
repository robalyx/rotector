package convert

import (
	"time"

	apiTypes "github.com/jaxron/roapi.go/pkg/api/types"
	"github.com/robalyx/rotector/internal/common/storage/database/types"
	"github.com/robalyx/rotector/internal/common/storage/database/types/enum"
	"github.com/robalyx/rotector/internal/rpc/proto"
)

// UserStatus converts a database user status to RPC API user status.
func UserStatus(status enum.UserType) proto.UserStatus {
	switch status {
	case enum.UserTypeFlagged:
		return proto.UserStatus_USER_STATUS_FLAGGED
	case enum.UserTypeConfirmed:
		return proto.UserStatus_USER_STATUS_CONFIRMED
	case enum.UserTypeCleared:
		return proto.UserStatus_USER_STATUS_CLEARED
	case enum.UserTypeUnflagged:
		return proto.UserStatus_USER_STATUS_UNFLAGGED
	default:
		return proto.UserStatus_USER_STATUS_UNFLAGGED
	}
}

// User converts a database user to RPC API user message.
func User(user *types.ReviewUser) *proto.User {
	if user == nil {
		return nil
	}

	return &proto.User{
		Id:             user.ID,
		Name:           user.Name,
		DisplayName:    user.DisplayName,
		Description:    user.Description,
		CreatedAt:      user.CreatedAt.Format(time.RFC3339),
		Reason:         user.Reason,
		Groups:         UserGroups(user.Groups),
		Friends:        Friends(user.Friends),
		Games:          Games(user.Games),
		FlaggedContent: user.FlaggedContent,
		FollowerCount:  user.FollowerCount,
		FollowingCount: user.FollowingCount,
		Confidence:     user.Confidence,
		LastScanned:    user.LastScanned.Format(time.RFC3339),
		LastUpdated:    user.LastUpdated.Format(time.RFC3339),
		LastViewed:     user.LastViewed.Format(time.RFC3339),
		ThumbnailUrl:   user.ThumbnailURL,
		Upvotes:        user.Reputation.Upvotes,
		Downvotes:      user.Reputation.Downvotes,
		Reputation:     user.Reputation.Score,
		IsBanned:       user.IsBanned,
	}
}

// UserGroups converts a slice of API user group roles to RPC API user groups.
func UserGroups(groups []*apiTypes.UserGroupRoles) []*proto.UserGroup {
	result := make([]*proto.UserGroup, len(groups))
	for i, g := range groups {
		result[i] = &proto.UserGroup{
			Id:   g.Group.ID,
			Name: g.Group.Name,
			Role: g.Role.Name,
		}
	}
	return result
}

// Friends converts a slice of database extended friends to RPC API friends.
func Friends(friends []*apiTypes.ExtendedFriend) []*proto.Friend {
	result := make([]*proto.Friend, len(friends))
	for i, f := range friends {
		result[i] = &proto.Friend{
			Id:          f.ID,
			Name:        f.Name,
			DisplayName: f.DisplayName,
		}
	}
	return result
}

// Games converts a slice of API games to RPC API games.
func Games(games []*apiTypes.Game) []*proto.Game {
	result := make([]*proto.Game, len(games))
	for i, g := range games {
		result[i] = &proto.Game{
			Id:   g.ID,
			Name: g.Name,
		}
	}
	return result
}
