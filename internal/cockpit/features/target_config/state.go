package target_config

import (
	"strings"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/profile"
)

// chatGPTReadiness is the ChatGPT arm of the feature's derived form status.
func chatGPTReadiness(w *TargetConfig, base providerSetupState) providerSetupState {
	setup := base
	setup.InteractiveAuth = true
	if label := interactiveAuthLabel(setup.AuthModes); label != "" {
		setup.CredentialLabel = label
	} else {
		setup.CredentialLabel = "browser login"
	}
	if strings.TrimSpace(setup.CredentialRef) == "" {
		setup.BlockReason = "auth first"
		return setup
	}
	setup.ReadyForCatalog = true
	return setup
}

// readiness_projection.go projects the current provider's setup readiness: the
// providerSetupState struct and setupState, which builds the provider-metadata
// baseline then applies the explicit provider readiness projection. Pure: no
// ports, no go-tui; recomputed every read, never stored.

// providerSetupState is the readiness projection consumed across the form:
// whether an endpoint/credential/auth-mode is required, whether the catalog is
// ready to probe, and what blocks it.
type providerSetupState struct {
	ProviderSpec       string
	DisplayName        string
	EndpointKind       profile.ProviderEndpointKind
	EndpointLabel      string
	CredentialLabel    string
	CredentialRef      string
	CredentialRequired bool
	AuthModeRequired   bool
	InteractiveAuth    bool
	AuthModes          []profile.AuthModeSpec
	RequiresEndpoint   bool
	ReadyForCatalog    bool
	BlockReason        string
}

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
	endpoint := provider.Endpoint
	label := strings.TrimSpace(endpoint.Label)
	if label == "" {
		label = "endpoint"
	}
	credentialRef := strings.TrimSpace(w.Draft.Get().CredentialRef) // swobu:io-string source=boundary
	return providerSetupState{
		ProviderSpec:       spec,
		DisplayName:        provider.ProviderDisplayName,
		EndpointKind:       endpoint.Kind,
		EndpointLabel:      label,
		CredentialRef:      credentialRef,
		CredentialRequired: profile.RequiresCredential(spec, endpointValue),
		AuthModes:          provider.AllowedAuthModes,
	}
}

