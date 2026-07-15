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

func (fakeWorkspacePorts) SaveWorkspace(context.Context, SaveWorkspaceRequest) (readmodel.WorkspaceReadModel, error) {
	return readmodel.WorkspaceReadModel{}, nil
}

func (fakeWorkspacePorts) DeleteWorkspace(context.Context, DeleteWorkspaceRequest) error {
	return nil
}

type fakeRoutePorts struct{}

func (fakeRoutePorts) SaveRoute(context.Context, SaveRouteRequest) (readmodel.RouteReadModel, error) {
	return readmodel.RouteReadModel{}, nil
}

func (fakeRoutePorts) DeleteRoute(context.Context, DeleteRouteRequest) error {
	return nil
}

func (fakeRoutePorts) SaveTarget(context.Context, SaveTargetRequest) (readmodel.TargetReadModel, error) {
	return readmodel.TargetReadModel{}, nil
}

func (fakeRoutePorts) DeleteTarget(context.Context, DeleteTargetRequest) error {
	return nil
}

type fakeRunExecutor struct{}

func (fakeRunExecutor) ExecuteRunCommand(context.Context, ExecuteRunCommandRequest) (RunExecutionResult, error) {
	return RunExecutionResult{}, nil
}

type fakeActivityQueries struct{}

func (fakeActivityQueries) ListActivity(context.Context, ListActivityRequest) (readmodel.ActivityReadModel, error) {
	return readmodel.ActivityReadModel{}, nil
}

func (fakeActivityQueries) GetActivity(context.Context, readmodel.ActivityID) (readmodel.ActivityRowReadModel, error) {
	return readmodel.ActivityRowReadModel{}, nil
}

type fakeHelpActions struct{}

func (fakeHelpActions) OpenDocs(context.Context) error {
	return nil
}

func (fakeHelpActions) OpenCommunity(context.Context) error {
	return nil
}

func (fakeHelpActions) OpenIssue(context.Context) error {
	return nil
}

func (fakeHelpActions) CopyDiagnostics(context.Context) (DiagnosticsCopyResult, error) {
	return DiagnosticsCopyResult{Status: DiagnosticsCopyCopied}, nil
}

func TestPortInterfacesSeparateCockpitConcerns(t *testing.T) {
	var _ WorkspaceQueries = fakeWorkspacePorts{}
	var _ WorkspaceCommands = fakeWorkspacePorts{}
	var _ RouteCommands = fakeRoutePorts{}
	var _ RunExecutor = fakeRunExecutor{}
	var _ ActivityQueries = fakeActivityQueries{}
	var _ HelpActions = fakeHelpActions{}
}
