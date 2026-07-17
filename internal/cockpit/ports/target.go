package ports

import (
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/domain/endpointintent"
)

// SaveTargetRequest describes an add or edit request for one route target.
type SaveTargetRequest struct {
	WorkspaceID readmodel.WorkspaceID
	RouteID     readmodel.RouteID
	TargetID    readmodel.TargetID
	Draft       endpointintent.TargetDraft
}

// DeleteTargetRequest names the target to remove from a route.
type DeleteTargetRequest struct {
	WorkspaceID readmodel.WorkspaceID
	RouteID     readmodel.RouteID
	TargetID    readmodel.TargetID
}
