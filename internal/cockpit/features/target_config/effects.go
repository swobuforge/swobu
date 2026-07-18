package target_config

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/swobuforge/swobu/internal/cockpit/ports"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/profile"
)

// draft_projection.go projects between the durable TargetDraft the cockpit
// edits/persists and the read/working shapes around it. Pure: no ports, no
// go-tui. Provider arms are opaque to these projections — each provider writes
// its own arm directly, so arms ride through unchanged.

// TargetDraftFromReadModel un-flattens the system wire/read shape into the typed
// draft the cockpit edits and persists. It is the read-boundary reverse of
// the workspace command projection into the local draft an edit session starts
// from.
//
// Provider-specific options return to their typed arm: AuthHeader is the
// OpenAI-compatible arm, AuthMode/Region/ProfileName are the Bedrock arm.
// Azure/ChatGPT/default carry everything on the spine and contribute no arm.
func TargetDraftFromReadModel(routeID readmodel.RouteID, t readmodel.TargetReadModel) readmodel.TargetDraft {
	spec := strings.TrimSpace(t.Provider) // swobu:io-string source=boundary
	endpointSpec, _ := profile.EndpointSpecForProvider(spec)
	d := readmodel.TargetDraft{
		ProviderSpec:     spec,
		Endpoint:         readmodel.ProviderEndpointDraft{Kind: endpointSpec.Kind, Value: strings.TrimSpace(t.BaseURL)},
		CredentialRef:    strings.TrimSpace(t.CredentialRef),
		ProviderProtocol: strings.TrimSpace(t.ProviderProtocol),
		ModelID:          strings.TrimSpace(t.Model),
		RouteModelID:     strings.TrimSpace(string(routeID)),
	}
	switch profile.ProviderID(spec) {
	case profile.ProviderSpecOpenAICompatible:
		d.ProviderOptions.OpenAICompatible.CredentialHeader = strings.TrimSpace(t.AuthHeader)
	case profile.ProviderSpecBedrock:
		d.ProviderOptions.Bedrock.AuthMode = strings.TrimSpace(t.AuthMode)
		d.ProviderOptions.Bedrock.Region = profile.BedrockMantleRegionFromEndpoint(t.BaseURL)
		d.ProviderOptions.Bedrock.ProfileName = profile.BedrockProfileNameFromCredentialRef(t.CredentialRef)
	}
	return d
}

// currentTargetDraft projects the working Draft into the save-boundary draft,
// applying only the spine fields still held as UI state (endpoint value,
// selected model, placement). Provider arms are opaque to this projection —
// each provider writes its own arm directly, so it rides through unchanged.
func (w *TargetConfig) currentTargetDraft(modelID string, providerProtocol string, placement readmodel.PlacementOptionReadModel) readmodel.TargetDraft {
	draft := w.Draft.Get() // provider arms ride through, owned by their providers
	endpointSpec, _ := profile.EndpointSpecForProvider(draft.ProviderSpec)
	draft.Endpoint.Kind = endpointSpec.Kind
	draft.Endpoint.Value = strings.TrimSpace(w.BaseURL.Get()) // swobu:io-string source=boundary
	draft.ProviderProtocol = strings.TrimSpace(providerProtocol)
	draft.ModelID = strings.TrimSpace(modelID)
	draft.RouteModelID = strings.TrimSpace(string(w.Route.ID)) // swobu:io-string source=boundary
	_ = placement
	return draft
}

func (w *TargetConfig) actionContext() context.Context {
	if w.operationContext == nil {
		w.operationContext, w.cancelOperations = context.WithCancel(context.Background())
	}
	return w.operationContext
}

// effects.go owns target_config product transitions and all calls to cockpit
// ports. UI primitives call these methods; no effect owns entered-row state.

