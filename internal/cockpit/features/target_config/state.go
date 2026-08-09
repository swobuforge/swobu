package target_config

import (
	"strings"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/routing"
)

// chatGPTReadiness is the ChatGPT arm of the feature's derived form status.
func chatGPTReadiness(w *TargetConfig, base providerSetupState) providerSetupState {
	setup := base
	if strings.TrimSpace(setup.CredentialRef) == "" {
		setup.Status = setupMissingInteractiveAuth
		return setup
	}
	setup.Status = setupReady
	return setup
}

// readiness_projection.go projects the current provider's setup readiness: the
// providerSetupState struct and setupState, which builds the provider-metadata
// baseline then applies the explicit provider readiness projection. Pure: no
// ports, no go-tui; recomputed every read, never stored.

type setupStatus uint8

const (
	setupMissingLocator setupStatus = iota
	setupMissingCredential
	setupMissingInteractiveAuth
	setupReady
)

// providerSetupState is semantic setup readiness, independent from operation
// phase, loading, and errors. Provider forms explain the missing owning field;
// generic tail rows consume only Ready.
type providerSetupState struct {
	ProviderSpec       string
	DisplayName        string
	LocatorKind        profile.LocatorKind
	EndpointLabel      string
	CredentialRef      string
	CredentialRequired bool
	Status             setupStatus
}

func (s providerSetupState) RequiresLocator() bool    { return s.Status == setupMissingLocator }
func (s providerSetupState) RequiresCredential() bool { return s.Status == setupMissingCredential }
func (s providerSetupState) Ready() bool              { return s.Status == setupReady }

// setupState computes the current provider-readiness projection. It builds the
// provider-metadata baseline then applies the provider-specific projection.
// Pure function: never stored, recomputed every read, cannot go stale.
func (w *TargetConfig) setupState() providerSetupState {
	spec := strings.TrimSpace(w.Draft.Get().ProviderSpec) // swobu:io-string source=boundary
	if spec == "" {
		return providerSetupState{}
	}
	provider, ok := providerProfileForSpec(spec)
	if !ok {
		return providerSetupState{}
	}
	base := w.baseSetupState(provider, w.endpointValueFor(provider))
	return providerReadiness(w, base)
}

func (w *TargetConfig) baseSetupState(provider profile.Profile, endpointValue string) providerSetupState {
	spec := string(provider.ProviderID)
	locator := provider.Locator
	label := strings.TrimSpace(locator.Label)
	if label == "" {
		label = "endpoint"
	}
	credentialRef := strings.TrimSpace(w.Draft.Get().CredentialRef) // swobu:io-string source=boundary
	return providerSetupState{
		ProviderSpec:       spec,
		DisplayName:        provider.ProviderDisplayName,
		LocatorKind:        locator.Kind,
		EndpointLabel:      label,
		CredentialRef:      credentialRef,
		CredentialRequired: profile.RequiresCredential(spec, endpointValue),
		Status:             setupMissingLocator,
	}
}

func (w *TargetConfig) endpointValueFor(provider profile.Profile) string {
	if provider.ProviderID == profile.ProviderSpecBedrock {
		return strings.TrimSpace(w.BaseURL.Get())
	}
	endpointValue := strings.TrimSpace(w.BaseURL.Get()) // swobu:io-string source=boundary
	if endpointValue == "" && !profile.RequiresLocator(string(provider.ProviderID)) {
		endpointValue = strings.TrimSpace(provider.Locator.Default)
	}
	return endpointValue
}

func providerProfileForSpec(spec string) (profile.Profile, bool) {
	for _, candidate := range profile.All() {
		if string(candidate.ProviderID) == spec {
			return candidate, true
		}
	}
	return profile.Profile{}, false
}

// RequiresInteractiveAuth reports the one provider-specific login exception.
// Interactive login is ChatGPT workflow policy, not generic setup metadata.
func (w *TargetConfig) RequiresInteractiveAuth() bool {
	return profile.ProviderID(w.Draft.Get().ProviderSpec) == profile.ProviderSpecChatGPT
}

