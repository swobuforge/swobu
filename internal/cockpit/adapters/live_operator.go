package adapters

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"

	operatorclient "github.com/swobuforge/swobu/internal/app/operator/client"
	clientprofile "github.com/swobuforge/swobu/internal/app/operator/clientprofile"
	"github.com/swobuforge/swobu/internal/cockpit/ports"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/platform/config"
	"github.com/swobuforge/swobu/internal/profile"
)

type operatorClient interface {
	ListEndpoints(context.Context) ([]operatorclient.EndpointData, error)
	GetEndpoint(context.Context, string) (operatorclient.EndpointData, error)
	UpsertEndpoint(context.Context, operatorclient.EndpointData) error
	DeleteEndpoint(context.Context, string) error
	Status(context.Context, string) (operatorclient.StatusProjection, error)
	StartAuthSession(context.Context, string, string, string) (operatorclient.AuthSessionStartResult, error)
	GetAuthSessionStatus(context.Context, string) (operatorclient.AuthSessionStatusResult, error)
	CancelAuthSession(context.Context, string) error
	RetryAuthSession(context.Context, string) (operatorclient.AuthSessionRetryResult, error)
	ProbeModelCatalog(context.Context, string, string, string, string, string) (operatorclient.ModelCatalogResult, error)
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
//
// Draft workspace creation stays local until the first real route/target save
// can materialize a valid endpoint. The daemon endpoint domain requires at
// least one provider config, so a blank slug submit must not synthesize an
// invalid endpoint intent.
func (a *LiveOperatorAdapter) SaveWorkspace(ctx context.Context, request ports.SaveWorkspaceRequest) (readmodel.WorkspaceReadModel, error) {
	slug := strings.TrimSpace(request.Slug) // swobu:io-string source=boundary
	if slug == "" {
		return readmodel.WorkspaceReadModel{}, errors.New("save workspace: workspace slug is required")
	}
	if request.ID == "" || request.ID == "+" {
		return a.workspaceFromEndpoint(ctx, operatorclient.EndpointData{Name: slug})
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
	return a.workspaceFromEndpoint(ctx, current)
}

// DeleteWorkspace removes one daemon endpoint.
func (a *LiveOperatorAdapter) DeleteWorkspace(ctx context.Context, request ports.DeleteWorkspaceRequest) error {
	if err := a.client.DeleteEndpoint(ctx, string(request.ID)); err != nil {
		return adapterFailure(fmt.Sprintf("delete workspace %q", request.ID), err)
	}
	return nil
}

// SaveRoute renames the projected route group backed by matching provider
// configs. Cockpit groups provider configs by client-visible route model name
// and rewrites that route identity in place. Creating an empty route,
// persisting an enabled flag, or changing routing policy without targets is
// therefore not representable here yet.
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
		endpoint.ProviderConfigs[i].RouteModelID = modelName
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
	workspaceID := strings.TrimSpace(string(request.WorkspaceID)) // swobu:io-string source=boundary
	if workspaceID == "" {
		return readmodel.TargetReadModel{}, errors.New("save target: workspace is required")
	}
	endpoint, err := a.client.GetEndpoint(ctx, workspaceID)
	if err != nil {
		if targetID := strings.TrimSpace(string(request.TargetID)); targetID != "" || !operatorclient.IsNotFound(err) {
			return readmodel.TargetReadModel{}, adapterFailure(fmt.Sprintf("save target %q", request.TargetID), err)
		}
		endpoint = operatorclient.EndpointData{Name: workspaceID}
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
var _ ports.TargetSetupQueries = (*LiveOperatorAdapter)(nil)
var _ ports.TargetAuthCommands = (*LiveOperatorAdapter)(nil)

// ---------------------------------------------------------------------------
// TargetSetupQueries
// ---------------------------------------------------------------------------

// ListTargetProviders returns provider options from the profile catalog
// where VisibleInOperatorUI is true.
func (a *LiveOperatorAdapter) ListTargetProviders(ctx context.Context) ([]readmodel.ProviderOptionReadModel, error) {
	profiles := profile.All()
	var opts []readmodel.ProviderOptionReadModel
	sort.Slice(profiles, func(i, j int) bool {
		return profiles[i].ProviderDisplayName < profiles[j].ProviderDisplayName
	})
	for _, p := range profiles {
		if !p.VisibleInOperatorUI {
			continue
		}
		opts = append(opts, readmodel.ProviderOptionReadModel{
			ProviderSpec: string(p.ProviderID),
			DisplayName:  p.ProviderDisplayName,
			SetupHint:    p.SetupHint,
		})
	}
	return opts, nil
}

// ResolveProviderSetup projects the provider-setup facts from the provider
// profile catalog and current cockpit inputs.
func (a *LiveOperatorAdapter) ResolveProviderSetup(ctx context.Context, req ports.ResolveProviderSetupRequest) (readmodel.ProviderSetupReadModel, error) {
	_ = ctx
	spec := strings.TrimSpace(req.ProviderSpec) // swobu:io-string source=boundary
	if spec == "" {
		return readmodel.ProviderSetupReadModel{}, errors.New("provider is required")
	}
	provider, ok := providerProfileForSpec(spec)
	if !ok {
		return readmodel.ProviderSetupReadModel{}, fmt.Errorf("provider %q is unsupported", spec)
	}

	baseURL := strings.TrimSpace(req.BaseURL) // swobu:io-string source=boundary
	defaultBaseURL := strings.TrimSpace(provider.DefaultBaseURL)
	if baseURL == "" && !profile.RequiresExplicitExecuteBaseURL(spec) {
		baseURL = defaultBaseURL
	}
	credentialRef := strings.TrimSpace(req.CredentialRef) // swobu:io-string source=boundary
	defaultEnvKey := strings.TrimSpace(provider.DefaultCredentialEnvVar)
	if credentialRef == "" && defaultEnvKey != "" {
		credentialRef = "env:" + defaultEnvKey
	}

	authModes := providerSetupAuthModes(provider.AllowedAuthModes)
	setup := readmodel.ProviderSetupReadModel{
		ProviderSpec:       spec,
		DisplayName:        provider.ProviderDisplayName,
		DefaultBaseURL:     defaultBaseURL,
		DefaultAuthHeader:  strings.TrimSpace(provider.DefaultAuthHeader),
		CredentialRef:      credentialRef,
		CredentialRequired: profile.RequiresCredential(spec, baseURL),
		InteractiveAuth:    providerSetupHasInteractiveAuth(authModes),
		AuthModes:          authModes,
		RequiresBaseURL:    profile.RequiresExplicitExecuteBaseURL(spec) && strings.TrimSpace(req.BaseURL) == "",
	}

	if setup.InteractiveAuth {
		setup.CredentialLabel = providerSetupInteractiveLabel(authModes)
		if setup.CredentialLabel == "" {
			setup.CredentialLabel = "browser login"
		}
		setup.BlockReason = "auth first"
		setup.ReadyForCatalog = false
		return setup, nil
	}

	if setup.RequiresBaseURL {
		setup.CredentialLabel = "enter base URL"
		setup.BlockReason = "enter base URL"
		setup.ReadyForCatalog = false
		return setup, nil
	}

	if setup.CredentialRequired {
		if ok := providerSetupCredentialReady(spec, credentialRef); !ok {
			missing := defaultEnvKey
			if missing == "" {
				missing = "credential"
			}
			setup.CredentialLabel = "missing " + missing
			setup.BlockReason = setup.CredentialLabel
			setup.ReadyForCatalog = false
			return setup, nil
		}
	}

	if setup.CredentialLabel == "" {
		setup.CredentialLabel = credentialRef
	}
	setup.ReadyForCatalog = true
	return setup, nil
}

// ProbeProviderModels calls the daemon model catalog endpoint.
func (a *LiveOperatorAdapter) ProbeProviderModels(ctx context.Context, req ports.ProbeProviderModelsRequest) (readmodel.ModelCatalogReadModel, error) {
	result, err := a.client.ProbeModelCatalog(ctx, req.ProviderSpec, req.BaseURL, req.AuthHeader, req.CredentialRef, req.ProviderProtocol)
	if err != nil {
		return readmodel.ModelCatalogReadModel{}, adapterFailure("probe model catalog", err)
	}
	var deployments []readmodel.ModelDeploymentReadModel
	for _, d := range result.Deployments {
		deployments = append(deployments, readmodel.ModelDeploymentReadModel{
			ID:                         d.Name,
			Name:                       d.Name,
			ModelName:                  d.ModelName,
			ModelPublisher:             d.ModelPublisher,
			ModelVersion:               d.ModelVersion,
			Family:                     d.Family,
			DefaultProviderProtocol:    d.DefaultProviderProtocol,
		})
	}
	return readmodel.ModelCatalogReadModel{
		Deployments: deployments,
		ResolvedProviderProtocol: result.ResolvedProviderProtocol,
		Error: result.Error,
	}, nil
}

// ---------------------------------------------------------------------------
// TargetAuthCommands
// ---------------------------------------------------------------------------

// StartAuthSession starts an interactive auth session on the daemon.
func (a *LiveOperatorAdapter) StartAuthSession(ctx context.Context, req ports.StartAuthSessionRequest) (readmodel.AuthSessionReadModel, error) {
	result, err := a.client.StartAuthSession(ctx, req.ProviderSpec, req.EndpointRef, req.AuthMode)
	if err != nil {
		return readmodel.AuthSessionReadModel{}, adapterFailure("start auth session", err)
	}
	return readmodel.AuthSessionReadModel{
		ProviderSpec:  result.ProviderSpec,
		SessionID:     result.SessionID,
		AuthorizeURL:  result.AuthorizeURL,
		UserCode:      result.UserCode,
		State:         result.State,
	}, nil
}

// PollAuthSession polls daemon for auth session status.
func (a *LiveOperatorAdapter) PollAuthSession(ctx context.Context, sessionID string) (readmodel.AuthSessionReadModel, error) {
	result, err := a.client.GetAuthSessionStatus(ctx, sessionID)
	if err != nil {
		return readmodel.AuthSessionReadModel{}, adapterFailure("poll auth session", err)
	}
	return readmodel.AuthSessionReadModel{
		ProviderSpec:  result.ProviderSpec,
		SessionID:     result.SessionID,
		State:         result.State,
		CredentialRef: result.CredentialRef,
		ErrorMessage:  result.ErrorMessage,
	}, nil
}

// CancelAuthSession cancels the auth session on the daemon.
func (a *LiveOperatorAdapter) CancelAuthSession(ctx context.Context, sessionID string) error {
	if err := a.client.CancelAuthSession(ctx, sessionID); err != nil {
		return adapterFailure("cancel auth session", err)
	}
	return nil
}

// RetryAuthSession retries a failed auth session.
func (a *LiveOperatorAdapter) RetryAuthSession(ctx context.Context, sessionID string) (readmodel.AuthSessionReadModel, error) {
	result, err := a.client.RetryAuthSession(ctx, sessionID)
	if err != nil {
		return readmodel.AuthSessionReadModel{}, adapterFailure("retry auth session", err)
	}
	return readmodel.AuthSessionReadModel{
		ProviderSpec:  "",
		SessionID:     result.SessionID,
		AuthorizeURL:  result.AuthorizeURL,
		UserCode:      result.UserCode,
		State:         result.State,
	}, nil
}

func providerProfileForSpec(spec string) (profile.Profile, bool) {
	for _, candidate := range profile.All() {
		if string(candidate.ProviderID) == spec {
			return candidate, true
		}
	}
	return profile.Profile{}, false
}

func providerSetupAuthModes(modes []profile.AuthModeSpec) []readmodel.AuthModeReadModel {
	out := make([]readmodel.AuthModeReadModel, 0, len(modes))
	for _, mode := range modes {
		out = append(out, readmodel.AuthModeReadModel{
			Mode:        string(mode.Mode),
			Kind:        string(mode.Kind),
			Requirement: string(mode.Requirement),
			Interactive: mode.Interactive,
		})
	}
	return out
}

func providerSetupHasInteractiveAuth(modes []readmodel.AuthModeReadModel) bool {
	for _, mode := range modes {
		if mode.Interactive {
			return true
		}
	}
	return false
}

func providerSetupInteractiveLabel(modes []readmodel.AuthModeReadModel) string {
	for _, mode := range modes {
		if !mode.Interactive {
			continue
		}
		switch strings.TrimSpace(strings.ToLower(mode.Mode)) {
		case string(profile.AuthModeChatGPTLogin):
			return "browser login"
		case string(profile.AuthModeChatGPTDeviceAuth):
			return "device auth"
		default:
			if mode.Mode != "" {
				return mode.Mode
			}
		}
	}
	return ""
}

func providerSetupCredentialReady(providerSpec string, credentialRef string) bool {
	ref := strings.TrimSpace(credentialRef) // swobu:io-string source=boundary
	if ref == "" {
		return false
	}
	envKey, ok := providerSetupEnvCredentialName(providerSpec, ref)
	if !ok {
		return true
	}
	if envKey == "" {
		return false
	}
	val, ok := os.LookupEnv(envKey)
	return ok && strings.TrimSpace(val) != ""
}

func providerSetupEnvCredentialName(providerSpec string, credentialRef string) (string, bool) {
	ref := strings.TrimSpace(credentialRef) // swobu:io-string source=boundary
	if ref == "" {
		defaultKey := strings.TrimSpace(profile.DefaultEnvKeyForSpec(providerSpec))
		return defaultKey, defaultKey != ""
	}
	lower := strings.ToLower(ref) // swobu:io-string source=boundary
	switch {
	case lower == "env":
		defaultKey := strings.TrimSpace(profile.DefaultEnvKeyForSpec(providerSpec))
		return defaultKey, defaultKey != ""
	case lower == "env:":
		defaultKey := strings.TrimSpace(profile.DefaultEnvKeyForSpec(providerSpec))
		return defaultKey, defaultKey != ""
	case strings.HasPrefix(lower, "env:"):
		return strings.TrimSpace(ref[len("env:"):]), true
	default:
		return "", false
	}
}