func (w *TargetConfig) endpointValueFor(provider profile.Profile) string {
	endpointValue := strings.TrimSpace(w.BaseURL.Get()) // swobu:io-string source=boundary
	if endpointValue == "" && !profile.RequiresExplicitEndpoint(string(provider.ProviderID)) {
		endpointValue = strings.TrimSpace(provider.Endpoint.DefaultURL)
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

func interactiveAuthLabel(modes []profile.AuthModeSpec) string {
	for _, mode := range modes {
		if !mode.Interactive {
			continue
		}
		return authModeLabel(mode.Mode)
	}
	return ""
}

// RequiresInteractiveAuth reports whether the current provider uses interactive
// (browser/device) auth. Derives from the readiness projection when a provider
// is set, else from the supported auth modes for the draft spec.
func (w *TargetConfig) RequiresInteractiveAuth() bool {
	setup := w.setupState()
	if setup.ProviderSpec != "" {
		return setup.InteractiveAuth
	}
	mode, _ := w.interactiveAuthMode()
	return mode != ""
}

// interactiveAuthMode resolves the interactive auth mode + its display label for
// the current draft spec (preferring the readiness projection's allowed modes).
func (w *TargetConfig) interactiveAuthMode() (profile.AuthMode, string) {
	setup := w.setupState()
	if setup.ProviderSpec != "" && len(setup.AuthModes) > 0 {
		for _, mode := range setup.AuthModes {
			if !mode.Interactive {
				continue
			}
			return mode.Mode, authModeLabel(mode.Mode)
		}
	}
	for _, mode := range profile.SupportedAuthModesForSpec(w.Draft.Get().ProviderSpec) {
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

func providerReadiness(w *TargetConfig, base providerSetupState) providerSetupState {
	switch profile.ProviderID(base.ProviderSpec) {
	case profile.ProviderSpecChatGPT:
		return chatGPTReadiness(w, base)
	case profile.ProviderSpecBedrock:
		return bedrockReadiness(w, base)
	case profile.ProviderSpecAzure:
		return azureReadiness(w, base)
	case profile.ProviderSpecOpenAICompatible:
		return openAICompatibleReadiness(w, base)
	default:
		return httpReadiness(w, base)
	}
}

// genericCredentialRowVisible is the shared credential eligibility calculation
// for HTTP-style forms. Provider forms own their exceptional visibility rules.
func genericCredentialRowVisible(w *TargetConfig) bool {
	setup := w.setupState()
	if setup.RequiresEndpoint || setup.AuthModeRequired {
		return false
	}
	if strings.TrimSpace(w.Draft.Get().CredentialRef) != "" {
		return true
	}
	return setup.CredentialRequired
}

func providerAllowsNoCredential(w *TargetConfig) bool {
	spec := strings.TrimSpace(w.Draft.Get().ProviderSpec)
	if spec == "" {
		return false
	}
	setup := w.setupState()
	if setup.AuthModeRequired || setup.CredentialRequired {
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
		setup.CredentialLabel = "enter credential"
		setup.BlockReason = setup.CredentialLabel
		return setup
	}
	setup.CredentialLabel = strings.TrimSpace(setup.CredentialRef)
	if setup.CredentialLabel == "" && !profile.RequiresCredential(setup.ProviderSpec, w.endpointValueForProfile()) {
		setup.CredentialLabel = "none"
	}
	setup.ReadyForCatalog = true
	return setup
}

func (w *TargetConfig) endpointValueForProfile() string {
	if endpoint := strings.TrimSpace(w.BaseURL.Get()); endpoint != "" {
		return endpoint
	}
	if provider, ok := providerProfileForSpec(w.Draft.Get().ProviderSpec); ok && !profile.RequiresExplicitEndpoint(w.Draft.Get().ProviderSpec) {
		return strings.TrimSpace(provider.Endpoint.DefaultURL)
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

// Phase is the target-config workflow's durable state machine.
type Phase int

const (
	PhaseClosed Phase = iota
	PhaseConfiguring
	PhaseAuthPending
	PhaseLoadingCatalog
	PhaseReadyToCreate
	PhaseCreated
	PhaseCatalogFailed
	PhaseAuthFailed
	PhaseCreateFailed
)

func (p Phase) IsTerminal() bool {
	return p == PhaseCreated
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
	Phase       *tui.State[Phase]
	DeleteArmed *tui.State[bool]
	Error       *tui.State[string]

	// Draft is the durable value under edit — the object we persist. Single
	// source of truth for the target; create starts from a zero draft, edit from
	// one seeded by TargetDraftFromReadModel. Same type either way.
	Draft *tui.State[readmodel.TargetDraft]

	BaseURL                *tui.State[string]
	CredentialHeaderEdited *tui.State[bool]
	AuthSession            *tui.State[readmodel.AuthSessionReadModel]

	Catalog        *tui.State[readmodel.ModelCatalogReadModel]
	CatalogLoading *tui.State[bool]

	SelectedModel *tui.State[readmodel.ModelDeploymentReadModel]

	Placement *tui.State[readmodel.PlacementOptionReadModel]
}

// bindApp binds every reactive state field to the live app. State is generic
// (*State[T] has no shared non-generic interface), so the field list is expressed
// as bind closures — one source of truth shared with newStates/resetState.
func (s appState) bindApp(app *tui.App) {
	s.Phase.BindApp(app)
	s.DeleteArmed.BindApp(app)
	s.Error.BindApp(app)
	s.Draft.BindApp(app)
	s.BaseURL.BindApp(app)
	s.CredentialHeaderEdited.BindApp(app)
	s.AuthSession.BindApp(app)
	s.Catalog.BindApp(app)
	s.CatalogLoading.BindApp(app)
	s.SelectedModel.BindApp(app)
	s.Placement.BindApp(app)
}

// newStates builds the zero-value reactive state. Placement is left nil for the
// caller (route/target-derived) to set.
func newStates() appState {
	return appState{
		Phase:       tui.NewState(PhaseClosed),
		DeleteArmed: tui.NewState(false),
		Error:       tui.NewState(""),
		Draft:       tui.NewState(readmodel.TargetDraft{}),
		BaseURL:     tui.NewState(""),

		CredentialHeaderEdited: tui.NewState(false),
		AuthSession:            tui.NewState(readmodel.AuthSessionReadModel{}),

		Catalog:        tui.NewState(readmodel.ModelCatalogReadModel{}),
		CatalogLoading: tui.NewState(false),

		SelectedModel: tui.NewState(readmodel.ModelDeploymentReadModel{}),

		Placement: nil, // set by caller (route/target-derived)
	}
}

// resetFlowState clears provider/setup/catalog state to switch providers or
// cancel. It delegates the bulk reactive-state reset to resetSetupState (single
// source of truth), then re-derives the route/target-dependent placement and
// drops non-state caches. Phase/Provider/Error are left for the caller to set.
func (w *TargetConfig) resetFlowState() {
	w.resetSetupState()
	w.Placement.Set(defaultPlacementForRoute(w.Route))
	w.catalogProbeSeq++
}

func (w *TargetConfig) resetSetupState() {
	d := w.Draft.Get()
	d.CredentialRef = ""
	d.ProviderOptions = readmodel.ProviderOptionsDraft{}
	w.Draft.Set(d)
	w.BaseURL.Set("")
	w.CredentialHeaderEdited.Set(false)
	w.AuthSession.Set(readmodel.AuthSessionReadModel{})
	w.Catalog.Set(readmodel.ModelCatalogReadModel{})
	w.CatalogLoading.Set(false)
	w.SelectedModel.Set(readmodel.ModelDeploymentReadModel{})
	w.Draft.Update(func(d readmodel.TargetDraft) readmodel.TargetDraft {
		d.ProviderProtocol = ""
		return d
	})
}