// interactiveAuthMode is ChatGPT workflow policy, not a provider-catalog fact.
func (w *TargetConfig) interactiveAuthMode() (string, string) {
	if profile.ProviderID(w.Draft.Get().ProviderSpec) == profile.ProviderSpecChatGPT {
		mode := w.ChatGPTAuthMode.Get()
		return mode.requestValue(), mode.label()
	}
	return "", ""
}

func providerReadiness(w *TargetConfig, base providerSetupState) providerSetupState {
	switch profile.ProviderID(base.ProviderSpec) {
	case profile.ProviderSpecZAI:
		return zaiReadiness(w, base)
	case profile.ProviderSpecChatGPT:
		return chatGPTReadiness(w, base)
	case profile.ProviderSpecBedrock:
		return bedrockReadiness(w, base)
	case profile.ProviderSpecAzure:
		return azureReadiness(w, base)
	case profile.ProviderSpecCustom:
		return customReadiness(w, base)
	default:
		return httpReadiness(w, base)
	}
}

func zaiReadiness(w *TargetConfig, base providerSetupState) providerSetupState {
	setup := base
	if _, err := routing.ParseZAIAccess(w.Draft.Get().ZAIAccess); err != nil {
		setup.Status = setupMissingLocator
		return setup
	}
	return httpReadiness(w, setup)
}

// genericCredentialRowVisible is the shared credential eligibility calculation
// for HTTP-style forms. Provider forms own their exceptional visibility rules.
func genericCredentialRowVisible(w *TargetConfig) bool {
	setup := w.setupState()
	if setup.RequiresLocator() {
		return false
	}
	if strings.TrimSpace(w.Draft.Get().CredentialRef) != "" {
		return true
	}
	provider, ok := providerProfileForSpec(setup.ProviderSpec)
	return setup.CredentialRequired || ok && provider.Credential.Requirement == profile.CredentialOptional
}

func providerAllowsNoCredential(w *TargetConfig) bool {
	spec := strings.TrimSpace(w.Draft.Get().ProviderSpec)
	if spec == "" {
		return false
	}
	setup := w.setupState()
	if setup.CredentialRequired {
		return false
	}
	baseURL := strings.TrimSpace(w.BaseURL.Get())
	if baseURL == "" {
		baseURL = profile.DefaultExecuteBaseURL(spec)
	}
	return !profile.RequiresCredential(spec, baseURL)
}

func httpReadiness(w *TargetConfig, base providerSetupState) providerSetupState {
	setup := base
	if setup.CredentialRequired && strings.TrimSpace(setup.CredentialRef) == "" {
		setup.Status = setupMissingCredential
		return setup
	}
	setup.Status = setupReady
	return setup
}

func (w *TargetConfig) endpointValueForProfile() string {
	if profile.ProviderID(w.Draft.Get().ProviderSpec) == profile.ProviderSpecBedrock {
		return strings.TrimSpace(w.BaseURL.Get())
	}
	if endpoint := strings.TrimSpace(w.BaseURL.Get()); endpoint != "" {
		return endpoint
	}
	if provider, ok := providerProfileForSpec(w.Draft.Get().ProviderSpec); ok && !profile.RequiresLocator(w.Draft.Get().ProviderSpec) {
		return strings.TrimSpace(provider.Locator.Default)
	}
	return ""
}

// CurrentProtocolOptions resolves the operator-facing protocol choices for the
// current draft + selected model. It is a derived accessor that reads reactive
// state, not a pure projection, so it lives in state.go rather than
// catalog_projection.go (projections must take plain values only).
func (w *TargetConfig) CurrentProtocolOptions() []protocolOption {
	return resolveProtocolOptions(w.Draft.Get().ProviderSpec, w.SelectedModel.Get())
}

