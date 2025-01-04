package convert

import (
	"time"

	"github.com/rotector/rotector/internal/common/storage/database/types"
	"github.com/rotector/rotector/internal/rpc/proto"
)

// UserStatus converts a database user status to RPC API user status.
func UserStatus(status types.UserType) proto.UserStatus {
	switch status {
	case types.UserTypeFlagged:
		return proto.UserStatus_USER_STATUS_FLAGGED
	case types.UserTypeConfirmed:
		return proto.UserStatus_USER_STATUS_CONFIRMED
	case types.UserTypeCleared:
		return proto.UserStatus_USER_STATUS_CLEARED
	case types.UserTypeBanned:
		return proto.UserStatus_USER_STATUS_BANNED
	case types.UserTypeUnflagged:
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

	protoUser := &proto.User{
		Id:             user.ID,
		Name:           user.Name,
		DisplayName:    user.DisplayName,
		Description:    user.Description,
		CreatedAt:      user.CreatedAt.Format(time.RFC3339),
		Reason:         user.Reason,
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
	}

	// Convert groups
	for _, g := range user.Groups {
		protoUser.Groups = append(protoUser.Groups, &proto.UserGroup{
			Id:   g.Group.ID,
			Name: g.Group.Name,
			Role: g.Role.Name,
		})
	}

	// Convert friends
	for _, f := range user.Friends {
		protoUser.Friends = append(protoUser.Friends, &proto.Friend{
			Id:               f.ID,
			Name:             f.Name,
			DisplayName:      f.DisplayName,
			HasVerifiedBadge: f.HasVerifiedBadge,
		})
	}

	// Convert games
	for _, g := range user.Games {
		protoUser.Games = append(protoUser.Games, &proto.Game{
			Id:   g.ID,
			Name: g.Name,
		})
	}

	return protoUser
}
