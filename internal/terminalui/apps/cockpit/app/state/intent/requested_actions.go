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

type SetCreateDraftModelID struct {
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

// ClientBaseURLCopyRequestedAction asks the reducer to copy a client base URL.
type ClientBaseURLCopyRequestedAction struct{ Value string }

// ClientLaunchRequestedAction asks the reducer to launch a local client preset.
type ClientLaunchRequestedAction struct {
	BaseURL string
	Preset  string
	ModelID string
}

// Back-compat aliases for existing call sites.
type ClientBaseURLCopyRequested = ClientBaseURLCopyRequestedAction
type ClientLaunchRequested = ClientLaunchRequestedAction

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
}

// SaveProviderConfigRequested asks the reducer to save a provider config.
type SaveProviderConfigRequested struct {
	EndpointName   string
	ProviderConfig stateModel.ProviderConfigSnapshot
}

// AddProviderConfigRequested asks the reducer to append a new provider config
// and set it as primary.
type AddProviderConfigRequested struct {
	EndpointName   string
	ProviderConfig stateModel.ProviderConfigSnapshot
}

// DeleteProviderConfigRequested asks the reducer to delete one provider config.
type DeleteProviderConfigRequested struct {
	EndpointName string
	ProviderRef  string
}

// StoreKeychainCredentialRequested asks reducer to persist a keychain secret.
type StoreKeychainCredentialRequested struct {
	ProviderSpec string
	KeyName      string
	Secret       string
}

// LoadCreateDraftModelCatalogRequested asks reducer to load provider-backed
// model catalog choices for first-run draft routing composition.
type LoadCreateDraftModelCatalogRequested struct {
	ProviderSpec  string
	BaseURL       string
	CredentialRef string
	ProtocolKind  string
}

// LoadAddModelDraftModelCatalogRequested asks reducer to load provider-backed
// model catalog choices for workspace add-model draft routing composition.
type LoadAddModelDraftModelCatalogRequested struct {
	ProviderSpec  string
	BaseURL       string
	CredentialRef string
	ProtocolKind  string
}