// SetCatalogResult updates the catalog result and transitions to ChoosingModel
// or CatalogFailed.
func (w *TargetConfig) SetCatalogResult(result readmodel.ModelCatalogReadModel) {
	if w.IsAzureFlow() {
		result.Error = azureCatalogOperatorError(result)
	}
	w.Catalog.Set(result)
	w.CatalogLoading.Set(false)
	if result.Error != "" {
		w.Error.Set(result.Error)
		w.Phase.Set(PhaseCatalogFailed)
		return
	}
	w.Error.Set("")
	if w.mode == targetConfigModeEdit && w.hydrateSelectedModel(result.Deployments) {
		w.Phase.Set(PhaseReadyToCreate)
		return
	}
	if w.IsAzureFlow() && len(result.Deployments) == 0 {
		w.Error.Set("none found")
		w.Phase.Set(PhaseCatalogFailed)
		return
	}
	w.Phase.Set(PhaseConfiguring)
}

func (w *TargetConfig) hydrateSelectedModel(deployments []readmodel.ModelDeploymentReadModel) bool {
	selected := w.SelectedModel.Get()
	if strings.TrimSpace(selected.ModelName) == "" {
		return false
	}
	for _, deployment := range deployments {
		if deployment.ID != selected.ID && deployment.ModelName != selected.ModelName {
			continue
		}
		w.SelectedModel.Set(deployment)
		options := resolveProtocolOptions(w.Draft.Get().ProviderSpec, deployment)
		for _, option := range options {
			if option.ID == w.Draft.Get().ProviderProtocol {
				return true
			}
		}
		return false
	}
	return false
}

// SelectModel commits a model choice and advances to the explicit protocol
// decision required before target creation.
func (w *TargetConfig) SelectModel(model readmodel.ModelDeploymentReadModel) {
	w.SelectedModel.Set(model)
	w.Draft.Update(func(d readmodel.TargetDraft) readmodel.TargetDraft { d.ProviderProtocol = ""; return d })
	options := resolveProtocolOptions(w.Draft.Get().ProviderSpec, model)
	w.Error.Set("")
	switch len(options) {
	case 0:
		w.Error.Set("no supported protocol for selected model")
		w.Phase.Set(PhaseReadyToCreate)
	case 1:
		w.Draft.Update(func(d readmodel.TargetDraft) readmodel.TargetDraft {
			d.ProviderProtocol = options[0].ID
			return d
		})
		w.Phase.Set(PhaseReadyToCreate)
		w.CommitEdit(w.actionContext())
	default:
		// Multiple protocols: the protocol ui.Select row becomes enterable; the
		// operator opens it manually (no auto-open).
		w.Phase.Set(PhaseReadyToCreate)
	}
}

// ProbeCatalog resolves the catalog for the current provider and advances
// the phase. If TargetSetupQueries is set it calls the daemon; otherwise
// it falls back to the static catalog for Tier 1-2 providers.
func (w *TargetConfig) ProbeCatalog() {
	if w.Phase.Get() != PhaseLoadingCatalog {
		return
	}
	draft := w.currentTargetDraft("", "", w.Placement.Get())
	result := probeCatalogSnapshot(
		w.actionContext(),
		w.TargetSetupQueries,
		draft,
	)
	w.SetCatalogResult(result)
}

// ReadyAndProbe commits the projected setup inputs, enters LoadingCatalog,
// and probes the catalog asynchronously when a live app exists.
// Callers without a live app must drive ProbeCatalog themselves after the
// loading state is visible.
func (w *TargetConfig) ReadyAndProbe(credentialRef, baseURL string) {
	credentialRef = strings.TrimSpace(credentialRef) // swobu:io-string source=boundary
	baseURL = strings.TrimSpace(baseURL)             // swobu:io-string source=boundary
	if baseURL == "" {
		baseURL = profile.DefaultExecuteBaseURL(w.Draft.Get().ProviderSpec)
	}
	w.Draft.Update(func(d readmodel.TargetDraft) readmodel.TargetDraft {
		d.CredentialRef = credentialRef
		return d
	})
	w.BaseURL.Set(baseURL)
	setup := w.refreshSetup()
	if !setup.ReadyForCatalog {
		w.CatalogLoading.Set(false)
		w.Phase.Set(PhaseConfiguring)
		return
	}
	w.CatalogLoading.Set(true)
	w.Error.Set("")
	w.Phase.Set(PhaseLoadingCatalog)
	if w.hasLiveApp() {
		w.catalogProbeSeq++
		seq := w.catalogProbeSeq
		app := w.app
		draft := w.currentTargetDraft("", "", w.Placement.Get())
		queries := w.TargetSetupQueries
		ctx := w.actionContext()
		go func() {
			result := probeCatalogSnapshot(ctx, queries, draft)
			if app == nil {
				return
			}
			app.QueueUpdate(func() {
				if seq != w.catalogProbeSeq || w.Phase.Get() != PhaseLoadingCatalog {
					return
				}
				w.SetCatalogResult(result)
			})
		}()
		return
	}
}

