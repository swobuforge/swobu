package target_add

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"time"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/ports"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/cockpit/ui"
	"github.com/swobuforge/swobu/internal/profile"
)

// ---------------------------------------------------------------------------
// Options
// ---------------------------------------------------------------------------

// WorkflowOption configures a Workflow at construction time.
type WorkflowOption func(*Workflow)

// WithProviderOptions sets the provider list shown in the provider picker.
func WithProviderOptions(opts []readmodel.ProviderOptionReadModel) WorkflowOption {
	return func(w *Workflow) {
		w.providerOptions = opts
	}
}

// ---------------------------------------------------------------------------
// Save func
// ---------------------------------------------------------------------------

// SaveTargetFunc is the narrow command boundary for target creation.
type SaveTargetFunc func(context.Context, ports.SaveTargetRequest) (readmodel.TargetReadModel, error)

// ---------------------------------------------------------------------------
// Workflow
// ---------------------------------------------------------------------------

// Workflow owns the state machine for adding a target to a route, including
// the projected provider-setup facts and the manual catalog-failure fallback.
//
// The route section mounts the workflow when add-target is open and closes it
// when a target is created or the operator escapes/cancels.
type Workflow struct {
	WorkspaceID readmodel.WorkspaceID
	Route       readmodel.RouteReadModel
	Phase       *tui.State[Phase]
	Error       *tui.State[string]

	// Provider selection
	Provider *tui.State[string] // provider spec

	// Setup/auth (populated after provider selection)
	CredentialRef   *tui.State[string]
	BaseURL         *tui.State[string]
	ReadyForCatalog *tui.State[bool]
	ProviderSetup   *tui.State[readmodel.ProviderSetupReadModel]
	AuthSession     *tui.State[readmodel.AuthSessionReadModel]

	// Catalog
	Catalog        *tui.State[readmodel.ModelCatalogReadModel]
	CatalogLoading *tui.State[bool]

	// Model selection
	SelectedModel *tui.State[readmodel.ModelDeploymentReadModel]
	ManualModelID *tui.State[string]

	// Placement
	Placement *tui.State[readmodel.PlacementOptionReadModel]

	// Ports + callbacks
	SaveTarget         SaveTargetFunc
	TargetSetupQueries ports.TargetSetupQueries
	TargetAuthCommands ports.TargetAuthCommands
	OnCreated          func(readmodel.TargetReadModel)
	OnClose            func()

	// Cached pickers + options
	providerOptions      []readmodel.ProviderOptionReadModel
	providerPickerCache  *ui.SearchPicker
	modelPickerCache     *ui.SearchPicker
	placementPickerCache []readmodel.PlacementOptionReadModel
	app                  *tui.App
	catalogProbeSeq      int64
}

// NewWorkflow builds an idle (closed) workflow. Mount it by calling Open().
func NewWorkflow(workspaceID readmodel.WorkspaceID, route readmodel.RouteReadModel, save SaveTargetFunc, onClose func(), opts ...WorkflowOption) *Workflow {
	w := &Workflow{
		WorkspaceID:     workspaceID,
		Route:           route,
		Phase:           tui.NewState(PhaseClosed),
		Error:           tui.NewState(""),
		Provider:        tui.NewState(""),
		CredentialRef:   tui.NewState(""),
		BaseURL:         tui.NewState(""),
		ReadyForCatalog: tui.NewState(false),
		ProviderSetup:   tui.NewState(readmodel.ProviderSetupReadModel{}),
		AuthSession:     tui.NewState(readmodel.AuthSessionReadModel{}),
		Catalog:         tui.NewState(readmodel.ModelCatalogReadModel{}),
		CatalogLoading:  tui.NewState(false),
		SelectedModel:   tui.NewState(readmodel.ModelDeploymentReadModel{}),
		ManualModelID:   tui.NewState(""),
		Placement:       tui.NewState(defaultPlacementForRoute(route)),
		SaveTarget:      save,
		OnClose:         onClose,
	}
	for _, opt := range opts {
		opt(w)
	}
	return w
}

// Open transitions from Closed to ChoosingProvider.
func (w *Workflow) Open() {
	if w.Phase.Get() != PhaseClosed {
		return
	}
	w.Error.Set("")
	w.Phase.Set(PhaseChoosingProvider)
}

// IsOpen reports whether the workflow has left the Closed phase.
func (w *Workflow) IsOpen() bool {
	return w.Phase.Get() != PhaseClosed
}

// SelectProvider commits a provider choice and moves to ProviderSetup.
func (w *Workflow) SelectProvider(spec string) {
	w.resetFlowState()
	w.Provider.Set(spec)
	w.Error.Set("")
	w.seedProviderSetupDefaults()
	w.refreshProviderSetup()
	w.Phase.Set(PhaseProviderSetup)
}

// SetSetupReady transitions to LoadingCatalog when the adapter indicates the
// provider is ready to probe.
func (w *Workflow) SetSetupReady(credentialRef, baseURL string) {
	credentialRef = strings.TrimSpace(credentialRef) // swobu:io-string source=boundary
	baseURL = strings.TrimSpace(baseURL)             // swobu:io-string source=boundary
	if baseURL == "" {
		baseURL = profile.DefaultExecuteBaseURL(w.Provider.Get())
	}
	w.CredentialRef.Set(credentialRef)
	w.BaseURL.Set(baseURL)
	w.Error.Set("")
	w.advanceFromProviderSetup()
}

