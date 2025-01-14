package convert

import (
	"time"

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
	case enum.GroupTypeLocked:
		return proto.GroupStatus_GROUP_STATUS_LOCKED
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

	// Convert owner information
	var owner *proto.GroupUser
	if group.Owner != nil {
		owner = &proto.GroupUser{
			Id:          group.Owner.UserID,
			Name:        group.Owner.Username,
			DisplayName: group.Owner.DisplayName,
		}
	}

	// Convert shout information
	var shout *proto.GroupShout
	if group.Shout != nil {
		shout = &proto.GroupShout{
			Content: group.Shout.Body,
			Poster: &proto.GroupUser{
				Id:          group.Shout.Poster.UserID,
				Name:        group.Shout.Poster.Username,
				DisplayName: group.Shout.Poster.DisplayName,
			},
		}
	}

	return &proto.Group{
		Id:           group.ID,
		Name:         group.Name,
		Description:  group.Description,
		Owner:        owner,
		Shout:        shout,
		Reason:       group.Reason,
		Confidence:   group.Confidence,
		LastScanned:  group.LastScanned.Format(time.RFC3339),
		LastUpdated:  group.LastUpdated.Format(time.RFC3339),
		LastViewed:   group.LastViewed.Format(time.RFC3339),
		ThumbnailUrl: group.ThumbnailURL,
		Upvotes:      group.Reputation.Upvotes,
		Downvotes:    group.Reputation.Downvotes,
		Reputation:   group.Reputation.Score,
	}
}
