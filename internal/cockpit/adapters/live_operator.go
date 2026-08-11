package adapters

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strings"

	operatorclient "github.com/swobuforge/swobu/internal/app/operator/client"
	"github.com/swobuforge/swobu/internal/app/operator/controlplane"
	workspaceapi "github.com/swobuforge/swobu/internal/app/operator/workspaces"
	"github.com/swobuforge/swobu/internal/cockpit/ports"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/cockpit/ui"
	"github.com/swobuforge/swobu/internal/platform/browser"
	"github.com/swobuforge/swobu/internal/platform/clipboard"
	"github.com/swobuforge/swobu/internal/platform/config"
	"github.com/swobuforge/swobu/internal/routing"
)

type operatorClient interface {
	ListWorkspaces(context.Context) ([]workspaceapi.WorkspaceSummary, error)
	GetWorkspace(context.Context, string) (workspaceapi.Workspace, error)
	CreateWorkspace(context.Context, workspaceapi.CreateWorkspace) (workspaceapi.Workspace, error)
	RenameWorkspace(context.Context, string, string) (workspaceapi.Workspace, error)
	DeleteWorkspace(context.Context, string) error
	CreateRoute(context.Context, workspaceapi.CreateRoute) (workspaceapi.Workspace, error)
	RenameRoute(context.Context, workspaceapi.RenameRoute) (workspaceapi.Workspace, error)
	SetDefaultRoute(context.Context, workspaceapi.SetDefaultRoute) (workspaceapi.Workspace, error)
	DeleteRoute(context.Context, workspaceapi.DeleteRoute) (workspaceapi.Workspace, error)
	CreateTarget(context.Context, workspaceapi.CreateTarget) (workspaceapi.Workspace, error)
	UpdateTargetSettings(context.Context, workspaceapi.UpdateTargetSettings) (workspaceapi.Workspace, error)
	DeleteTarget(context.Context, workspaceapi.DeleteTarget) (workspaceapi.Workspace, error)
	Status(context.Context, string) (operatorclient.StatusProjection, error)
	DaemonVersion(context.Context) (string, error)
	StartAuthSession(context.Context, string, string, string, string, string, string) (operatorclient.AuthSessionStartResult, error)
	GetAuthSessionStatus(context.Context, string) (operatorclient.AuthSessionStatusResult, error)
	CancelAuthSession(context.Context, string) error
	RetryAuthSession(context.Context, string) (operatorclient.AuthSessionRetryResult, error)
	ProbeTarget(context.Context, workspaceapi.Connection, string) (operatorclient.ModelCatalogResult, error)
	StorePastedCredential(context.Context, string, string, string) (string, error)
}
type LiveOperatorAdapter struct {
	client operatorClient
	addr   string
}

func NewLiveOperatorAdapter(httpClient *http.Client, addr string) *LiveOperatorAdapter {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	a := &LiveOperatorAdapter{client: operatorclient.New(httpClient, config.BaseURL(addr)), addr: addr}
	ui.RegisterEffectHooks(browser.Open, clipboard.TryWriteText, clipboard.WriteTempFileFallback)
	return a
}

