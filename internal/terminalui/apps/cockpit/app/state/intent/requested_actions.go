// Actions owned by the cockpit state machine.
// Each type represents one semantic state change.
package intent

import stateModel "github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state/model"

// --- User input actions ---

type SelectEndpoint struct {
	Name string
}

type CreateEndpoint struct {
	Name string
}

type SetCreateDraftName struct {
	Name string
}

type SetCreateDraftProviderSpec struct {
	ProviderSpec string
}

type SetCreateDraftModelIDAction struct {
	ModelID string
}

type SetCreateDraftCredentialRef struct {
	CredentialRef string
}

type SetCreateDraftBaseURL struct {
	BaseURL string
}

type SetCreateDraftTargetAlias struct {
	TargetAlias string
}

type SetCreateDraftSelectedFrame struct {
	SelectedFrame string
}

type RenameCurrentEndpoint struct {
	Name string
}

type ToggleStream struct{}

type SetInteractionMode struct {
	Mode string
}

// SetFocusedRowAffordance updates footer-derivation context from focused row.
type SetFocusedRowAffordance struct {
	Verb       string
	AllowSpace bool
}

// FocusNextAfterRebuildRequested asks reducer to schedule a deferred
// focus-next action after rebuild, so newly opened section children exist.
type FocusNextAfterRebuildRequested struct{}

// --- Workflow trigger actions ---

type WorkspaceCreateRequested struct {
	Name string
}

type WorkspaceRenameRequested struct {
	CurrentName string
	Name        string
}

type WorkspaceDeleteRequested struct {
	Name string
}

type RoutingSaveStartedAction struct{}

type ClientAccessCheckStarted struct{}

// --- Request actions (reducer maps these to Effects) ---

// EndpointCopyRequested asks the reducer to copy an endpoint value.
type EndpointCopyRequested struct{ Value string }

// AuthSessionURLCopyRequested asks reducer to copy auth session URL and report
// status near auth rows.
type AuthSessionURLCopyRequested struct{ Value string }
type AuthSessionURLCopyScopedRequested struct {
	OwnerKey string
	Value    string
}

// ClientBaseURLCopyRequestedAction asks the reducer to copy a client base URL.
type ClientBaseURLCopyRequestedAction struct{ Value string }

// ClientLaunchRequestedAction asks the reducer to launch a local client preset.
type ClientLaunchRequestedAction struct {
	BaseURL string
	Preset  string
	ModelID string
}

// RefreshStatusProjectionRequested asks the reducer to refresh traffic data.
type RefreshStatusProjectionRequested struct{}

// SetHelpTabOpenAction asks reducer to open/close the global help tab.
type SetHelpTabOpenAction struct{ Open bool }

// OpenSupportLinkRequested asks reducer to open a support URL and capture
// fallback text for operators when opening fails.
type OpenSupportLinkRequested struct {
	Label string
	URL   string
}

// HelpDiagnosticsCopyRequested asks reducer to copy diagnostics text.
type HelpDiagnosticsCopyRequested struct{ Text string }

// CompatibilityRestartRequested asks reducer to trigger recovery guidance for
// a daemon/TUI control-plane incompatibility.
type CompatibilityRestartRequested struct{}

// CompatibilityDiagnosticsCopyRequested asks reducer to copy incompatibility
// diagnostics for operator support.
type CompatibilityDiagnosticsCopyRequested struct{}

// SaveSelectedTargetRequested asks the reducer to save a selected target.
type SaveSelectedTargetRequested struct {
	EndpointName string
	ProviderRef  string
	ErrorAnchor  string
}

// SaveProviderConfigRequested asks the reducer to save a provider config.
type SaveProviderConfigRequested struct {
	EndpointName   string
	ProviderConfig stateModel.ProviderConfigSnapshot
	ErrorAnchor    string
}

// AddProviderConfigRequested asks the reducer to append a new provider config
// and set it as primary.
type AddProviderConfigRequested struct {
	EndpointName   string
	ProviderConfig stateModel.ProviderConfigSnapshot
	ErrorAnchor    string
}

// DeleteProviderConfigRequested asks the reducer to delete one provider config.
type DeleteProviderConfigRequested struct {
	EndpointName string
	ProviderRef  string
	ErrorAnchor  string
}

// StoreKeychainCredentialRequested asks reducer to persist a keychain secret.
type StoreKeychainCredentialRequested struct {
	ProviderSpec string
	KeyName      string
	Secret       string
	ErrorAnchor  string
}

// StartProviderAuthSessionRequested asks reducer to start provider login/auth
// session for one provider config and resolve a credential reference.
type StartProviderAuthSessionRequested struct {
	EndpointName   string
	ProviderConfig stateModel.ProviderConfigSnapshot
	OwnerKey       string
	AuthScope      string
}

// ResetAuthSessionUIRequestedAction clears transient auth-login presentation state.
type ResetAuthSessionUIRequestedAction struct{}

// ResetAddModelAuthUIRequested clears transient add-model auth presentation
// state without touching persisted-model auth rows.
type ResetAddModelAuthUIRequested struct{}

const (
	RoutingModelCatalogScopeCreateDraft   = "create_draft"
	RoutingModelCatalogScopeAddModelDraft = "add_model_draft"
)

// LoadRoutingModelCatalogRequestedAction asks reducer to load provider-backed
// model catalog choices for routing composition across scopes.
type LoadRoutingModelCatalogRequestedAction struct {
	Scope         string
	ProviderSpec  string
	BaseURL       string
	CredentialRef string
}
