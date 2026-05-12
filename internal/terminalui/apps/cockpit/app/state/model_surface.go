package state

import stateModel "github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state/model"

// Public cockpit app model surface.
type Model = stateModel.Model
type CatalogEntry = stateModel.CatalogEntry
type EndpointSnapshot = stateModel.EndpointSnapshot
type ProviderConfigSnapshot = stateModel.ProviderConfigSnapshot
type TrafficRow = stateModel.TrafficRow
type ControlPlaneMismatch = stateModel.ControlPlaneMismatch

const (
	InteractionModeNAV        = stateModel.InteractionModeNAV
	InteractionModeEditText   = stateModel.InteractionModeEditText
	InteractionModePickOne    = stateModel.InteractionModePickOne
	InteractionModeManageList = stateModel.InteractionModeManageList
	InteractionModeBusySave   = stateModel.InteractionModeBusySave
	InteractionModeBusyLaunch = stateModel.InteractionModeBusyLaunch
	DraftProviderRef          = stateModel.DraftProviderRef
)

var ProviderConfigForSpec = stateModel.ProviderConfigForSpec
var ProviderRequiresCredential = stateModel.ProviderRequiresCredential
var ProviderSupportsCatalog = stateModel.ProviderSupportsCatalog
var ProviderOptions = stateModel.ProviderOptions
var ProviderCredentialSelectionRequired = stateModel.ProviderCredentialSelectionRequired
var ProviderModelCatalogLoadBlocked = stateModel.ProviderModelCatalogLoadBlocked
var ProviderModelCatalogBlockedMessage = stateModel.ProviderModelCatalogBlockedMessage