// SetCatalogResult updates the catalog result and transitions to ChoosingModel
// or CatalogFailed.
func (w *Workflow) SetCatalogResult(result readmodel.ModelCatalogReadModel) {
	w.Catalog.Set(result)
	w.CatalogLoading.Set(false)
	w.resetPickers()
	if result.Error != "" {
		w.Error.Set(result.Error)
		if w.ManualModelID != nil {
			if strings.TrimSpace(w.ManualModelID.Get()) == "" {
				w.ManualModelID.Set("enter model id")
			}
		}
		w.Phase.Set(PhaseCatalogFailed)
		return
	}
	w.Error.Set("")
	if w.ManualModelID != nil {
		w.ManualModelID.Set("")
	}
	w.Phase.Set(PhaseChoosingModel)
}

// SelectModel commits a model choice and moves to ReadyToCreate.
func (w *Workflow) SelectModel(model readmodel.ModelDeploymentReadModel) {
	w.SelectedModel.Set(model)
	w.Error.Set("")
	w.Phase.Set(PhaseReadyToCreate)
}

// OpenPlacementPicker generates placement options from current route targets
// and opens the placement selection phase.
func (w *Workflow) OpenPlacementPicker() {
	if w.Phase.Get() != PhaseReadyToCreate {
		return
	}
	w.buildPlacementOptions()
	w.Phase.Set(PhaseChoosingPlacement)
}

// SelectPlacement commits a placement choice and returns to ReadyToCreate.
func (w *Workflow) SelectPlacement(p readmodel.PlacementOptionReadModel) {
	w.placementPickerCache = nil // clear picker cache
	w.Placement.Set(p)
	w.Phase.Set(PhaseReadyToCreate)
}

// getPlacementOptions returns the current placement picker options.  Lazily built
// when the picker phase is opened.
func (w *Workflow) getPlacementOptions() []readmodel.PlacementOptionReadModel {
	if w.placementPickerCache == nil {
		w.buildPlacementOptions()
	}
	return w.placementPickerCache
}

