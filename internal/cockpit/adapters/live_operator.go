package adapters

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	operatorclient "github.com/swobuforge/swobu/internal/app/operator/client"
	clientprofile "github.com/swobuforge/swobu/internal/app/operator/clientprofile"
	"github.com/swobuforge/swobu/internal/cockpit/ports"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/platform/config"
)

type operatorClient interface {
	ListEndpoints(context.Context) ([]operatorclient.EndpointData, error)
	GetEndpoint(context.Context, string) (operatorclient.EndpointData, error)
	UpsertEndpoint(context.Context, operatorclient.EndpointData) error
	DeleteEndpoint(context.Context, string) error
	Status(context.Context, string) (operatorclient.StatusProjection, error)
}

// LiveOperatorAdapter implements Cockpit ports over the daemon operator control
// plane. Endpoint intent is projected as one Cockpit workspace per endpoint.
type LiveOperatorAdapter struct {
	client     operatorClient
	daemonURL  string
	runCommand runCommandExecutor
	commandIO  runCommandIOConfig
}

// NewLiveOperatorAdapter builds the daemon-backed Cockpit adapter.
func NewLiveOperatorAdapter(httpClient *http.Client, daemonURL string) *LiveOperatorAdapter {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	resolvedURL := config.ResolveDaemonURL(daemonURL)
	adapter := &LiveOperatorAdapter{
		client:    operatorclient.New(httpClient, resolvedURL),
		daemonURL: strings.TrimRight(resolvedURL, "/"),
		commandIO: processRunCommandIO(),
	}
	adapter.runCommand = func(ctx context.Context, command clientprofile.RunCommandSpec) error {
		return executeClientRunCommand(ctx, command, adapter.commandIO)
	}
	return adapter
}

// LoadCockpit projects the daemon endpoint list into the Cockpit read model.
func (a *LiveOperatorAdapter) LoadCockpit(ctx context.Context) (readmodel.CockpitReadModel, error) {
	endpoints, err := a.client.ListEndpoints(ctx)
	if err != nil {
		return readmodel.CockpitReadModel{}, adapterFailure("load cockpit data", err)
	}
	sort.Slice(endpoints, func(i, j int) bool {
		return endpoints[i].Name < endpoints[j].Name
	})

	model := readmodel.CockpitReadModel{
		HeaderRight: a.headerRight(),
		ActivePage:  readmodel.CockpitWorkspacePage,
		Help: readmodel.HelpReadModel{
			Diagnostics: diagnosticsPayloadFromEndpoints(a.baseDiagnosticsPayload(), endpoints, a.diagnosticsActivityCounts(ctx, endpoints)),
		},
	}
	if len(endpoints) == 0 {
		model.SelectedWorkspaceID = "+"
		model.SelectedWorkspace = draftWorkspace()
		model.Help.Diagnostics = diagnosticsPayloadFromEndpoints(a.baseDiagnosticsPayload(), nil, nil)
		model.Tabs = []readmodel.WorkspaceTabReadModel{
			{ID: "+", Kind: readmodel.WorkspaceTabDraft, Selected: true},
			{ID: "?", Kind: readmodel.WorkspaceTabHelp},
		}
		return model, nil
	}

	selected := endpoints[0]
	workspace, err := a.workspaceFromEndpoint(ctx, selected)
	if err != nil {
		return readmodel.CockpitReadModel{}, adapterFailure(fmt.Sprintf("load workspace %q", selected.Name), err)
	}
	model.SelectedWorkspaceID = readmodel.WorkspaceID(selected.Name)
	model.SelectedWorkspace = workspace
	for _, endpoint := range endpoints {
		id := readmodel.WorkspaceID(endpoint.Name)
		model.Tabs = append(model.Tabs, readmodel.WorkspaceTabReadModel{
			ID:       id,
			Slug:     endpoint.Name,
			Kind:     readmodel.WorkspaceTabExisting,
			Selected: id == model.SelectedWorkspaceID,
		})
	}
	model.Tabs = append(model.Tabs,
		readmodel.WorkspaceTabReadModel{ID: "+", Kind: readmodel.WorkspaceTabDraft},
		readmodel.WorkspaceTabReadModel{ID: "?", Kind: readmodel.WorkspaceTabHelp},
	)
	return model, nil
}

