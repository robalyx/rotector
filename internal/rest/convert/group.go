package convert

import (
	apiTypes "github.com/jaxron/roapi.go/pkg/api/types"
	"github.com/robalyx/rotector/internal/common/storage/database/types"
	restTypes "github.com/robalyx/rotector/internal/rest/types"
)

// GroupStatus converts a database group status to REST API group status.
func GroupStatus(status types.GroupType) restTypes.GroupStatus {
	switch status {
	case types.GroupTypeFlagged:
		return restTypes.GroupStatusFlagged
	case types.GroupTypeConfirmed:
		return restTypes.GroupStatusConfirmed
	case types.GroupTypeCleared:
		return restTypes.GroupStatusCleared
	case types.GroupTypeLocked:
		return restTypes.GroupStatusLocked
	case types.GroupTypeUnflagged:
		return restTypes.GroupStatusUnflagged
	default:
		return restTypes.GroupStatusUnflagged
	}
}

// Group converts a database group to REST API group.
func Group(group *types.ReviewGroup) *restTypes.Group {
	if group == nil {
		return nil
	}

	return &restTypes.Group{
		ID:           group.ID,
		Name:         group.Name,
		Description:  group.Description,
		Owner:        GroupUser(group.Owner),
		Shout:        GroupShout(group.Shout),
		Reason:       group.Reason,
		Confidence:   group.Confidence,
		LastScanned:  group.LastScanned,
		LastUpdated:  group.LastUpdated,
		LastViewed:   group.LastViewed,
		ThumbnailURL: group.ThumbnailURL,
		Upvotes:      group.Reputation.Upvotes,
		Downvotes:    group.Reputation.Downvotes,
		Reputation:   group.Reputation.Score,
	}
}

// GroupUser converts an API group user to REST API group user.
func GroupUser(user *apiTypes.GroupUser) restTypes.GroupUser {
	if user == nil {
		return restTypes.GroupUser{}
	}
	return restTypes.GroupUser{
		ID:          user.UserID,
		Name:        user.Username,
		DisplayName: user.DisplayName,
	}
}

// GroupShout converts an API group shout to REST API group shout.
func GroupShout(shout *apiTypes.GroupShout) restTypes.GroupShout {
	if shout == nil {
		return restTypes.GroupShout{}
	}
	return restTypes.GroupShout{
		Content: shout.Body,
		Poster:  GroupUser(&shout.Poster),
	}
}
