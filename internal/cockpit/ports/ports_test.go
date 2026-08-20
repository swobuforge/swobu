package ports

import (
	"context"
	"testing"

	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
)

type fakeWorkspacePorts struct{}

func (fakeWorkspacePorts) LoadCockpit(context.Context) (readmodel.CockpitReadModel, error) {
	return readmodel.CockpitReadModel{}, nil
}

func (fakeWorkspacePorts) LoadWorkspace(context.Context, readmodel.WorkspaceID) (readmodel.WorkspaceReadModel, error) {
	return readmodel.WorkspaceReadModel{}, nil
}

func (fakeWorkspacePorts) RenameWorkspace(context.Context, RenameWorkspaceRequest) (readmodel.WorkspaceReadModel, error) {
	return readmodel.WorkspaceReadModel{}, nil
}

func (fakeWorkspacePorts) DeleteWorkspace(context.Context, DeleteWorkspaceRequest) error {
	return nil
}

type fakeRoutePorts struct{}

func (fakeRoutePorts) SaveRoute(context.Context, SaveRouteRequest) (RouteMutationResult, error) {
	return RouteMutationResult{}, nil
}

func (fakeRoutePorts) DeleteRoute(context.Context, DeleteRouteRequest) (RouteMutationResult, error) {
	return RouteMutationResult{}, nil
}

func (fakeRoutePorts) SaveTarget(context.Context, SaveTargetRequest) (SaveTargetResult, error) {
	return SaveTargetResult{}, nil
}

func (fakeRoutePorts) ApplyRouteDraft(context.Context, ApplyRouteDraftRequest) (RouteMutationResult, error) {
	return RouteMutationResult{}, nil
}

type fakeActivityQueries struct{}

func (fakeActivityQueries) ListActivity(context.Context, ListActivityRequest) (readmodel.ActivityReadModel, error) {
	return readmodel.ActivityReadModel{}, nil
}

func TestPortInterfacesSeparateCockpitConcerns(t *testing.T) {
	var _ WorkspaceQueries = fakeWorkspacePorts{}
	var _ WorkspaceCommands = fakeWorkspacePorts{}
	var _ RouteCommands = fakeRoutePorts{}
	var _ ActivityQueries = fakeActivityQueries{}
}