// Create attempts to persist the target.
func (w *Workflow) Create(ctx context.Context) {
	if w.SaveTarget == nil {
		w.Error.Set("target save is not wired yet")
		w.Phase.Set(PhaseCreateFailed)
		return
	}
	provider := w.Provider.Get()
	model := w.SelectedModel.Get()
	placement := w.Placement.Get()
	req := ports.SaveTargetRequest{
		WorkspaceID:      w.WorkspaceID,
		RouteID:          w.Route.ID,
		Provider:         provider,
		Model:            model.ModelName,
		ProviderProtocol: model.DefaultProviderProtocol,
		BaseURL:          w.BaseURL.Get(),
		CredentialRef:    w.CredentialRef.Get(),
		Rank:             placement.Rank,
		Weight:           placement.Weight,
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

// Back navigates to the previous phase or closes the workflow.
// Returns true if the event was consumed.
func (w *Workflow) Back() bool {
	switch w.Phase.Get() {
	case PhaseChoosingProvider:
		w.Phase.Set(PhaseClosed)
		if w.OnClose != nil {
			w.OnClose()
		}
	case PhaseProviderSetup:
		w.resetFlowState()
		w.Provider.Set("")
		w.Phase.Set(PhaseChoosingProvider)
	case PhaseLoadingCatalog:
		w.resetFlowState()
		w.Phase.Set(PhaseProviderSetup)
	case PhaseAuthPending:
		w.CancelAuthSession()
		return true
	case PhaseChoosingModel:
		w.SelectedModel.Set(readmodel.ModelDeploymentReadModel{})
		w.Phase.Set(PhaseProviderSetup)
	case PhaseCatalogFailed:
		w.resetFlowState()
		w.Phase.Set(PhaseProviderSetup)
	case PhaseChoosingPlacement:
		w.placementPickerCache = nil
		w.Phase.Set(PhaseReadyToCreate)
	case PhaseReadyToCreate:
		w.SelectedModel.Set(readmodel.ModelDeploymentReadModel{})
		w.Phase.Set(PhaseChoosingModel)
	case PhaseAuthFailed:
		w.resetFlowState()
		w.Phase.Set(PhaseProviderSetup)
	case PhaseCreateFailed:
		w.Phase.Set(PhaseReadyToCreate)
	case PhaseClosed, PhaseCreated:
		return false
	default:
		return false
	}
	return true
}

// Close forcibly closes the workflow from any phase.
func (w *Workflow) Close() {
	w.Phase.Set(PhaseClosed)
	if w.OnClose != nil {
		w.OnClose()
	}
}

// KeyMap returns back/cancel bindings when the workflow is open.
func (w *Workflow) KeyMap() tui.KeyMap {
	if !w.IsOpen() {
		return nil
	}
	return tui.KeyMap{
		tui.OnFocused(tui.KeyEscape, func(tui.KeyEvent) { w.Back() }),
	}
}

// UpdateProps refreshes port references and route/workspace identity without
// resetting user selection state.
func (w *Workflow) UpdateProps(fresh tui.Component) {
	f, ok := fresh.(*Workflow)
	if !ok {
		return
	}
	w.WorkspaceID = f.WorkspaceID
	w.Route = f.Route
	w.SaveTarget = f.SaveTarget
	w.TargetSetupQueries = f.TargetSetupQueries
	w.TargetAuthCommands = f.TargetAuthCommands
	w.OnCreated = f.OnCreated
	w.OnClose = f.OnClose
	w.providerOptions = f.providerOptions
	w.resetPickers()
	w.placementPickerCache = nil
}

// BindApp wires the workflow's state fields to the live go-tui app so async
// updates can safely queue back onto the main loop.
func (w *Workflow) BindApp(app *tui.App) {
	if app == nil {
		return
	}
	w.app = app
	if w.Phase != nil {
		w.Phase.BindApp(app)
	}
	if w.Error != nil {
		w.Error.BindApp(app)
	}
	if w.Provider != nil {
		w.Provider.BindApp(app)
	}
	if w.CredentialRef != nil {
		w.CredentialRef.BindApp(app)
	}
	if w.BaseURL != nil {
		w.BaseURL.BindApp(app)
	}
	if w.ReadyForCatalog != nil {
		w.ReadyForCatalog.BindApp(app)
	}
	if w.ProviderSetup != nil {
		w.ProviderSetup.BindApp(app)
	}
	if w.AuthSession != nil {
		w.AuthSession.BindApp(app)
	}
	if w.Catalog != nil {
		w.Catalog.BindApp(app)
	}
	if w.CatalogLoading != nil {
		w.CatalogLoading.BindApp(app)
	}
	if w.SelectedModel != nil {
		w.SelectedModel.BindApp(app)
	}
	if w.ManualModelID != nil {
		w.ManualModelID.BindApp(app)
	}
	if w.Placement != nil {
		w.Placement.BindApp(app)
	}
}

// UnbindApp drops the live app handle. go-tui does not unbind State values, so
// they remain attached until the workflow is rebound on a fresh app.
func (w *Workflow) UnbindApp() {
	w.app = nil
}

// ---------------------------------------------------------------------------
// Picker components (exposed to GSX)
// ---------------------------------------------------------------------------

// ProviderPickerComponent returns the provider SearchPicker for this workflow.
func ProviderPickerComponent(w *Workflow) *ui.SearchPicker {
	return w.providerPicker()
}

// ModelPickerComponent returns the model SearchPicker for this workflow.
func ModelPickerComponent(w *Workflow) *ui.SearchPicker {
	return w.modelPicker()
}

func (w *Workflow) providerPicker() *ui.SearchPicker {
	if w.providerPickerCache == nil {
		opts := make([]ui.SearchOption, len(w.providerOptions))
		for i, p := range w.providerOptions {
			opts[i] = ui.SearchOption{
				ID:    p.ProviderSpec,
				Label: p.DisplayName,
			}
		}
		w.providerPickerCache = ui.NewSearchPicker(
			"provider-picker",
			"provider",
			opts,
			func(opt ui.SearchOption) {
				w.SelectProvider(opt.ID)
				w.ContinueSetup()
			},
			func() { w.Back() },
		)
		w.providerPickerCache.AutoFocus = true
	}
	return w.providerPickerCache
}

func (w *Workflow) modelPicker() *ui.SearchPicker {
	if w.modelPickerCache == nil {
		catalog := w.Catalog.Get()
		opts := make([]ui.SearchOption, len(catalog.Deployments))
		for i, d := range catalog.Deployments {
			opts[i] = ui.SearchOption{
				ID:    d.ID,
				Label: d.Name,
			}
		}
		w.modelPickerCache = ui.NewSearchPicker(
			"model-picker",
			"model",
			opts,
			func(opt ui.SearchOption) {
				for _, d := range catalog.Deployments {
					if d.ID == opt.ID {
						w.SelectModel(d)
						break
					}
				}
			},
			func() { w.Back() },
		)
		w.modelPickerCache.AutoFocus = true
	}
	return w.modelPickerCache
}

func (w *Workflow) resetPickers() {
	w.providerPickerCache = nil
	w.modelPickerCache = nil
}

// ProbeCatalog resolves the catalog for the current provider and advances
// the phase. If TargetSetupQueries is set it calls the daemon; otherwise
// it falls back to the static catalog for Tier 1-2 providers.
func (w *Workflow) ProbeCatalog() {
	if w.Phase.Get() != PhaseLoadingCatalog {
		return
	}
	result := probeCatalogSnapshot(
		w.TargetSetupQueries,
		strings.TrimSpace(w.Provider.Get()),      // swobu:io-string source=boundary
		strings.TrimSpace(w.BaseURL.Get()),       // swobu:io-string source=boundary
		strings.TrimSpace(w.CredentialRef.Get()), // swobu:io-string source=boundary
	)
	w.SetCatalogResult(result)
}

// ReadyAndProbe commits the projected setup inputs, enters LoadingCatalog,
// and probes the catalog asynchronously when a live app exists.
// Callers without a live app must drive ProbeCatalog themselves after the
// loading state is visible.
func (w *Workflow) ReadyAndProbe(credentialRef, baseURL string) {
	credentialRef = strings.TrimSpace(credentialRef) // swobu:io-string source=boundary
	baseURL = strings.TrimSpace(baseURL)             // swobu:io-string source=boundary
	if baseURL == "" {
		baseURL = profile.DefaultExecuteBaseURL(w.Provider.Get())
	}
	w.CredentialRef.Set(credentialRef)
	w.BaseURL.Set(baseURL)
	setup := w.refreshProviderSetup()
	if !setup.ReadyForCatalog {
		w.ReadyForCatalog.Set(false)
		w.CatalogLoading.Set(false)
		w.Phase.Set(PhaseProviderSetup)
		return
	}
	w.ReadyForCatalog.Set(true)
	w.CatalogLoading.Set(true)
	w.Error.Set("")
	w.Phase.Set(PhaseLoadingCatalog)
	if w.hasLiveApp() {
		w.catalogProbeSeq++
		seq := w.catalogProbeSeq
		app := w.app
		provider := strings.TrimSpace(w.Provider.Get())          // swobu:io-string source=boundary
		baseURL = strings.TrimSpace(w.BaseURL.Get())             // swobu:io-string source=boundary
		credentialRef = strings.TrimSpace(w.CredentialRef.Get()) // swobu:io-string source=boundary
		queries := w.TargetSetupQueries
		go func() {
			result := probeCatalogSnapshot(queries, provider, baseURL, credentialRef)
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

// ContinueSetup advances from provider setup when the projection says the
// provider is ready, or starts the interactive auth branch for ChatGPT.
func (w *Workflow) ContinueSetup() {
	if w.Phase.Get() != PhaseProviderSetup {
		return
	}
	setup := w.refreshProviderSetup()
	if setup.InteractiveAuth {
		w.startInteractiveAuth()
		return
	}
	if setup.ReadyForCatalog {
		w.ReadyAndProbe(setup.CredentialRef, w.BaseURL.Get())
		return
	}
	w.Phase.Set(PhaseProviderSetup)
}

// ChangeProvider goes back to ChoosingProvider from ProviderSetup.
func (w *Workflow) ChangeProvider() {
	if w.Phase.Get() == PhaseProviderSetup {
		w.Back()
	}
}

func (w *Workflow) seedProviderSetupDefaults() {
	spec := w.Provider.Get()
	if spec == "" {
		return
	}
	if w.BaseURL.Get() == "" && !profile.RequiresExplicitExecuteBaseURL(spec) {
		if defaultBaseURL := profile.DefaultExecuteBaseURL(spec); defaultBaseURL != "" {
			w.BaseURL.Set(defaultBaseURL)
		}
	}
	if w.CredentialRef.Get() == "" {
		if envKey := profile.DefaultEnvKeyForSpec(spec); envKey != "" {
			w.CredentialRef.Set("env:" + envKey)
		}
	}
}

func (w *Workflow) refreshProviderSetup() readmodel.ProviderSetupReadModel {
	setup := w.resolveProviderSetupProjection()
	w.ProviderSetup.Set(setup)
	w.ReadyForCatalog.Set(setup.ReadyForCatalog)
	return setup
}

func (w *Workflow) advanceFromProviderSetup() {
	setup := w.refreshProviderSetup()
	if setup.ReadyForCatalog {
		w.ReadyAndProbe(setup.CredentialRef, w.BaseURL.Get())
		return
	}
	w.Phase.Set(PhaseProviderSetup)
}

func (w *Workflow) resolveProviderSetupProjection() readmodel.ProviderSetupReadModel {
	spec := strings.TrimSpace(w.Provider.Get())
	if spec == "" {
		return readmodel.ProviderSetupReadModel{}
	}
	baseURL := strings.TrimSpace(w.BaseURL.Get())
	credentialRef := strings.TrimSpace(w.CredentialRef.Get())
	if w.TargetSetupQueries != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		setup, err := w.TargetSetupQueries.ResolveProviderSetup(ctx, ports.ResolveProviderSetupRequest{
			ProviderSpec:  spec,
			BaseURL:       baseURL,
			CredentialRef: credentialRef,
		})
		if err == nil {
			return setup
		}
	}
	return projectProviderSetupLocal(spec, baseURL, credentialRef)
}

func projectProviderSetupLocal(providerSpec, baseURL, credentialRef string) readmodel.ProviderSetupReadModel {
	provider, ok := providerProfileForSpecLocal(providerSpec)
	if !ok {
		return readmodel.ProviderSetupReadModel{}
	}
	normalizedBaseURL := strings.TrimSpace(baseURL)
	defaultBaseURL := strings.TrimSpace(provider.DefaultBaseURL)
	if normalizedBaseURL == "" && !profile.RequiresExplicitExecuteBaseURL(providerSpec) {
		normalizedBaseURL = defaultBaseURL
	}
	normalizedCredentialRef := strings.TrimSpace(credentialRef)
	defaultEnvKey := strings.TrimSpace(provider.DefaultCredentialEnvVar)
	if normalizedCredentialRef == "" && defaultEnvKey != "" {
		normalizedCredentialRef = "env:" + defaultEnvKey
	}
	authModes := providerSetupLocalAuthModes(provider.AllowedAuthModes)
	setup := readmodel.ProviderSetupReadModel{
		ProviderSpec:       providerSpec,
		DisplayName:        provider.ProviderDisplayName,
		DefaultBaseURL:     defaultBaseURL,
		DefaultAuthHeader:  strings.TrimSpace(provider.DefaultAuthHeader),
		CredentialRef:      normalizedCredentialRef,
		CredentialRequired: profile.RequiresCredential(providerSpec, normalizedBaseURL),
		InteractiveAuth:    providerSetupLocalHasInteractiveAuth(authModes),
		AuthModes:          authModes,
		RequiresBaseURL:    profile.RequiresExplicitExecuteBaseURL(providerSpec) && strings.TrimSpace(baseURL) == "",
	}
	if setup.InteractiveAuth {
		setup.CredentialLabel = providerSetupLocalInteractiveLabel(authModes)
		if setup.CredentialLabel == "" {
			setup.CredentialLabel = "browser login"
		}
		if strings.TrimSpace(normalizedCredentialRef) == "" {
			setup.BlockReason = "auth first"
			return setup
		}
		setup.ReadyForCatalog = true
		return setup
	}
	if setup.RequiresBaseURL {
		setup.CredentialLabel = "enter base URL"
		setup.BlockReason = "enter base URL"
		return setup
	}
	if setup.CredentialRequired && !providerSetupLocalCredentialReady(providerSpec, normalizedCredentialRef) {
		missing := defaultEnvKey
		if missing == "" {
			missing = "credential"
		}
		setup.CredentialLabel = "missing " + missing
		setup.BlockReason = setup.CredentialLabel
		return setup
	}
	if setup.CredentialLabel == "" {
		setup.CredentialLabel = normalizedCredentialRef
	}
	setup.ReadyForCatalog = true
	return setup
}

func providerProfileForSpecLocal(providerSpec string) (profile.Profile, bool) {
	for _, candidate := range profile.All() {
		if string(candidate.ProviderID) == providerSpec {
			return candidate, true
		}
	}
	return profile.Profile{}, false
}

func providerSetupLocalAuthModes(modes []profile.AuthModeSpec) []readmodel.AuthModeReadModel {
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

func providerSetupLocalHasInteractiveAuth(modes []readmodel.AuthModeReadModel) bool {
	for _, mode := range modes {
		if mode.Interactive {
			return true
		}
	}
	return false
}

func providerSetupLocalInteractiveLabel(modes []readmodel.AuthModeReadModel) string {
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

func providerSetupLocalCredentialReady(providerSpec string, credentialRef string) bool {
	ref := strings.TrimSpace(credentialRef) // swobu:io-string source=boundary
	if ref == "" {
		return false
	}
	envKey, ok := providerSetupLocalEnvCredentialName(providerSpec, ref)
	if !ok {
		return true
	}
	if envKey == "" {
		return false
	}
	val, ok := os.LookupEnv(envKey)
	return ok && strings.TrimSpace(val) != ""
}

func providerSetupLocalEnvCredentialName(providerSpec string, credentialRef string) (string, bool) {
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

func (w *Workflow) resetFlowState() {
	if w.AuthSession != nil {
		w.AuthSession.Set(readmodel.AuthSessionReadModel{})
	}
	if w.CredentialRef != nil {
		w.CredentialRef.Set("")
	}
	if w.BaseURL != nil {
		w.BaseURL.Set("")
	}
	if w.ReadyForCatalog != nil {
		w.ReadyForCatalog.Set(false)
	}
	if w.ProviderSetup != nil {
		w.ProviderSetup.Set(readmodel.ProviderSetupReadModel{})
	}
	if w.CatalogLoading != nil {
		w.CatalogLoading.Set(false)
	}
	if w.Catalog != nil {
		w.Catalog.Set(readmodel.ModelCatalogReadModel{})
	}
	if w.SelectedModel != nil {
		w.SelectedModel.Set(readmodel.ModelDeploymentReadModel{})
	}
	if w.ManualModelID != nil {
		w.ManualModelID.Set("")
	}
	if w.Placement != nil {
		w.Placement.Set(defaultPlacementForRoute(w.Route))
	}
	w.resetPickers()
	w.catalogProbeSeq++
}

func (w *Workflow) hasLiveApp() bool {
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

func (w *Workflow) requiresInteractiveAuth() bool {
	setup := w.ProviderSetup.Get()
	if setup.ProviderSpec != "" {
		return setup.InteractiveAuth
	}
	mode, _ := w.interactiveAuthMode()
	return mode != ""
}

func (w *Workflow) interactiveAuthMode() (profile.AuthMode, string) {
	setup := w.ProviderSetup.Get()
	if setup.ProviderSpec != "" && len(setup.AuthModes) > 0 {
		for _, mode := range setup.AuthModes {
			if !mode.Interactive {
				continue
			}
			return profile.AuthMode(mode.Mode), authModeLabel(profile.AuthMode(mode.Mode))
		}
	}
	for _, mode := range profile.SupportedAuthModesForSpec(w.Provider.Get()) {
		if !profile.IsInteractiveAuthMode(mode) {
			continue
		}
		return mode, authModeLabel(mode)
	}
	return "", ""
}

func authModeLabel(mode profile.AuthMode) string {
	switch mode {
	case profile.AuthModeChatGPTLogin:
		return "browser login"
	case profile.AuthModeChatGPTDeviceAuth:
		return "device auth"
	default:
		return strings.TrimSpace(string(mode)) // swobu:io-string source=boundary
	}
}

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

func authSubjectLocator(w *Workflow) string {
	return fmt.Sprintf("subject:%s#%s", strings.TrimSpace(string(w.WorkspaceID)), strings.TrimSpace(string(w.Route.ID)))
}

func (w *Workflow) startInteractiveAuth() {
	mode, _ := w.interactiveAuthMode()
	if mode == "" {
		w.Error.Set("interactive auth is unavailable for provider " + w.Provider.Get())
		w.Phase.Set(PhaseAuthFailed)
		return
	}
	if w.TargetAuthCommands == nil {
		w.Error.Set("auth session commands are not wired yet")
		w.Phase.Set(PhaseAuthFailed)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := w.TargetAuthCommands.StartAuthSession(ctx, ports.StartAuthSessionRequest{
		ProviderSpec: w.Provider.Get(),
		EndpointRef:  authSubjectLocator(w),
		AuthMode:     authModeDaemonName(mode),
	})
	if err != nil {
		w.Error.Set(err.Error())
		w.Phase.Set(PhaseAuthFailed)
		return
	}
	w.applyAuthSessionResult(session)
}

func (w *Workflow) RefreshAuthSession() {
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
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := w.TargetAuthCommands.PollAuthSession(ctx, session.SessionID)
	if err != nil {
		w.Error.Set(err.Error())
		w.Phase.Set(PhaseAuthFailed)
		return
	}
	w.applyAuthSessionResult(result)
}

func (w *Workflow) CancelAuthSession() {
	session := w.AuthSession.Get()
	if w.TargetAuthCommands != nil && strings.TrimSpace(session.SessionID) != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		_ = w.TargetAuthCommands.CancelAuthSession(ctx, session.SessionID)
		cancel()
	}
	w.resetFlowState()
	w.Phase.Set(PhaseProviderSetup)
}

func (w *Workflow) applyAuthSessionResult(result readmodel.AuthSessionReadModel) {
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
		w.Phase.Set(PhaseProviderSetup)
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

func probeCatalogSnapshot(queries ports.TargetSetupQueries, provider, baseURL, credentialRef string) readmodel.ModelCatalogReadModel {
	if queries != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		authHeader := profile.DefaultAuthHeaderForSpec(provider)
		providerProtocol, _ := profile.ResolveConcreteProtocolForAutoAtBoundary(provider)
		result, err := queries.ProbeProviderModels(ctx, ports.ProbeProviderModelsRequest{
			ProviderSpec:     provider,
			BaseURL:          baseURL,
			AuthHeader:       authHeader,
			CredentialRef:    credentialRef,
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

func openBrowserURL(raw string) error {
	url := strings.TrimSpace(raw)
	if url == "" {
		return fmt.Errorf("auth URL is missing")
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", "", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func defaultPlacementForRoute(route readmodel.RouteReadModel) readmodel.PlacementOptionReadModel {
	maxRank := 0
	for _, t := range route.Targets {
		if t.Rank > maxRank {
			maxRank = t.Rank
		}
	}
	return readmodel.PlacementOptionReadModel{
		Label:  fallbackPlacementLabel(maxRank),
		Rank:   maxRank + 1,
		Weight: 1,
		Kind:   readmodel.PlacementFallback,
	}
}

func workflowTitle(w *Workflow) string {
	if w.Phase.Get() == PhaseChoosingProvider {
		return "add target"
	}

	provider := strings.TrimSpace(providerDisplay(w))
	if provider == "" {
		provider = strings.TrimSpace(w.Provider.Get())
	}
	if provider == "" {
		return "new target"
	}
	return "new target · " + provider
}

// buildPlacementOptions generates placement options from current route targets.
// For each existing rank: balance with step.
// For new targets: one fallback after last step.
// RetryCatalog re-enters LoadingCatalog from CatalogFailed, reusing setup inputs.
func (w *Workflow) RetryCatalog() {
	if w.Phase.Get() != PhaseCatalogFailed {
		return
	}
	w.Error.Set("")
	w.Phase.Set(PhaseProviderSetup)
	w.ContinueSetup()
}

func (w *Workflow) buildPlacementOptions() []readmodel.PlacementOptionReadModel {
	ranks := make([]int, 0, len(w.Route.Targets))
	seen := make(map[int]struct{})
	maxRank := 0
	for _, t := range w.Route.Targets {
		if t.Rank > maxRank {
			maxRank = t.Rank
		}
		if _, ok := seen[t.Rank]; ok {
			continue
		}
		seen[t.Rank] = struct{}{}
		ranks = append(ranks, t.Rank)
	}
	sort.Ints(ranks)

	opts := make([]readmodel.PlacementOptionReadModel, 0, len(ranks)+1)
	for _, rank := range ranks {
		opts = append(opts, readmodel.PlacementOptionReadModel{
			Label:  fmt.Sprintf("balance with step %d", rank),
			Rank:   rank,
			Weight: 1,
			Kind:   readmodel.PlacementBalance,
		})
	}
	opts = append(opts, readmodel.PlacementOptionReadModel{
		Label:  fallbackPlacementLabel(maxRank),
		Rank:   maxRank + 1,
		Weight: 1,
		Kind:   readmodel.PlacementFallback,
	})
	w.placementPickerCache = opts
	return opts
}

func fallbackPlacementLabel(maxRank int) string {
	if maxRank <= 0 {
		return "fallback after last step"
	}
	return fmt.Sprintf("fallback after step %d", maxRank)
}

// placementPickerSelectableRow creates one selectable row for a placement option.
func PlacementPickerOptionRowComponent(w *Workflow, idx int, opt readmodel.PlacementOptionReadModel) *ui.SelectableRow {
	row := ui.NewSelectableRow(
		targetAddMountKey(w, fmt.Sprintf("placement-opt-%d", idx)),
		"",
		opt.Summary(),
		"select ↵",
		func() { w.SelectPlacement(opt) },
	)
	row.AutoFocus = idx == 0
	return row
}
func providerDisplay(w *Workflow) string {
	setup := w.ProviderSetup.Get()
	if setup.DisplayName != "" {
		return setup.DisplayName
	}
	spec := w.Provider.Get()
	if spec == "" {
		return ""
	}
	for _, p := range w.providerOptions {
		if p.ProviderSpec == spec {
			return p.DisplayName
		}
	}
	return spec
}

func credentialDisplay(w *Workflow) string {
	setup := w.ProviderSetup.Get()
	if setup.CredentialLabel != "" && setup.ReadyForCatalog {
		return setup.CredentialLabel
	}
	return strings.TrimSpace(w.CredentialRef.Get())
}

// ProviderDisplayRowComponent is the "change provider" actionable row.
func ProviderDisplayRowComponent(w *Workflow) *ui.SelectableRow {
	return ui.NewSelectableRow(
		targetAddMountKey(w, "provider-display"),
		"provider",
		providerDisplay(w),
		"change ↵",
		func() { w.ChangeProvider() },
	)
}

// CredentialDisplayRowComponent shows the credential ref that unlocked catalog
// probing.
func CredentialDisplayRowComponent(w *Workflow) *ui.SelectableRow {
	return ui.NewSelectableRow(
		targetAddMountKey(w, "credential-display"),
		"credential",
		credentialDisplay(w),
		"ok",
		nil,
	)
}

// ProviderSetupBlockedRowComponent shows the missing credential or base URL
// blocker instead of pretending the provider is ready.
func ProviderSetupBlockedRowComponent(w *Workflow) *ui.SelectableRow {
	setup := w.ProviderSetup.Get()
	label := "credential"
	value := setup.BlockReason
	if setup.RequiresBaseURL {
		label = "base URL"
		if value == "" {
			value = "enter base URL"
		}
	}
	if value == "" {
		value = "blocked"
	}
	row := ui.NewSelectableRow(
		targetAddMountKey(w, "provider-setup-blocked"),
		label,
		value,
		"choose ↵",
		func() { w.ContinueSetup() },
	)
	row.AutoFocus = true
	return row
}

// CatalogFailedRetryRowComponent lets the operator retry catalog probing.
func CatalogFailedRetryRowComponent(w *Workflow) *ui.SelectableRow {
	row := ui.NewSelectableRow(
		targetAddMountKey(w, "catalog-retry"),
		"model",
		"catalog failed",
		"retry ↵",
		func() { w.RetryCatalog() },
	)
	row.AutoFocus = true
	return row
}

// AuthStartRowComponent starts the interactive auth flow for providers such
// as ChatGPT.
func AuthStartRowComponent(w *Workflow) *ui.SelectableRow {
	_, label := w.interactiveAuthMode()
	if label == "" {
		label = "browser login"
	}
	row := ui.NewSelectableRow(
		targetAddMountKey(w, "auth-start"),
		"auth",
		label,
		"start ↵",
		func() { w.ContinueSetup() },
	)
	row.AutoFocus = true
	return row
}

// AuthPendingSummaryRowComponent shows the pending browser/device auth state.
func AuthPendingSummaryRowComponent(w *Workflow) *ui.SelectableRow {
	_, label := w.interactiveAuthMode()
	if label == "" {
		label = "browser login"
	}
	return ui.NewSelectableRow(
		targetAddMountKey(w, "auth-pending"),
		"auth",
		label,
		"pending",
		nil,
	)
}

// AuthPendingOpenRowComponent opens the pending browser-auth URL.
func AuthPendingOpenRowComponent(w *Workflow) *ui.SelectableRow {
	url := ""
	session := w.AuthSession.Get()
	if session.SessionID != "" {
		url = session.AuthorizeURL
	}
	return ui.NewSelectableRow(
		targetAddMountKey(w, "auth-open"),
		"open",
		url,
		"open ↵",
		func() {
			if err := openBrowserURL(url); err != nil {
				w.Error.Set(err.Error())
			}
		},
	)
}

// AuthPendingStatusRowComponent polls the daemon for auth session status.
func AuthPendingStatusRowComponent(w *Workflow) *ui.SelectableRow {
	row := ui.NewSelectableRow(
		targetAddMountKey(w, "auth-status"),
		"status",
		"waiting for login",
		"refresh ↵",
		func() { w.RefreshAuthSession() },
	)
	row.AutoFocus = true
	return row
}

// AuthPendingCancelRowComponent cancels the browser auth session and returns
// the operator to provider setup.
func AuthPendingCancelRowComponent(w *Workflow) *ui.SelectableRow {
	return ui.NewSelectableRow(
		targetAddMountKey(w, "auth-cancel"),
		"cancel",
		"",
		"cancel ↵",
		func() { w.CancelAuthSession() },
	)
}

// AuthSignedInRowComponent reports that auth has completed successfully.
func AuthSignedInRowComponent(w *Workflow) *ui.SelectableRow {
	return ui.NewSelectableRow(
		targetAddMountKey(w, "auth-signed-in"),
		"auth",
		"signed in",
		"ok",
		nil,
	)
}

// AuthFailedRowComponent returns the operator to provider setup after an auth
// error.
func AuthFailedRowComponent(w *Workflow) *ui.SelectableRow {
	return ui.NewSelectableRow(
		targetAddMountKey(w, "auth-failed"),
		"auth",
		"failed",
		"back ↵",
		func() { w.Back() },
	)
}

// LoadingCatalogRowComponent shows the catalog probe in flight.
func LoadingCatalogRowComponent(w *Workflow) *ui.SelectableRow {
	row := ui.NewSelectableRow(
		targetAddMountKey(w, "loading-catalog"),
		"model",
		"loading catalog…",
		"wait",
		nil,
	)
	row.AutoFocus = true
	return row
}

// ModelBlockedRowComponent keeps the operator honest about auth-gated model
// selection.
func ModelBlockedRowComponent(w *Workflow) *ui.SelectableRow {
	return ui.NewSelectableRow(
		targetAddMountKey(w, "model-blocked"),
		"model",
		"blocked",
		"auth first",
		nil,
	)
}

// ManualModelEntryRowComponent lets the operator type a model id when catalog
// probing failed. It keeps the fallback local to the workflow instead of
// reopening the provider/model contract.
func ManualModelEntryRowComponent(w *Workflow) *ui.EditableRow {
	value := w.ManualModelID
	if value == nil {
		value = tui.NewState("enter model id")
	}
	if strings.TrimSpace(value.Get()) == "" {
		value.Set("enter model id")
	}
	row := ui.NewEditableRow(
		targetAddMountKey(w, "manual-model"),
		"manual",
		value,
	)
	row.ViewAction = "enter ↵"
	row.EditAction = "save ↵"
	row.OnSubmit = func(raw string) {
		modelID := strings.TrimSpace(raw)
		if modelID == "" {
			return
		}
		if w.ManualModelID != nil {
			w.ManualModelID.Set(modelID)
		}
		protocol := strings.TrimSpace(w.Catalog.Get().ResolvedProviderProtocol)
		if protocol == "" {
			if concrete, ok := profile.ResolveConcreteProtocolForAutoAtBoundary(w.Provider.Get()); ok {
				protocol = concrete
			}
		}
		if protocol == "" {
			for _, supported := range profile.SupportedProviderProtocolsForSpec(w.Provider.Get()) {
				if strings.TrimSpace(supported) == "" || supported == profile.ProviderProtocolAuto {
					continue
				}
				protocol = supported
				break
			}
		}
		if protocol == "" {
			protocol = strings.TrimSpace(w.SelectedModel.Get().DefaultProviderProtocol)
		}
		model := readmodel.ModelDeploymentReadModel{
			ID:                      modelID,
			Name:                    modelID,
			ModelName:               modelID,
			DefaultProviderProtocol: protocol,
		}
		if protocol != "" {
			model.SupportedProviderProtocols = []string{protocol}
		}
		w.SelectModel(model)
	}
	return row
}

// CreateRetryRowComponent lets the operator retry after create failure.
func CreateRetryRowComponent(w *Workflow) *ui.SelectableRow {
	return ui.NewSelectableRow(
		targetAddMountKey(w, "create-retry"),
		"create",
		"failed",
		"retry ↵",
		func() {
			w.Error.Set("")
			w.Phase.Set(PhaseReadyToCreate)
		},
	)
}

// ModelDisplayRowComponent lets the operator go back to choose a different model.
func ModelDisplayRowComponent(w *Workflow) *ui.SelectableRow {
	return ui.NewSelectableRow(
		targetAddMountKey(w, "model-display"),
		"model",
		w.SelectedModel.Get().ModelName,
		"change ↵",
		func() { w.Back() },
	)
}

// PlacementDisplayRowComponent lets the operator open the placement picker.
func PlacementDisplayRowComponent(w *Workflow) *ui.SelectableRow {
	return ui.NewSelectableRow(
		targetAddMountKey(w, "placement-display"),
		"placement",
		w.Placement.Get().Summary(),
		"change ↵",
		func() { w.OpenPlacementPicker() },
	)
}

// CreateRowComponent is the final "create target" action row.
func CreateRowComponent(w *Workflow) *ui.SelectableRow {
	row := ui.NewSelectableRow(
		targetAddMountKey(w, "create"),
		"create",
		"",
		"create ↵",
		func() { w.Create(context.Background()) },
	)
	row.AutoFocus = true
	return row
}

// createMountKey was at the file bottom; move it with other helpers.
// targetAddMountKey returns a stable mount key scoped to this workflow.
func targetAddMountKey(w *Workflow, suffix string) string {
	return fmt.Sprintf("target-add:%s:%s:%s", w.WorkspaceID, w.Route.ID, suffix)
}