// ContinueSetup advances from provider-specific setup when the current draft
// snapshot is ready, or starts an interactive auth branch such as ChatGPT.
func (w *TargetConfig) ContinueSetup() {
	if w.Phase.Get() != PhaseConfiguring {
		return
	}
	setup := w.refreshSetup()
	if setup.InteractiveAuth {
		w.startInteractiveAuth()
		return
	}
	if setup.ReadyForCatalog {
		w.ReadyAndProbe(setup.CredentialRef, w.BaseURL.Get())
		return
	}
	w.Phase.Set(PhaseConfiguring)
}

// ChangeProvider returns to the empty provider picker (RFC Phase 8). Changing
// provider is a reset, not a disclosure toggle: the draft provider clears, setup
// state resets, and the view shows the provider picker (ProviderEmpty). Picking
// a new provider re-seeds defaults; Escape from the empty picker abandons.
func (w *TargetConfig) ChangeProvider() {
	if !w.IsOpen() {
		return
	}
	if w.mode == targetConfigModeEdit {
		return
	}
	w.resetFlowState()
	w.Draft.Update(func(d readmodel.TargetDraft) readmodel.TargetDraft { d.ProviderSpec = ""; return d })
	w.Phase.Set(PhaseConfiguring)
}

// seedSetupDefaults seeds only parent-owned lifecycle trivia: the default
// endpoint for providers that do not require an explicit one. Provider arms are
// opaque to the parent — each provider seeds its own defaults (e.g. the
// openai-compatible header defaults at the domain boundary when empty).
func (w *TargetConfig) seedSetupDefaults() {
	spec := w.Draft.Get().ProviderSpec
	if spec == "" {
		return
	}
	if w.BaseURL.Get() == "" && !profile.RequiresExplicitEndpoint(spec) {
		if defaultBaseURL := profile.DefaultExecuteBaseURL(spec); defaultBaseURL != "" {
			w.BaseURL.Set(defaultBaseURL)
		}
	}
}

func (w *TargetConfig) refreshSetup() providerSetupState {
	return w.setupState()
}

func (w *TargetConfig) advanceFromSetup() {
	seedProviderDefaults(w)
	setup := w.refreshSetup()
	if setup.ReadyForCatalog {
		w.ReadyAndProbe(setup.CredentialRef, w.BaseURL.Get())
		return
	}
	w.Phase.Set(PhaseConfiguring)
}

// RetryCatalog re-enters LoadingCatalog from CatalogFailed, reusing setup inputs.
func (w *TargetConfig) RetryCatalog() {
	if w.Phase.Get() != PhaseCatalogFailed {
		return
	}
	w.Error.Set("")
	w.Phase.Set(PhaseConfiguring)
	w.ContinueSetup()
}

func probeCatalogSnapshot(ctx context.Context, queries ports.TargetSetupQueries, draft readmodel.TargetDraft) readmodel.ModelCatalogReadModel {
	provider := strings.TrimSpace(draft.ProviderSpec)
	if queries != nil {
		ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		providerProtocol, _ := profile.ResolveConcreteProtocolForAutoAtBoundary(provider)
		result, err := queries.ProbeProviderModels(ctx, ports.ProbeProviderModelsRequest{
			ProviderSpec:     provider,
			BaseURL:          strings.TrimSpace(draft.Endpoint.Value),
			AuthHeader:       resolvedCredentialHeader(provider, draft.ProviderOptions.OpenAICompatible.CredentialHeader),
			CredentialRef:    strings.TrimSpace(draft.CredentialRef),
			AuthMode:         strings.TrimSpace(draft.ProviderOptions.Bedrock.AuthMode),
			ProviderProtocol: providerProtocol,
		})
		if err != nil {
			return readmodel.ModelCatalogReadModel{Error: err.Error()}
		}
		return result
	}
	// Static catalog fallback: tier 1-2 providers only.
	deployments := staticCatalogFallback(provider)
	if len(deployments) == 0 {
		return readmodel.ModelCatalogReadModel{
			Error: "no model catalog for provider " + provider + ". try manual entry.",
		}
	}
	return readmodel.ModelCatalogReadModel{
		Deployments:              deployments,
		ResolvedProviderProtocol: deployments[0].DefaultProviderProtocol,
	}
}

