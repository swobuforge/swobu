package ports

import (
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/routing"
)

// SaveTargetRequest describes an add or edit request for one route target.
type SaveTargetRequest struct {
	WorkspaceID readmodel.WorkspaceID
	RouteID     readmodel.RouteID
	TargetID    readmodel.TargetID
	ModelID     string
	Protocol    string
	// Connection is validated authoring output. Persistence projects it to
	// transport without receiving or reinterpreting incomplete UI draft state.
	Connection routing.Connection
	Placement  readmodel.PlacementOptionReadModel
}

type SaveTargetResult struct {
	Target    readmodel.TargetReadModel
	Route     readmodel.RouteReadModel
	Workspace readmodel.WorkspaceReadModel
}

// DeleteTargetRequest names the target to remove from a route.
type DeleteTargetRequest struct {
	WorkspaceID readmodel.WorkspaceID
	RouteID     readmodel.RouteID
	TargetID    readmodel.TargetID
}