func (a *LiveOperatorAdapter) LoadCockpit(ctx context.Context) (readmodel.CockpitReadModel, error) {
	summaries, err := a.client.ListWorkspaces(ctx)
	if err != nil {
		return readmodel.CockpitReadModel{}, adapterFailure("load cockpit data", err)
	}
	sort.Slice(summaries, func(i, j int) bool { return summaries[i].Slug < summaries[j].Slug })
	model := readmodel.CockpitReadModel{HeaderRight: a.headerRight(), ActivePage: readmodel.CockpitWorkspacePage, Help: a.helpReadModel(ctx, summaries)}
	if len(summaries) == 0 {
		model.SelectedWorkspaceID = "+"
		model.SelectedWorkspace = draftWorkspace()
		model.Tabs = []readmodel.WorkspaceTabReadModel{{ID: "+", Kind: readmodel.WorkspaceTabDraft, Selected: true}, {ID: "?", Kind: readmodel.WorkspaceTabHelp}}
		return model, nil
	}
	selected, err := a.client.GetWorkspace(ctx, summaries[0].Slug)
	if err != nil {
		return readmodel.CockpitReadModel{}, adapterFailure("load selected workspace", err)
	}
	workspace, err := a.workspaceFromView(ctx, selected)
	if err != nil {
		return readmodel.CockpitReadModel{}, err
	}
	model.SelectedWorkspaceID = workspace.ID
	model.SelectedWorkspace = workspace
	for _, summary := range summaries {
		id := readmodel.WorkspaceID(summary.Slug)
		model.Tabs = append(model.Tabs, readmodel.WorkspaceTabReadModel{ID: id, Slug: summary.Slug, Kind: readmodel.WorkspaceTabExisting, Selected: id == model.SelectedWorkspaceID})
	}
	model.Tabs = append(model.Tabs, readmodel.WorkspaceTabReadModel{ID: "+", Kind: readmodel.WorkspaceTabDraft}, readmodel.WorkspaceTabReadModel{ID: "?", Kind: readmodel.WorkspaceTabHelp})
	return model, nil
}
func (a *LiveOperatorAdapter) helpReadModel(ctx context.Context, summaries []workspaceapi.WorkspaceSummary) readmodel.HelpReadModel {
	version, _ := a.client.DaemonVersion(ctx)
	payload := readmodel.DiagnosticsPayload{Version: controlplane.SwobuVersion(), DaemonVersion: version, ConfigPath: config.DefaultConfigPath()}
	for _, summary := range summaries {
		payload.Workspaces = append(payload.Workspaces, readmodel.DiagnosticsWorkspacePayload{Name: summary.Slug, ActivityCount: a.activityCount(ctx, summary.Slug)})
	}
	return readmodel.HelpReadModel{Version: controlplane.SwobuVersion(), DaemonVersion: version, Diagnostics: payload}
}
func (a *LiveOperatorAdapter) activityCount(ctx context.Context, slug string) int {
	projection, err := a.client.Status(ctx, "workspace:"+slug)
	if err != nil {
		return 0
	}
	return len(projection.RecentTraffic)
}
func (a *LiveOperatorAdapter) LoadWorkspace(ctx context.Context, id readmodel.WorkspaceID) (readmodel.WorkspaceReadModel, error) {
	if id == "" || id == "+" {
		return draftWorkspace(), nil
	}
	workspace, err := a.client.GetWorkspace(ctx, string(id))
	if err != nil {
		return readmodel.WorkspaceReadModel{}, adapterFailure("load workspace", err)
	}
	return a.workspaceFromView(ctx, workspace)
}
func (a *LiveOperatorAdapter) RenameWorkspace(ctx context.Context, request ports.RenameWorkspaceRequest) (readmodel.WorkspaceReadModel, error) {
	slug := strings.TrimSpace(request.Slug)
	if slug == "" {
		return readmodel.WorkspaceReadModel{}, errors.New("workspace slug is required")
	}
	if request.ID == "" || request.ID == "+" {
		return readmodel.WorkspaceReadModel{}, errors.New("workspace draft naming is local to Cockpit")
	}
	if string(request.ID) == slug {
		workspace, err := a.client.GetWorkspace(ctx, slug)
		if err != nil {
			return readmodel.WorkspaceReadModel{}, adapterFailure("load workspace", err)
		}
		return a.workspaceFromView(ctx, workspace)
	}
	workspace, err := a.client.RenameWorkspace(ctx, string(request.ID), slug)
	if err != nil {
		return readmodel.WorkspaceReadModel{}, adapterFailure("rename workspace", err)
	}
	return a.workspaceFromView(ctx, workspace)
}
func (a *LiveOperatorAdapter) DeleteWorkspace(ctx context.Context, request ports.DeleteWorkspaceRequest) error {
	err := a.client.DeleteWorkspace(ctx, string(request.ID))
	if err == nil {
		return nil
	}
	if !operatorclient.IsNotFound(err) {
		return adapterFailure("delete workspace", err)
	}
	// Only persisted workspaces reach this adapter. A delete 404 is therefore
	// stale-projection evidence, not draft-discard behavior. Re-read the
	// authoritative object: absence satisfies the requested postcondition;
	// presence preserves the original delete failure.
	_, reconcileErr := a.client.GetWorkspace(ctx, string(request.ID))
	if operatorclient.IsNotFound(reconcileErr) {
		return nil
	}
	if reconcileErr != nil {
		return adapterFailure("reconcile workspace delete", reconcileErr)
	}
	return adapterFailure("delete workspace", err)
}
func (a *LiveOperatorAdapter) SaveRoute(ctx context.Context, request ports.SaveRouteRequest) (readmodel.RouteReadModel, error) {
	if request.RouteID == "" {
		return readmodel.RouteReadModel{}, ErrUnsupportedCommand
	}
	workspaceID := string(request.WorkspaceID)
	routeID := string(request.RouteID)
	name := strings.TrimSpace(request.ModelName)
	if name == "" {
		name = routeID
	}
	var workspace workspaceapi.Workspace
	var err error
	if name == routeID {
		workspace, err = a.client.GetWorkspace(ctx, workspaceID)
		if err != nil {
			return readmodel.RouteReadModel{}, adapterFailure("load route", err)
		}
	} else {
		workspace, err = a.client.RenameRoute(ctx, workspaceapi.RenameRoute{Workspace: workspaceID, Route: routeID, NewName: name})
		if err != nil {
			return readmodel.RouteReadModel{}, adapterFailure("rename route", err)
		}
	}
	if request.Default {
		workspace, err = a.client.SetDefaultRoute(ctx, workspaceapi.SetDefaultRoute{Workspace: workspaceID, Route: name})
		if err != nil {
			return readmodel.RouteReadModel{}, adapterFailure("set default route", err)
		}
	}
	for _, route := range workspace.Routes {
		if route.Name == name {
			return routeFromWorkspaceRoute(workspace.DefaultRoute, route)
		}
	}
	return readmodel.RouteReadModel{}, errors.New("committed route missing")
}
func (a *LiveOperatorAdapter) DeleteRoute(ctx context.Context, request ports.DeleteRouteRequest) error {
	workspace, err := a.client.GetWorkspace(ctx, string(request.WorkspaceID))
	if err != nil {
		return adapterFailure("load route", err)
	}
	replacement := ""
	if workspace.DefaultRoute == string(request.RouteID) {
		for _, route := range workspace.Routes {
			if route.Name != string(request.RouteID) {
				replacement = route.Name
				break
			}
		}
	}
	_, err = a.client.DeleteRoute(ctx, workspaceapi.DeleteRoute{Workspace: string(request.WorkspaceID), Route: string(request.RouteID), Replacement: replacement})
	if err != nil {
		return adapterFailure("delete route", err)
	}
	return nil
}
func (a *LiveOperatorAdapter) SaveTarget(ctx context.Context, request ports.SaveTargetRequest) (ports.SaveTargetResult, error) {
	workspaceID := strings.TrimSpace(string(request.WorkspaceID))
	routeID := strings.TrimSpace(string(request.RouteID))
	if workspaceID == "" || routeID == "" {
		return ports.SaveTargetResult{}, errors.New("workspace and route are required")
	}
	targetID := strings.TrimSpace(string(request.TargetID))
	if targetID == "" {
		var err error
		targetID, err = newTargetID()
		if err != nil {
			return ports.SaveTargetResult{}, err
		}
	}
	target, err := targetFromSaveRequest(request, targetID)
	if err != nil {
		return ports.SaveTargetResult{}, err
	}
	workspace, currentErr := a.client.GetWorkspace(ctx, workspaceID)
	if currentErr != nil {
		if !operatorclient.IsNotFound(currentErr) {
			return ports.SaveTargetResult{}, adapterFailure("load workspace", currentErr)
		}
		workspace, err = a.client.CreateWorkspace(ctx, workspaceapi.CreateWorkspace{Slug: workspaceID, InitialRoute: routeID, Target: target})
	} else {
		routeExists := false
		for _, route := range workspace.Routes {
			if route.Name == routeID {
				routeExists = true
				break
			}
		}
		if !routeExists {
			workspace, err = a.client.CreateRoute(ctx, workspaceapi.CreateRoute{Workspace: workspaceID, Name: routeID, Target: target})
		} else if request.TargetID == "" {
			workspace, err = a.client.CreateTarget(ctx, workspaceapi.CreateTarget{Workspace: workspaceID, Route: routeID, Target: target, Placement: placementFromReadModel(request.Placement)})
		} else {
			workspace, err = a.client.UpdateTargetSettings(ctx, workspaceapi.UpdateTargetSettings{Workspace: workspaceID, Route: routeID, TargetID: targetID, Target: workspaceapi.TargetSettingsDraft{Model: target.Model, Protocol: target.Protocol, Connection: target.Connection}})
		}
	}
	if err != nil {
		return ports.SaveTargetResult{}, adapterFailure("save target", err)
	}
	committedTarget, err := targetFromWorkspace(workspace, targetID)
	if err != nil {
		return ports.SaveTargetResult{}, err
	}
	for _, route := range workspace.Routes {
		if route.Name == routeID {
			workspaceModel, projectionErr := a.workspaceFromView(ctx, workspace)
			if projectionErr != nil {
				return ports.SaveTargetResult{}, projectionErr
			}
			projectedRoute, projectionErr := routeFromWorkspaceRoute(workspace.DefaultRoute, route)
			if projectionErr != nil {
				return ports.SaveTargetResult{}, projectionErr
			}
			return ports.SaveTargetResult{Target: committedTarget, Route: projectedRoute, Workspace: workspaceModel}, nil
		}
	}
	return ports.SaveTargetResult{}, errors.New("committed route missing from workspace response")
}
func (a *LiveOperatorAdapter) DeleteTarget(ctx context.Context, request ports.DeleteTargetRequest) (readmodel.RouteReadModel, error) {
	workspace, err := a.client.DeleteTarget(ctx, workspaceapi.DeleteTarget{Workspace: string(request.WorkspaceID), Route: string(request.RouteID), TargetID: string(request.TargetID)})
	if err != nil {
		return readmodel.RouteReadModel{}, adapterFailure("delete target", err)
	}
	for _, route := range workspace.Routes {
		if route.Name == string(request.RouteID) {
			return routeFromWorkspaceRoute(workspace.DefaultRoute, route)
		}
	}
	return readmodel.RouteReadModel{}, errors.New("committed route missing from workspace response")
}
func (a *LiveOperatorAdapter) StorePastedCredential(ctx context.Context, req ports.StorePastedCredentialRequest) (ports.StorePastedCredentialResult, error) {
	ref, err := a.client.StorePastedCredential(ctx, req.ProviderSpec, req.Name, req.Secret)
	if err != nil {
		return ports.StorePastedCredentialResult{}, err
	}
	return ports.StorePastedCredentialResult{CredentialRef: ref}, nil
}
func (a *LiveOperatorAdapter) ProbeProviderModels(ctx context.Context, req ports.ProbeProviderModelsRequest) (readmodel.ModelCatalogReadModel, error) {
	connection := workspaceapi.Connection{}
	bedrockProbe := false
	switch probe := req.Probe.(type) {
	case ports.BedrockCatalogProbe:
		bedrockProbe = true
		connection.Bedrock = &workspaceapi.BedrockConnection{
			Region:     strings.TrimSpace(probe.Region),
			Credential: strings.TrimSpace(probe.CredentialRef),
		}
	case ports.ConnectionCatalogProbe:
		if probe.Connection == nil {
			return readmodel.ModelCatalogReadModel{}, errors.New("model catalog connection is required")
		}
		connection = workspaceapi.ConnectionFromRouting(probe.Connection)
		_, bedrockProbe = probe.Connection.(routing.BedrockConnection)
	default:
		return readmodel.ModelCatalogReadModel{}, errors.New("model catalog connection is required")
	}
	result, err := a.client.ProbeTarget(ctx, connection, req.ProviderProtocol)
	if err != nil {
		return readmodel.ModelCatalogReadModel{}, err
	}
	var deployments []readmodel.ModelDeploymentReadModel
	for _, d := range result.Deployments {
		deployments = append(deployments, readmodel.ModelDeploymentReadModel{ID: d.Name, Name: d.Name, ModelName: d.ModelName, ModelPublisher: d.ModelPublisher, ModelVersion: d.ModelVersion, Family: d.Family, SupportedProviderProtocols: d.SupportedProviderProtocols, DefaultProviderProtocol: d.DefaultProviderProtocol})
	}
	model := readmodel.ModelCatalogReadModel{Deployments: deployments, ResolvedProviderProtocol: result.ResolvedProviderProtocol}
	if bedrockProbe && len(result.Diagnostics) > 0 {
		evidence, err := decodeBedrockAuthenticationDiagnostics(result.Diagnostics)
		if err != nil {
			return readmodel.ModelCatalogReadModel{}, err
		}
		model.BedrockAuthentication = evidence
	}
	if strings.TrimSpace(result.Error) != "" {
		return model, errors.New(result.Error)
	}
	return model, nil
}

