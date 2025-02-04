package convert

import (
	apiTypes "github.com/jaxron/roapi.go/pkg/api/types"
	"github.com/robalyx/rotector/internal/common/storage/database/types"
	"github.com/robalyx/rotector/internal/common/storage/database/types/enum"
	restTypes "github.com/robalyx/rotector/internal/rest/types"
)

// GroupStatus converts a database group status to REST API group status.
func GroupStatus(status enum.GroupType) restTypes.GroupStatus {
	switch status {
	case enum.GroupTypeFlagged:
		return restTypes.GroupStatusFlagged
	case enum.GroupTypeConfirmed:
		return restTypes.GroupStatusConfirmed
	case enum.GroupTypeCleared:
		return restTypes.GroupStatusCleared
	case enum.GroupTypeUnflagged:
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
		Reasons:      GroupReasons(group.Reasons),
		Confidence:   group.Confidence,
		LastScanned:  group.LastScanned,
		LastUpdated:  group.LastUpdated,
		LastViewed:   group.LastViewed,
		ThumbnailURL: group.ThumbnailURL,
		Upvotes:      group.Reputation.Upvotes,
		Downvotes:    group.Reputation.Downvotes,
		Reputation:   group.Reputation.Score,
		IsLocked:     group.IsLocked,
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

// GroupReasons converts a database group reasons to REST API group reasons.
func GroupReasons(reasons types.Reasons) restTypes.Reasons {
	result := make(restTypes.Reasons)
	for k, v := range reasons {
		result[k.String()] = restTypes.Reason{
			Message:    v.Message,
			Confidence: v.Confidence,
			Evidence:   v.Evidence,
		}
	}
	return result
}