func (w *TargetConfig) derivesProviderProtocol() bool {
	_, derived := profile.DerivedProtocolForSpec(w.Draft.Get().ProviderSpec)
	return derived
}

func (w *TargetConfig) catalogResult() readmodel.ModelCatalogReadModel { return w.Catalog.Get().Result }
func (w *TargetConfig) catalogLoading() bool                           { return w.Catalog.Get().Loading }
func (w *TargetConfig) catalogFailed() bool                            { return w.Catalog.Get().Err != "" }
func (w *TargetConfig) createFailed() bool                             { return w.SaveOperation.Get().Err != "" }
func (w *TargetConfig) readyToCreate() bool {
	protocolReady := w.derivesProviderProtocol() || strings.TrimSpace(w.Draft.Get().ProviderProtocol) != ""
	return w.setupState().Ready() &&
		w.modelSelectionValidated() &&
		protocolReady &&
		validateTargetDraftEndpoint(w.Draft.Get()) == nil
}

func (w *TargetConfig) modelSelectionValidated() bool {
	return strings.TrimSpace(w.SelectedModel.Get().ModelName) != ""
}

func (w *TargetConfig) authSessionPending() bool {
	return strings.EqualFold(strings.TrimSpace(w.AuthSession.Get().State), "pending")
}

func (w *TargetConfig) authSessionFailed() bool {
	state := strings.ToLower(strings.TrimSpace(w.AuthSession.Get().State))
	return state == "failed" || state == "expired"
}

func setupRequiresLocator(w *TargetConfig) bool      { return w.setupState().RequiresLocator() }
func setupRequiresCredential(w *TargetConfig) bool   { return w.setupState().RequiresCredential() }
func targetUsesInteractiveAuth(w *TargetConfig) bool { return w.RequiresInteractiveAuth() }
func targetCatalogLoading(w *TargetConfig) bool      { return w.catalogLoading() }
func targetCatalogFailed(w *TargetConfig) bool       { return w.catalogFailed() }
func targetCreateFailed(w *TargetConfig) bool        { return w.createFailed() }
func targetReadyToCreate(w *TargetConfig) bool       { return w.readyToCreate() }
func targetSaveVerb(w *TargetConfig) string          { return w.saveVerb() }
func targetAuthPending(w *TargetConfig) bool         { return w.authSessionPending() }
func targetAuthFailed(w *TargetConfig) bool          { return w.authSessionFailed() }

// Lifecycle is the target form's independent open/closed/completed lifecycle.
// Catalog, save, and ChatGPT login operations have their own state below.
type Lifecycle int

const (
	LifecycleClosed Lifecycle = iota
	LifecycleOpen
	LifecycleCreated
)

func (l Lifecycle) IsTerminal() bool {
	return l == LifecycleCreated
}

type catalogOperationState struct {
	Loading bool
	Result  readmodel.ModelCatalogReadModel
	Err     string
}

type createOperationState struct {
	Err string
}

type chatGPTAuthMode uint8

const (
	chatGPTAuthBrowser chatGPTAuthMode = iota
	chatGPTAuthDevice
)

func (m chatGPTAuthMode) requestValue() string {
	if m == chatGPTAuthDevice {
		return "device"
	}
	return "browser"
}

func (m chatGPTAuthMode) label() string {
	if m == chatGPTAuthDevice {
		return "device code"
	}
	return "browser login"
}

// state.go owns the target_config reactive state shape: the appState struct
// (the go-tui reactive fields embedded by TargetConfig), its constructor
// newStates, and app binding. It is the single source of truth for which fields
// are reactive — construction, reset, and BindApp all derive from this list.
// Adding a state field means adding it here once.

