package target_config

import (
	"context"
	"strings"

	"fmt"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/ports"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/cockpit/ui"
	"github.com/swobuforge/swobu/internal/profile"
)

func targetConfigTitle(w *TargetConfig) string {
	if strings.TrimSpace(w.Draft.Get().ProviderSpec) == "" {
		if w.mode == targetConfigModeEdit {
			return "edit target"
		}
		return "add target"
	}
	provider := strings.TrimSpace(providerDisplay(w))
	if provider == "" {
		provider = strings.TrimSpace(w.Draft.Get().ProviderSpec)
	}
	if w.mode == targetConfigModeEdit {
		return "edit target · " + provider
	}
	return "new target · " + provider
}

func targetConfigParentAction(w *TargetConfig) string {
	if strings.TrimSpace(w.Draft.Get().ProviderSpec) == "" {
		return "collapse ↵"
	}
	return "cancel ↵"
}

func TargetAddMountKey(w *TargetConfig, suffix string) string {
	if w.mode == targetConfigModeEdit {
		return fmt.Sprintf("target-edit:%s:%s:%s:%s", w.WorkspaceID, w.Route.ID, w.Target.ID, suffix)
	}
	return fmt.Sprintf("target-add:%s:%s:%s", w.WorkspaceID, w.Route.ID, suffix)
}

// SaveTargetFunc is the narrow command boundary for target creation.
type SaveTargetFunc func(context.Context, ports.SaveTargetRequest) (ports.SaveTargetResult, error)

type targetConfigMode int

const (
	targetConfigModeCreate targetConfigMode = iota
	targetConfigModeEdit
)

// appState (the reactive state shape), newStates, and bindApp live in state.go.

// TargetConfig owns the state machine for adding a target to a route. Provider-
// specific setup handlers build a TargetDraft-shaped snapshot before catalog
// probing and saving; durable validation remains in routing/configstore.
//
// The route section mounts the target config when add-target or target-edit is open
// and closes it when a target is saved or the operator escapes/cancels.
type TargetConfig struct {
	WorkspaceID readmodel.WorkspaceID
	Route       readmodel.RouteReadModel
	Target      readmodel.TargetReadModel
	mode        targetConfigMode

	appState

	// Ports + callbacks
	SaveTarget         SaveTargetFunc
	TargetSetupQueries ports.TargetSetupQueries
	TargetAuthCommands ports.TargetAuthCommands
	CredentialCommands ports.TargetCredentialCommands
	OnCreated          func(ports.SaveTargetResult)
	OnSaved            func(ports.SaveTargetResult)
	OnDeleteConfirmed  func() error
	OnClose            func()

	// Provider options are page input. Picker lifecycle is owned by mounted
	// components, not by the root feature.
	providerOptions []readmodel.ProviderOptionReadModel

	// All disclosure regions are gone: model + placement + provider are fresh
	// ui.Selects / reset-to-empty, and the credential drill-down lives on the
	// local component state (no disclosure control structs remain).
	app                   *tui.App
	catalogProbeSeq       int64
	catalogProbeInFlight  bool
	authObserverSeq       int64
	cancelAuthObserver    context.CancelFunc
	operationContext      context.Context
	cancelOperations      context.CancelFunc
	credentialReadDir     func(string) ([]ui.FileBrowserEntry, error)
	credentialInitialPath string
	credentialSlot        string
}

func (w *TargetConfig) actionContext() context.Context {
	if w.operationContext == nil {
		w.operationContext, w.cancelOperations = context.WithCancel(context.Background())
	}
	return w.operationContext
}

// NewTargetConfig builds an idle (closed) target config. Mount it by calling Open().
func NewTargetConfig(workspaceID readmodel.WorkspaceID, route readmodel.RouteReadModel, save SaveTargetFunc, onClose func()) *TargetConfig {
	st := newStates()
	st.Placement = tui.NewState(defaultPlacementForRoute(route))
	operationContext, cancelOperations := context.WithCancel(context.Background())
	return &TargetConfig{
		WorkspaceID:      workspaceID,
		Route:            route,
		appState:         st,
		SaveTarget:       save,
		OnClose:          onClose,
		operationContext: operationContext,
		cancelOperations: cancelOperations,
		credentialSlot:   newCredentialSlot(workspaceID, route.ID, ""),
	}
}

