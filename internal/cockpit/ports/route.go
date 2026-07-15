package ports

import (
	"context"

	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
)

// RouteCommands mutates client-visible routes and their targets.
type RouteCommands interface {
	SaveRoute(ctx context.Context, request SaveRouteRequest) (readmodel.RouteReadModel, error)
	DeleteRoute(ctx context.Context, request DeleteRouteRequest) error
	SaveTarget(ctx context.Context, request SaveTargetRequest) (readmodel.TargetReadModel, error)
	DeleteTarget(ctx context.Context, request DeleteTargetRequest) error
}

// SaveRouteRequest describes an add or edit request for one client-visible
// model name.
type SaveRouteRequest struct {
	WorkspaceID readmodel.WorkspaceID
	RouteID     readmodel.RouteID
	ModelName   string
	Enabled     bool
	Default     bool
}

// DeleteRouteRequest names the route to delete.
type DeleteRouteRequest struct {
	WorkspaceID readmodel.WorkspaceID
	RouteID     readmodel.RouteID
}
