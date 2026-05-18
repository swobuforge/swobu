package state

import stateModel "github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state/model"

// Public cockpit app model surface.
type Model = stateModel.Model
type CatalogEntry = stateModel.CatalogEntry
type EndpointSnapshot = stateModel.EndpointSnapshot
type ProviderConfigSnapshot = stateModel.ProviderConfigSnapshot
type TrafficRow = stateModel.TrafficRow
type ControlPlaneMismatch = stateModel.ControlPlaneMismatch
type RouteSetupFlowState = stateModel.RouteSetupFlowState
type RouteSetupSlotState = stateModel.RouteSetupSlotState

const (
	InteractionModeNAV        = stateModel.InteractionModeNAV
	InteractionModeEditText   = stateModel.InteractionModeEditText
	InteractionModePickOne    = stateModel.InteractionModePickOne
	InteractionModeManageList = stateModel.InteractionModeManageList
	InteractionModeBusySave   = stateModel.InteractionModeBusySave
	InteractionModeBusyLaunch = stateModel.InteractionModeBusyLaunch
	DraftProviderRef          = stateModel.DraftProviderRef
	RouteSetupSlotMissing     = stateModel.RouteSetupSlotMissing
	RouteSetupSlotBlocked     = stateModel.RouteSetupSlotBlocked
	RouteSetupSlotReady       = stateModel.RouteSetupSlotReady
	RouteSetupSlotExternal    = stateModel.RouteSetupSlotExternal
	RouteSetupSlotLoading     = stateModel.RouteSetupSlotLoading
	RouteSetupSlotFailed      = stateModel.RouteSetupSlotFailed
)

var ProviderConfigForSpec = stateModel.ProviderConfigForSpec
var ProviderRequiresCredential = stateModel.ProviderRequiresCredential
var ProviderSupportsCatalog = stateModel.ProviderSupportsCatalog
var ProviderOptions = stateModel.ProviderOptions
var ProviderCredentialSelectionRequired = stateModel.ProviderCredentialSelectionRequired
var ProviderModelCatalogLoadBlocked = stateModel.ProviderModelCatalogLoadBlocked
var ProviderModelCatalogBlockedMessage = stateModel.ProviderModelCatalogBlockedMessage
var ProviderModelCatalogAuthFailed = stateModel.ProviderModelCatalogAuthFailed
var ProviderModelCatalogAuthFailureMessage = stateModel.ProviderModelCatalogAuthFailureMessage
var EvaluateCreateDraftRouteSetup = stateModel.EvaluateCreateDraftRouteSetup
var CreateDraftCredentialStrategySelectable = stateModel.CreateDraftCredentialStrategySelectable