// NewEditTargetConfig builds an idle target config seeded from an existing target.
// Edit mode intentionally reuses the add-target setup/catalog/model seam so
// provider/model changes cannot bypass setup projection.
func NewEditTargetConfig(workspaceID readmodel.WorkspaceID, route readmodel.RouteReadModel, target readmodel.TargetReadModel, save SaveTargetFunc, onClose func()) *TargetConfig {
	w := NewTargetConfig(workspaceID, route, save, onClose)
	w.mode = targetConfigModeEdit
	w.Target = target
	w.credentialSlot = newCredentialSlot(workspaceID, route.ID, target.ID)
	w.Draft.Set(TargetDraftFromReadModel(route.ID, target))
	seedEndpointFromTarget(w, target)
	w.SelectedModel.Set(selectedModelSeedFromTarget(target))
	w.Placement.Set(defaultPlacementForRoute(route))
	if w.Draft.Get().ProviderSpec != "" {
		w.refreshSetup()
	}
	return w
}

// Open transitions the form from closed to open.
func (w *TargetConfig) Open() {
	if w.Lifecycle.Get() != LifecycleClosed {
		return
	}
	if w.operationContext == nil || w.operationContext.Err() != nil {
		w.operationContext, w.cancelOperations = context.WithCancel(context.Background())
	}
	w.Error.Set("")
	if w.mode == targetConfigModeEdit &&
		w.Draft.Get().ProviderSpec != "" &&
		w.SelectedModel.Get().ModelName != "" &&
		!w.IsZAIFlow() {
		w.Lifecycle.Set(LifecycleOpen)
		// Persisted catalog-backed edit values are selection seeds, not stale
		// capability evidence. Keep them visible until the current catalog either
		// hydrates or rejects them; setup changes use ReadyAndProbe's destructive
		// path for every discovery-capable provider. Z.AI keeps its authored
		// model authoritative without initiating catalog validation.
		w.startCatalogProbe()
		return
	}
	w.Lifecycle.Set(LifecycleOpen)
}

// IsOpen reports whether the target config has left the Closed phase.
func (w *TargetConfig) IsOpen() bool {
	return w.Lifecycle.Get() != LifecycleClosed
}

// ShouldRenderTargetTail keeps the one global tail out of provider-owned auth
// stages, where model/protocol/create controls would misrepresent readiness.
func (w *TargetConfig) ShouldRenderTargetTail() bool {
	return !w.authSessionPending() && !w.authSessionFailed()
}

// Back handles only feature-owned state. Entered rows and pickers own their
// local Escape behavior through ui primitives.
func (w *TargetConfig) Back() bool {
	if w.Lifecycle.Get() == LifecycleClosed || w.Lifecycle.Get() == LifecycleCreated {
		return false
	}
	if w.DeleteArmed.Get() {
		w.DeleteArmed.Set(false)
		return true
	}
	if w.authSessionPending() {
		w.CancelAuthSession()
		return true
	}
	w.Close()
	return true
}

// Close forcibly closes the target config from any phase.
func (w *TargetConfig) Close() {
	w.Lifecycle.Set(LifecycleClosed)
	w.DeleteArmed.Set(false)
	w.stopAuthSessionObserver()
	if w.cancelOperations != nil {
		w.cancelOperations()
	}
	if w.OnClose != nil {
		w.OnClose()
	}
}

// KeyMap returns back/cancel bindings when the target config is open.
func (w *TargetConfig) KeyMap() tui.KeyMap {
	if !w.IsOpen() {
		return nil
	}
	return ui.BackScope(w.IsOpen, func() { w.Back() })
}

// UpdateProps supports go-tui remounts. Production refresh paths should call
// UpdateRoute, UpdateTarget, UpdateProviderOptions, and UpdateCommands directly
// so construction and refresh stay separate.
func (w *TargetConfig) UpdateProps(fresh tui.Component) {
	f, ok := fresh.(*TargetConfig)
	if !ok {
		return
	}
	if f.mode == targetConfigModeEdit {
		w.UpdateTarget(f.WorkspaceID, f.Route, f.Target)
	} else {
		w.UpdateRoute(f.WorkspaceID, f.Route)
	}
	w.UpdateCommands(f.SaveTarget, f.TargetSetupQueries, f.TargetAuthCommands, f.CredentialCommands)
	w.OnCreated = f.OnCreated
	w.OnSaved = f.OnSaved
	w.OnDeleteConfirmed = f.OnDeleteConfirmed
	w.OnClose = f.OnClose
	w.UpdateProviderOptions(f.providerOptions)
}