// appState is the subset of TargetConfig fields that are go-tui reactive state.
// It is the single source of truth for: construction (zero values), reset
// (back-to-initial on provider change / cancel), and app binding. Adding a
// state field means adding it here once — the constructor, resetFlowState, and
// BindApp all derive from this list.
type appState struct {
	Lifecycle   *tui.State[Lifecycle]
	DeleteArmed *tui.State[bool]
	Error       *tui.State[string]

	// Draft is the durable value under edit — the object we persist. Single
	// source of truth for the target; create starts from a zero draft, edit from
	// one seeded by TargetDraftFromReadModel. Same type either way.
	Draft *tui.State[readmodel.TargetDraft]

	BaseURL                *tui.State[string]
	CredentialHeaderEdited *tui.State[bool]
	ChatGPTAuthMode        *tui.State[chatGPTAuthMode]
	AuthSession            *tui.State[readmodel.AuthSessionReadModel]

	Catalog       *tui.State[catalogOperationState]
	SaveOperation *tui.State[createOperationState]

	SelectedModel *tui.State[readmodel.ModelDeploymentReadModel]

	Placement *tui.State[readmodel.PlacementOptionReadModel]
}

// bindApp binds every reactive state field to the live app. State is generic
// (*State[T] has no shared non-generic interface), so the field list is expressed
// as bind closures — one source of truth shared with newStates/resetState.
func (s appState) bindApp(app *tui.App) {
	s.Lifecycle.BindApp(app)
	s.DeleteArmed.BindApp(app)
	s.Error.BindApp(app)
	s.Draft.BindApp(app)
	s.BaseURL.BindApp(app)
	s.CredentialHeaderEdited.BindApp(app)
	s.ChatGPTAuthMode.BindApp(app)
	s.AuthSession.BindApp(app)
	s.Catalog.BindApp(app)
	s.SaveOperation.BindApp(app)
	s.SelectedModel.BindApp(app)
	s.Placement.BindApp(app)
}

// newStates builds the zero-value reactive state. Placement is left nil for the
// caller (route/target-derived) to set.
func newStates() appState {
	return appState{
		Lifecycle:   tui.NewState(LifecycleClosed),
		DeleteArmed: tui.NewState(false),
		Error:       tui.NewState(""),
		Draft:       tui.NewState(readmodel.TargetDraft{}),
		BaseURL:     tui.NewState(""),

		CredentialHeaderEdited: tui.NewState(false),
		ChatGPTAuthMode:        tui.NewState(chatGPTAuthBrowser),
		AuthSession:            tui.NewState(readmodel.AuthSessionReadModel{}),

		Catalog:       tui.NewState(catalogOperationState{}),
		SaveOperation: tui.NewState(createOperationState{}),

		SelectedModel: tui.NewState(readmodel.ModelDeploymentReadModel{}),

		Placement: nil, // set by caller (route/target-derived)
	}
}

// resetFlowState clears provider/setup/catalog state to switch providers or
// cancel. It delegates the bulk reactive-state reset to resetSetupState (single
// source of truth), then re-derives the route/target-dependent placement and
// drops non-state caches. Lifecycle, provider, and form error are caller-owned.
func (w *TargetConfig) resetFlowState() {
	w.stopAuthSessionObserver()
	w.resetSetupState()
	w.Placement.Set(defaultPlacementForRoute(w.Route))
	w.catalogProbeSeq++
}

func (w *TargetConfig) resetSetupState() {
	d := w.Draft.Get()
	d.ZAIAccess = ""
	d.CredentialRef = ""
	d.CredentialHeader = ""
	w.Draft.Set(d)
	w.BaseURL.Set("")
	w.CredentialHeaderEdited.Set(false)
	w.ChatGPTAuthMode.Set(chatGPTAuthBrowser)
	w.AuthSession.Set(readmodel.AuthSessionReadModel{})
	w.Catalog.Set(catalogOperationState{})
	w.SaveOperation.Set(createOperationState{})
	w.SelectedModel.Set(readmodel.ModelDeploymentReadModel{})
	w.Draft.Update(func(d readmodel.TargetDraft) readmodel.TargetDraft {
		d.ProviderProtocol = ""
		return d
	})
}
