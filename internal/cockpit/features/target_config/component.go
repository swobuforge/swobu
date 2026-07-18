package target_config

import (
	"context"
	"strings"

	"fmt"
	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/ports"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/cockpit/ui"
	"github.com/swobuforge/swobu/internal/domain/credentialref"
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

func credentialDisplay(w *TargetConfig) string {
	setup := w.setupState()
	if setup.CredentialLabel != "" && setup.ReadyForCatalog {
		if setup.CredentialLabel == setup.CredentialRef {
			return credentialRefDisplay(setup.CredentialRef)
		}
		return setup.CredentialLabel
	}
	return credentialRefDisplay(w.Draft.Get().CredentialRef)
}

func credentialRefDisplay(raw string) string {
	ref := strings.TrimSpace(raw)
	switch credentialref.Parse(ref).Kind() {
	case credentialref.KindSecret, credentialref.KindSecretFile:
		return "secret"
	default:
		return ref
	}
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

// TargetConfigChildOptionLabel preserves the parent row label gutter for
// nested option rows without rendering visible label text.
const TargetConfigChildOptionLabel = " "

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
	app              *tui.App
	catalogProbeSeq  int64
	operationContext context.Context
	cancelOperations context.CancelFunc
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
	}
}

// NewEditTargetConfig builds an idle target config seeded from an existing target.
// Edit mode intentionally reuses the add-target setup/catalog/model seam so
// provider/model changes cannot bypass setup projection.
func NewEditTargetConfig(workspaceID readmodel.WorkspaceID, route readmodel.RouteReadModel, target readmodel.TargetReadModel, save SaveTargetFunc, onClose func()) *TargetConfig {
	w := NewTargetConfig(workspaceID, route, save, onClose)
	w.mode = targetConfigModeEdit
	w.Target = target
	w.Draft.Set(TargetDraftFromReadModel(route.ID, target))
	w.BaseURL.Set(strings.TrimSpace(target.BaseURL))
	w.SelectedModel.Set(readmodel.ModelDeploymentReadModel{
		ID:                      target.Model,
		Name:                    target.Model,
		ModelName:               target.Model,
		DefaultProviderProtocol: target.ProviderProtocol,
	})
	w.Placement.Set(defaultPlacementForRoute(route))
	if w.Draft.Get().ProviderSpec != "" {
		w.refreshSetup()
	}
	return w
}

// Open transitions from Closed to ChoosingProvider.
func (w *TargetConfig) Open() {
	if w.Phase.Get() != PhaseClosed {
		return
	}
	if w.operationContext == nil || w.operationContext.Err() != nil {
		w.operationContext, w.cancelOperations = context.WithCancel(context.Background())
	}
	w.Error.Set("")
	if w.mode == targetConfigModeEdit && w.Draft.Get().ProviderSpec != "" && w.SelectedModel.Get().ModelName != "" {
		w.Phase.Set(PhaseReadyToCreate)
		return
	}
	w.Phase.Set(PhaseConfiguring)
}

// IsOpen reports whether the target config has left the Closed phase.
func (w *TargetConfig) IsOpen() bool {
	return w.Phase.Get() != PhaseClosed
}

// ShouldRenderTargetTail keeps the one global tail out of provider-owned auth
// stages, where model/protocol/create controls would misrepresent readiness.
func (w *TargetConfig) ShouldRenderTargetTail() bool {
	return w.Phase.Get() != PhaseAuthPending && w.Phase.Get() != PhaseAuthFailed
}

// Back handles only feature-owned state. Entered rows and pickers own their
// local Escape behavior through ui primitives.
func (w *TargetConfig) Back() bool {
	if w.Phase.Get() == PhaseClosed || w.Phase.Get() == PhaseCreated {
		return false
	}
	if w.DeleteArmed.Get() {
		w.DeleteArmed.Set(false)
		return true
	}
	if w.Phase.Get() == PhaseAuthPending {
		w.CancelAuthSession()
		return true
	}
	w.Close()
	return true
}

// Close forcibly closes the target config from any phase.
func (w *TargetConfig) Close() {
	w.Phase.Set(PhaseClosed)
	w.DeleteArmed.Set(false)
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
		w.BaseURL.Set(strings.TrimSpace(target.BaseURL))
		w.SelectedModel.Set(readmodel.ModelDeploymentReadModel{
			ID:                      target.Model,
			Name:                    target.Model,
			ModelName:               target.Model,
			DefaultProviderProtocol: target.ProviderProtocol,
		})
		w.Placement.Set(defaultPlacementForRoute(route))
		if w.Draft.Get().ProviderSpec != "" {
			w.refreshSetup()
		}
	}
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
}

// UnbindApp drops the live app handle. go-tui does not unbind State values, so
// they remain attached until the target config is rebound on a fresh app.
func (w *TargetConfig) UnbindApp() {
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
