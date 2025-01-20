package convert

import (
	"time"

	apiTypes "github.com/jaxron/roapi.go/pkg/api/types"
	"github.com/robalyx/rotector/internal/common/storage/database/types"
	"github.com/robalyx/rotector/internal/common/storage/database/types/enum"
	"github.com/robalyx/rotector/internal/rpc/proto"
)

// GroupStatus converts a database group status to RPC API group status.
func GroupStatus(status enum.GroupType) proto.GroupStatus {
	switch status {
	case enum.GroupTypeFlagged:
		return proto.GroupStatus_GROUP_STATUS_FLAGGED
	case enum.GroupTypeConfirmed:
		return proto.GroupStatus_GROUP_STATUS_CONFIRMED
	case enum.GroupTypeCleared:
		return proto.GroupStatus_GROUP_STATUS_CLEARED
	case enum.GroupTypeUnflagged:
		return proto.GroupStatus_GROUP_STATUS_UNFLAGGED
	default:
		return proto.GroupStatus_GROUP_STATUS_UNFLAGGED
	}
}

// Group converts a database group to RPC API group message.
func Group(group *types.ReviewGroup) *proto.Group {
	if group == nil {
		return nil
	}

	return &proto.Group{
		Id:           group.ID,
		Name:         group.Name,
		Description:  group.Description,
		Owner:        GroupUser(group.Owner),
		Shout:        GroupShout(group.Shout),
		Reason:       group.Reason,
		Confidence:   group.Confidence,
		LastScanned:  group.LastScanned.Format(time.RFC3339),
		LastUpdated:  group.LastUpdated.Format(time.RFC3339),
		LastViewed:   group.LastViewed.Format(time.RFC3339),
		ThumbnailUrl: group.ThumbnailURL,
		Upvotes:      group.Reputation.Upvotes,
		Downvotes:    group.Reputation.Downvotes,
		Reputation:   group.Reputation.Score,
		IsLocked:     group.IsLocked,
	}
}

// GroupUser converts an API group user to RPC API group user.
func GroupUser(user *apiTypes.GroupUser) *proto.GroupUser {
	if user == nil {
		return nil
	}
	return &proto.GroupUser{
		Id:          user.UserID,
		Name:        user.Username,
		DisplayName: user.DisplayName,
	}
}

// GroupShout converts an API group shout to RPC API group shout.
func GroupShout(shout *apiTypes.GroupShout) *proto.GroupShout {
	if shout == nil {
		return nil
	}
	return &proto.GroupShout{
		Content: shout.Body,
		Poster:  GroupUser(&shout.Poster),
	}
}
