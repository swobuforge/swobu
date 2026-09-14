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
	// Connection carries raw operator intent. The daemon owns validation and
	// finalization before routing publication.
	Connection routing.ConnectionDraft
	Placement  readmodel.PlacementOptionReadModel
}

type SaveTargetResult struct {
	Target    readmodel.TargetReadModel
	Route     readmodel.RouteReadModel
	Workspace readmodel.WorkspaceReadModel
}