// ---------------------------------------------------------------------------
// Interactive auth (browser/device) — e.g. ChatGPT. The target config drives
// the daemon auth session through ports.TargetAuthCommands and projects session
// state onto Phase/Error.
// ---------------------------------------------------------------------------

func authModeDaemonName(mode profile.AuthMode) string {
	switch mode {
	case profile.AuthModeChatGPTLogin:
		return "browser"
	case profile.AuthModeChatGPTDeviceAuth:
		return "device"
	default:
		return strings.TrimSpace(string(mode)) // swobu:io-string source=boundary
	}
}

func authSubjectLocator(w *TargetConfig) string {
	return fmt.Sprintf("subject:%s#%s", strings.TrimSpace(string(w.WorkspaceID)), strings.TrimSpace(string(w.Route.ID)))
}

func (w *TargetConfig) startInteractiveAuth() {
	mode, _ := w.interactiveAuthMode()
	if mode == "" {
		w.Error.Set("interactive auth is unavailable for provider " + w.Draft.Get().ProviderSpec)
		w.Phase.Set(PhaseAuthFailed)
		return
	}
	if w.TargetAuthCommands == nil {
		w.Error.Set("auth session commands are not wired yet")
		w.Phase.Set(PhaseAuthFailed)
		return
	}
	ctx, cancel := context.WithTimeout(w.actionContext(), 10*time.Second)
	defer cancel()
	if w.Phase.Get() == PhaseAuthFailed {
		if sessionID := strings.TrimSpace(w.AuthSession.Get().SessionID); sessionID != "" {
			session, err := w.TargetAuthCommands.RetryAuthSession(ctx, sessionID)
			if err != nil {
				w.Error.Set(err.Error())
				w.Phase.Set(PhaseAuthFailed)
				return
			}
			w.applyAuthSessionResult(session)
			return
		}
	}
	session, err := w.TargetAuthCommands.StartAuthSession(ctx, ports.StartAuthSessionRequest{
		ProviderSpec: w.Draft.Get().ProviderSpec,
		Workspace:    string(w.WorkspaceID),
		Route:        string(w.Route.ID),
		TargetID:     string(w.Target.ID),
		DraftSubject: func() string {
			if w.Target.ID == "" {
				return authSubjectLocator(w)
			}
			return ""
		}(),
		AuthMode: authModeDaemonName(mode),
	})
	if err != nil {
		w.Error.Set(err.Error())
		w.Phase.Set(PhaseAuthFailed)
		return
	}
	w.applyAuthSessionResult(session)
}

func (w *TargetConfig) RefreshAuthSession() {
	if w.Phase.Get() != PhaseAuthPending {
		return
	}
	session := w.AuthSession.Get()
	if strings.TrimSpace(session.SessionID) == "" {
		return
	}
	if w.TargetAuthCommands == nil {
		w.Error.Set("auth session commands are not wired yet")
		w.Phase.Set(PhaseAuthFailed)
		return
	}
	ctx, cancel := context.WithTimeout(w.actionContext(), 10*time.Second)
	defer cancel()
	result, err := w.TargetAuthCommands.PollAuthSession(ctx, session.SessionID)
	if err != nil {
		w.Error.Set(err.Error())
		w.Phase.Set(PhaseAuthFailed)
		return
	}
	w.applyAuthSessionResult(result)
}