func (a *LiveOperatorAdapter) diagnosticsActivityCounts(ctx context.Context, endpoints []operatorclient.EndpointData) map[string]int {
	projection, err := a.client.Status(ctx, "all")
	if err != nil {
		return nil
	}
	counts := make(map[string]int, len(endpoints))
	endpointNames := make(map[string]struct{}, len(endpoints))
	for _, endpoint := range endpoints {
		endpointNames[endpoint.Name] = struct{}{}
	}
	for _, traffic := range projection.RecentTraffic {
		if _, ok := endpointNames[traffic.Endpoint]; ok {
			counts[traffic.Endpoint]++
		}
	}
	return counts
}

// LoadWorkspace projects one daemon endpoint into a Cockpit workspace row.
func (a *LiveOperatorAdapter) LoadWorkspace(ctx context.Context, id readmodel.WorkspaceID) (readmodel.WorkspaceReadModel, error) {
	if id == "" || id == "+" {
		return draftWorkspace(), nil
	}
	endpoint, err := a.client.GetEndpoint(ctx, string(id))
	if err != nil {
		return readmodel.WorkspaceReadModel{}, adapterFailure(fmt.Sprintf("load workspace %q", id), err)
	}
	workspace, err := a.workspaceFromEndpoint(ctx, endpoint)
	if err != nil {
		return readmodel.WorkspaceReadModel{}, adapterFailure(fmt.Sprintf("load workspace %q", id), err)
	}
	return workspace, nil
}

// SaveWorkspace updates the daemon endpoint. Slug changes (renames) are
// rejected because the daemon does not expose an atomic rename operation.
// Only in-place field edits of an existing endpoint are supported.
func (a *LiveOperatorAdapter) SaveWorkspace(ctx context.Context, request ports.SaveWorkspaceRequest) (readmodel.WorkspaceReadModel, error) {
	slug := strings.TrimSpace(request.Slug) // swobu:io-string source=boundary
	if request.ID == "" || request.ID == "+" {
		return readmodel.WorkspaceReadModel{}, adapterFailure(fmt.Sprintf("create workspace %q", slug), ErrUnsupportedCommand)
	}
	if slug == "" {
		return readmodel.WorkspaceReadModel{}, errors.New("save workspace: workspace slug is required")
	}
	current, err := a.client.GetEndpoint(ctx, string(request.ID))
	if err != nil {
		return readmodel.WorkspaceReadModel{}, adapterFailure(fmt.Sprintf("save workspace %q", request.ID), err)
	}
	if slug != string(request.ID) {
		return readmodel.WorkspaceReadModel{}, adapterFailure("rename workspace", ErrUnsupportedCommand)
	}
	current.Name = slug
	if err := a.client.UpsertEndpoint(ctx, current); err != nil {
		return readmodel.WorkspaceReadModel{}, adapterFailure(fmt.Sprintf("save workspace %q", slug), err)
	}
	workspace, err := a.workspaceFromEndpoint(ctx, current)
	if err != nil {
		return readmodel.WorkspaceReadModel{}, adapterFailure(fmt.Sprintf("load workspace %q after save", slug), err)
	}
	return workspace, nil
}

// DeleteWorkspace removes one daemon endpoint.
func (a *LiveOperatorAdapter) DeleteWorkspace(ctx context.Context, request ports.DeleteWorkspaceRequest) error {
	if err := a.client.DeleteEndpoint(ctx, string(request.ID)); err != nil {
		return adapterFailure(fmt.Sprintf("delete workspace %q", request.ID), err)
	}
	return nil
}