func (a *LiveOperatorAdapter) StartAuthSession(ctx context.Context, req ports.StartAuthSessionRequest) (readmodel.AuthSessionReadModel, error) {
	result, err := a.client.StartAuthSession(ctx, req.ProviderSpec, req.Workspace, req.Route, req.TargetID, req.DraftSubject, req.AuthMode)
	if err != nil {
		return readmodel.AuthSessionReadModel{}, err
	}
	return readmodel.AuthSessionReadModel{ProviderSpec: result.ProviderSpec, SessionID: result.SessionID, AuthorizeURL: result.AuthorizeURL, UserCode: result.UserCode, State: result.State}, nil
}
func (a *LiveOperatorAdapter) PollAuthSession(ctx context.Context, id string) (readmodel.AuthSessionReadModel, error) {
	result, err := a.client.GetAuthSessionStatus(ctx, id)
	if err != nil {
		return readmodel.AuthSessionReadModel{}, err
	}
	return readmodel.AuthSessionReadModel{ProviderSpec: result.ProviderSpec, SessionID: result.SessionID, State: result.State, CredentialRef: result.CredentialRef, ErrorMessage: result.ErrorMessage}, nil
}
func (a *LiveOperatorAdapter) CancelAuthSession(ctx context.Context, id string) error {
	return a.client.CancelAuthSession(ctx, id)
}
func (a *LiveOperatorAdapter) RetryAuthSession(ctx context.Context, id string) (readmodel.AuthSessionReadModel, error) {
	result, err := a.client.RetryAuthSession(ctx, id)
	if err != nil {
		return readmodel.AuthSessionReadModel{}, err
	}
	return readmodel.AuthSessionReadModel{SessionID: result.SessionID, AuthorizeURL: result.AuthorizeURL, UserCode: result.UserCode, State: result.State}, nil
}

var _ ports.WorkspaceQueries = (*LiveOperatorAdapter)(nil)
var _ ports.WorkspaceCommands = (*LiveOperatorAdapter)(nil)
var _ ports.RouteCommands = (*LiveOperatorAdapter)(nil)
var _ ports.ActivityQueries = (*LiveOperatorAdapter)(nil)
var _ ports.TargetAuthCommands = (*LiveOperatorAdapter)(nil)
var _ ports.TargetCredentialCommands = (*LiveOperatorAdapter)(nil)
