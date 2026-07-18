package ports

import (
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
)

// SaveTargetRequest describes an add or edit request for one route target.
type SaveTargetRequest struct {
	WorkspaceID readmodel.WorkspaceID
	RouteID     readmodel.RouteID
	TargetID    readmodel.TargetID
	Draft       readmodel.TargetDraft
	Placement   readmodel.PlacementOptionReadModel
}

type SaveTargetResult struct {
	Target readmodel.TargetReadModel
	Route  readmodel.RouteReadModel
}

// DeleteTargetRequest names the target to remove from a route.
type DeleteTargetRequest struct {
	WorkspaceID readmodel.WorkspaceID
	RouteID     readmodel.RouteID
	TargetID    readmodel.TargetID
}