// SaveRoute renames the projected route group backed by matching provider
// configs. The daemon does not have a first-class route record yet: Cockpit
// groups provider configs by client-visible model name and rewrites that group
// in place. Creating an empty route, persisting an enabled flag, or changing
// routing policy without targets is therefore not representable here yet.
func (a *LiveOperatorAdapter) SaveRoute(ctx context.Context, request ports.SaveRouteRequest) (readmodel.RouteReadModel, error) {
	endpoint, err := a.client.GetEndpoint(ctx, string(request.WorkspaceID))
	if err != nil {
		return readmodel.RouteReadModel{}, adapterFailure(fmt.Sprintf("save route %q", request.RouteID), err)
	}
	routeID := strings.TrimSpace(string(request.RouteID)) // swobu:io-string source=boundary
	if routeID == "" {
		return readmodel.RouteReadModel{}, adapterFailure("save route", ErrUnsupportedCommand)
	}
	modelName := strings.TrimSpace(request.ModelName) // swobu:io-string source=boundary
	if modelName == "" {
		modelName = routeID
	}
	var selectedRef string
	changed := false
	for i := range endpoint.ProviderConfigs {
		if projectedRouteModel(endpoint.ProviderConfigs[i]) != routeID {
			continue
		}
		endpoint.ProviderConfigs[i].ModelID = modelName
		if selectedRef == "" {
			selectedRef = endpoint.ProviderConfigs[i].Ref
		}
		changed = true
	}
	if !changed {
		return readmodel.RouteReadModel{}, adapterFailure(fmt.Sprintf("create empty route %q", modelName), ErrUnsupportedCommand)
	}
	if request.Default && selectedRef != "" {
		endpoint.SelectedRef = selectedRef
	}
	if err := a.client.UpsertEndpoint(ctx, endpoint); err != nil {
		return readmodel.RouteReadModel{}, adapterFailure(fmt.Sprintf("save route %q", modelName), err)
	}
	return routeFromEndpoint(endpoint, modelName)
}

// DeleteRoute removes one projected route group from the provider configs.
func (a *LiveOperatorAdapter) DeleteRoute(ctx context.Context, request ports.DeleteRouteRequest) error {
	endpoint, err := a.client.GetEndpoint(ctx, string(request.WorkspaceID))
	if err != nil {
		return adapterFailure(fmt.Sprintf("delete route %q", request.RouteID), err)
	}
	routeID := strings.TrimSpace(string(request.RouteID)) // swobu:io-string source=boundary
	next := make([]operatorclient.ProviderConfigData, 0, len(endpoint.ProviderConfigs))
	removedSelected := false
	removed := false
	for _, config := range endpoint.ProviderConfigs {
		if projectedRouteModel(config) != routeID {
			next = append(next, config)
			continue
		}
		removed = true
		removedSelected = removedSelected || config.Ref == endpoint.SelectedRef
	}
	if !removed {
		return fmt.Errorf("delete route %q: route could not be resolved", request.RouteID)
	}
	if len(next) == 0 {
		return adapterFailure(fmt.Sprintf("delete final route %q", request.RouteID), ErrUnsupportedCommand)
	}
	endpoint.ProviderConfigs = next
	if removedSelected {
		endpoint.SelectedRef = next[0].Ref
	}
	if err := a.client.UpsertEndpoint(ctx, endpoint); err != nil {
		return adapterFailure(fmt.Sprintf("delete route %q", request.RouteID), err)
	}
	return nil
}

// SaveTarget updates or appends one provider config.
func (a *LiveOperatorAdapter) SaveTarget(ctx context.Context, request ports.SaveTargetRequest) (readmodel.TargetReadModel, error) {
	endpoint, err := a.client.GetEndpoint(ctx, string(request.WorkspaceID))
	if err != nil {
		return readmodel.TargetReadModel{}, adapterFailure(fmt.Sprintf("save target %q", request.TargetID), err)
	}
	routeID := strings.TrimSpace(string(request.RouteID)) // swobu:io-string source=boundary
	if routeID == "" {
		return readmodel.TargetReadModel{}, errors.New("save target: route is required")
	}
	targetID := strings.TrimSpace(string(request.TargetID)) // swobu:io-string source=boundary
	config, err := providerConfigFromTargetRequest(request, targetID)
	if err != nil {
		return readmodel.TargetReadModel{}, adapterFailure("save target", err)
	}
	if targetID == "" {
		ref, err := newProviderConfigRef(endpoint.ProviderConfigs)
		if err != nil {
			return readmodel.TargetReadModel{}, adapterFailure("save target", err)
		}
		config.Ref = ref
		endpoint.ProviderConfigs = append(endpoint.ProviderConfigs, config)
		if endpoint.SelectedRef == "" {
			endpoint.SelectedRef = ref
		}
	} else {
		replaced := false
		for i := range endpoint.ProviderConfigs {
			if !targetMatchesRoute(endpoint.ProviderConfigs[i], targetID, routeID) {
				continue
			}
			config.Ref = targetID
			endpoint.ProviderConfigs[i] = config
			replaced = true
			break
		}
		if !replaced {
			return readmodel.TargetReadModel{}, fmt.Errorf("save target %q: target not found in route %q", targetID, routeID)
		}
	}
	if err := a.client.UpsertEndpoint(ctx, endpoint); err != nil {
		return readmodel.TargetReadModel{}, adapterFailure(fmt.Sprintf("save target %q", config.Ref), err)
	}
	for _, route := range routesFromEndpoint(endpoint) {
		for _, target := range route.Targets {
			if string(target.ID) == config.Ref {
				return target, nil
			}
		}
	}
	return targetFromProviderConfig(config), nil
}