func (w *TargetConfig) CancelAuthSession() {
	session := w.AuthSession.Get()
	if w.TargetAuthCommands != nil && strings.TrimSpace(session.SessionID) != "" {
		ctx, cancel := context.WithTimeout(w.actionContext(), 10*time.Second)
		_ = w.TargetAuthCommands.CancelAuthSession(ctx, session.SessionID)
		cancel()
	}
	w.resetFlowState()
	w.Phase.Set(PhaseConfiguring)
}

func (w *TargetConfig) applyAuthSessionResult(result readmodel.AuthSessionReadModel) {
	w.AuthSession.Set(result)
	switch strings.ToLower(strings.TrimSpace(result.State)) {
	case "", "pending":
		w.Error.Set("")
		w.Phase.Set(PhaseAuthPending)
	case "succeeded":
		credentialRef := strings.TrimSpace(result.CredentialRef)
		if credentialRef == "" {
			w.Error.Set("auth session succeeded without credential ref")
			w.Phase.Set(PhaseAuthFailed)
			return
		}
		w.SetSetupReady(credentialRef, "")
	case "canceled":
		w.resetFlowState()
		w.Phase.Set(PhaseConfiguring)
	case "expired", "failed":
		msg := strings.TrimSpace(result.ErrorMessage)
		if msg == "" {
			msg = "auth session " + strings.TrimSpace(result.State)
		}
		w.Error.Set(msg)
		w.Phase.Set(PhaseAuthFailed)
	default:
		msg := strings.TrimSpace(result.ErrorMessage)
		if msg == "" {
			msg = "auth session " + strings.TrimSpace(result.State)
		}
		w.Error.Set(msg)
		w.Phase.Set(PhaseAuthFailed)
	}
}

// ---------------------------------------------------------------------------
// Save / create / edit-commit side-effects.
// ---------------------------------------------------------------------------

// Create attempts to persist the target.
func (w *TargetConfig) Create(ctx context.Context) {
	if w.SaveTarget == nil {
		w.Error.Set("target save is not wired yet")
		w.Phase.Set(PhaseCreateFailed)
		return
	}
	model := w.SelectedModel.Get()
	placement := w.Placement.Get()
	protocol := strings.TrimSpace(w.Draft.Get().ProviderProtocol)
	if protocol == "" {
		w.Error.Set("protocol first")
		w.Phase.Set(PhaseReadyToCreate)
		return
	}
	req := ports.SaveTargetRequest{
		WorkspaceID: w.WorkspaceID,
		RouteID:     w.Route.ID,
		TargetID:    w.Target.ID,
		Draft:       w.currentTargetDraft(model.ModelName, protocol, placement),
		Placement:   placement,
	}
	saved, err := w.SaveTarget(ctx, req)
	if err != nil {
		w.Error.Set(err.Error())
		w.Phase.Set(PhaseCreateFailed)
		return
	}
	w.Error.Set("")
	w.Phase.Set(PhaseCreated)
	if w.OnCreated != nil {
		w.OnCreated(saved)
	}
	if w.OnClose != nil {
		w.OnClose()
	}
}

// CommitEdit persists the current row-level target facts for an existing target.
// Create mode remains draft-only until the operator activates CreateControl.
func (w *TargetConfig) CommitEdit(ctx context.Context) {
	if w.mode != targetConfigModeEdit {
		return
	}
	if w.SaveTarget == nil {
		w.Error.Set("target save is not wired yet")
		return
	}
	model := w.SelectedModel.Get()
	modelID := strings.TrimSpace(model.ModelName)
	protocol := strings.TrimSpace(w.Draft.Get().ProviderProtocol)
	if modelID == "" || protocol == "" {
		return
	}
	saved, err := w.SaveTarget(ctx, ports.SaveTargetRequest{
		WorkspaceID: w.WorkspaceID,
		RouteID:     w.Route.ID,
		TargetID:    w.Target.ID,
		Draft:       w.currentTargetDraft(modelID, protocol, w.Placement.Get()),
		Placement:   w.Placement.Get(),
	})
	if err != nil {
		w.Error.Set(err.Error())
		return
	}
	w.Target = saved.Target
	w.Route = saved.Route
	w.Error.Set("")
	if w.OnSaved != nil {
		w.OnSaved(saved)
	}
}