// UpdateRoute refreshes the target config subject for create mode without resetting
// operator-entered provider, setup, catalog, or model state.
func (w *TargetConfig) UpdateRoute(workspaceID readmodel.WorkspaceID, route readmodel.RouteReadModel) {
	w.WorkspaceID = workspaceID
	w.Route = route
	w.Target = readmodel.TargetReadModel{}
	w.mode = targetConfigModeCreate
}

// UpdateTarget refreshes the target config subject for edit mode without resetting
// operator-entered provider, setup, catalog, or model state.
func (w *TargetConfig) UpdateTarget(workspaceID readmodel.WorkspaceID, route readmodel.RouteReadModel, target readmodel.TargetReadModel) {
	w.WorkspaceID = workspaceID
	w.Route = route
	w.Target = target
	w.mode = targetConfigModeEdit
	if strings.TrimSpace(w.Draft.Get().ProviderSpec) == "" {
		w.Draft.Set(TargetDraftFromReadModel(route.ID, target))
		seedEndpointFromTarget(w, target)
		w.SelectedModel.Set(selectedModelSeedFromTarget(target))
		w.Placement.Set(defaultPlacementForRoute(route))
		if w.Draft.Get().ProviderSpec != "" {
			w.refreshSetup()
		}
	}
}

// selectedModelSeedFromTarget keeps persisted selection distinct from model
// capability evidence. The target protocol is a default selection only;
// supported protocols remain sparse for profile resolution to own.
func selectedModelSeedFromTarget(target readmodel.TargetReadModel) readmodel.ModelDeploymentReadModel {
	return readmodel.ModelDeploymentReadModel{
		ID:                      target.Model,
		Name:                    target.Model,
		ModelName:               target.Model,
		DefaultProviderProtocol: target.ProviderProtocol,
	}
}

// seedEndpointFromTarget initializes the temporary editor buffer on edit entry.
// Bedrock keeps explicit-or-empty durable intent in Draft.Endpoint while the
// editor receives the protocol-aware effective API base.
func seedEndpointFromTarget(w *TargetConfig, target readmodel.TargetReadModel) {
	if profile.ProviderID(target.Provider) == profile.ProviderSpecBedrock {
		kind, _, _ := profile.ProviderProtocolKindAndFrame(target.Provider, target.ProviderProtocol)
		w.BaseURL.Set(profile.EffectiveBedrockAPIURL(target.BedrockRegion, target.BaseURL, kind))
		return
	}
	w.BaseURL.Set(strings.TrimSpace(target.BaseURL))
}

// UpdateProviderOptions refreshes static provider picker options from the page
// readmodel path.
func (w *TargetConfig) UpdateProviderOptions(opts []readmodel.ProviderOptionReadModel) {
	w.providerOptions = opts
}

// UpdateCommands refreshes effect ports for save, setup probing, and
// interactive auth without rebuilding target config state.
func (w *TargetConfig) UpdateCommands(save SaveTargetFunc, setup ports.TargetSetupQueries, auth ports.TargetAuthCommands, credentials ports.TargetCredentialCommands) {
	w.SaveTarget = save
	w.TargetSetupQueries = setup
	w.TargetAuthCommands = auth
	w.CredentialCommands = credentials
}

// BindApp wires the target config's reactive state fields to the live go-tui app
// so async updates can safely queue back onto the main loop.
func (w *TargetConfig) BindApp(app *tui.App) {
	if app == nil {
		return
	}
	w.app = app
	w.appState.bindApp(app)
	// Routes opens an edit component before GSX mounts it. Resume the probe
	// that Open marked pending now that async results can return through the
	// app update loop.
	w.launchPendingCatalogProbe()
	w.launchPendingAuthSessionObserver()
}

// UnbindApp drops the live app handle. go-tui does not unbind State values, so
// they remain attached until the target config is rebound on a fresh app.
func (w *TargetConfig) UnbindApp() {
	w.stopAuthSessionObserver()
	w.app = nil
}

func (w *TargetConfig) hasLiveApp() bool {
	if w.app == nil {
		return false
	}
	select {
	case <-w.app.StopCh():
		return false
	default:
		return true
	}
}