// DeleteTarget removes one provider config from the endpoint.
func (a *LiveOperatorAdapter) DeleteTarget(ctx context.Context, request ports.DeleteTargetRequest) error {
	endpoint, err := a.client.GetEndpoint(ctx, string(request.WorkspaceID))
	if err != nil {
		return adapterFailure(fmt.Sprintf("delete target %q", request.TargetID), err)
	}
	targetID := strings.TrimSpace(string(request.TargetID)) // swobu:io-string source=boundary
	next := make([]operatorclient.ProviderConfigData, 0, len(endpoint.ProviderConfigs))
	removedSelected := false
	removed := false
	routeID := strings.TrimSpace(string(request.RouteID)) // swobu:io-string source=boundary
	for _, config := range endpoint.ProviderConfigs {
		if !targetMatchesRoute(config, targetID, routeID) {
			next = append(next, config)
			continue
		}
		removed = true
		removedSelected = config.Ref == endpoint.SelectedRef
	}
	if !removed {
		return fmt.Errorf("delete target %q: target could not be resolved", targetID)
	}
	if len(next) == 0 {
		return adapterFailure(fmt.Sprintf("delete final target %q", targetID), ErrUnsupportedCommand)
	}
	endpoint.ProviderConfigs = next
	if removedSelected {
		endpoint.SelectedRef = next[0].Ref
	}
	if err := a.client.UpsertEndpoint(ctx, endpoint); err != nil {
		return adapterFailure(fmt.Sprintf("delete target %q", targetID), err)
	}
	return nil
}

// ExecuteRunCommand resolves a run command through the operator client-profile
// facade and executes it.
func (a *LiveOperatorAdapter) ExecuteRunCommand(ctx context.Context, request ports.ExecuteRunCommandRequest) (ports.RunExecutionResult, error) {
	if request.WorkspaceID == "" {
		return ports.RunExecutionResult{}, errors.New("workspace is required")
	}
	if request.RunCommandID == "" {
		return ports.RunExecutionResult{}, errors.New("run command is required")
	}
	workspace, err := a.LoadWorkspace(ctx, request.WorkspaceID)
	if err != nil {
		return ports.RunExecutionResult{}, adapterFailure(fmt.Sprintf("run command %q", request.RunCommandID), err)
	}
	modelID := string(request.RouteID)
	if modelID == "" && len(workspace.Routes) > 0 {
		modelID = workspace.Routes[0].ModelName
	}
	command, ok := clientprofile.ResolveRunCommand(string(request.RunCommandID), workspace.ClientBaseURL, modelID)
	if !ok {
		return ports.RunExecutionResult{}, adapterFailure(fmt.Sprintf("run command %q", request.RunCommandID), ErrUnsupportedCommand)
	}
	executor := a.runCommand
	if executor == nil {
		executor = func(ctx context.Context, command clientprofile.RunCommandSpec) error {
			return executeClientRunCommand(ctx, command, a.commandIO)
		}
	}
	if err := executor(ctx, command); err != nil {
		return ports.RunExecutionResult{}, adapterFailure(fmt.Sprintf("run command %q", request.RunCommandID), err)
	}
	return ports.RunExecutionResult{}, nil
}

var _ ports.WorkspaceQueries = (*LiveOperatorAdapter)(nil)
var _ ports.WorkspaceCommands = (*LiveOperatorAdapter)(nil)
var _ ports.RouteCommands = (*LiveOperatorAdapter)(nil)
var _ ports.RunExecutor = (*LiveOperatorAdapter)(nil)
var _ ports.ActivityQueries = (*LiveOperatorAdapter)(nil)
