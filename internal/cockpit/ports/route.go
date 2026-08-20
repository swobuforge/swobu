package ports

import (
	"context"

	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
)

// RouteCommands mutates client-visible routes and their targets.
type RouteCommands interface {
	SaveRoute(ctx context.Context, request SaveRouteRequest) (RouteMutationResult, error)
	DeleteRoute(ctx context.Context, request DeleteRouteRequest) (RouteMutationResult, error)
	SaveTarget(ctx context.Context, request SaveTargetRequest) (SaveTargetResult, error)
	ApplyRouteDraft(ctx context.Context, request ApplyRouteDraftRequest) (RouteMutationResult, error)
}

// RouteMutationResult carries the daemon-committed route and its containing
// workspace projection. The workspace is the authority boundary; the route
// is retained only for local interaction handoff.
type RouteMutationResult struct {
	Route     readmodel.RouteReadModel
	Workspace readmodel.WorkspaceReadModel
}

// ApplyRouteDraft submits the complete desired target topology for one route.
type ApplyRouteDraftRequest struct {
	WorkspaceID readmodel.WorkspaceID
	Route       readmodel.RouteReadModel
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
