package state

import (
	stateEffect "github.com/metrofun/swobu/internal/adapters/inbound/tui/app/state/effect"
	stateIntent "github.com/metrofun/swobu/internal/adapters/inbound/tui/app/state/intent"
	stateModel "github.com/metrofun/swobu/internal/adapters/inbound/tui/app/state/model"
)

// Model aliases

type Model = stateModel.Model

type CatalogSnapshot = stateModel.CatalogSnapshot

type EndpointSnapshot = stateModel.EndpointSnapshot

type ProviderConfigSnapshot = stateModel.ProviderConfigSnapshot

type CatalogEntry = stateModel.CatalogEntry

type StatusProjectionSnapshot = stateModel.StatusProjectionSnapshot

type TrafficRow = stateModel.TrafficRow
type ControlPlaneMismatch = stateModel.ControlPlaneMismatch

type ProviderOption = stateModel.ProviderOption

const DraftProviderRef = stateModel.DraftProviderRef
const (
	InteractionModeNAV        = stateModel.InteractionModeNAV
	InteractionModeEditText   = stateModel.InteractionModeEditText
	InteractionModePickOne    = stateModel.InteractionModePickOne
	InteractionModeManageList = stateModel.InteractionModeManageList
	InteractionModeBusySave   = stateModel.InteractionModeBusySave
	InteractionModeBusyLaunch = stateModel.InteractionModeBusyLaunch
)

var ProviderOptions = stateModel.ProviderOptions
var ProviderConfigForSpec = stateModel.ProviderConfigForSpec
var ProviderRequiresCredential = stateModel.ProviderRequiresCredential
var ProviderSupportsCatalog = stateModel.ProviderSupportsCatalog

// Intent aliases

type SelectEndpoint = stateIntent.SelectEndpoint

type CreateEndpoint = stateIntent.CreateEndpoint

type SetCreateDraftName = stateIntent.SetCreateDraftName

type SetCreateDraftProviderSpec = stateIntent.SetCreateDraftProviderSpec

type SetCreateDraftModelID = stateIntent.SetCreateDraftModelID

type SetCreateDraftCredentialRef = stateIntent.SetCreateDraftCredentialRef

type SetCreateDraftBaseURL = stateIntent.SetCreateDraftBaseURL

type SetCreateDraftTargetAlias = stateIntent.SetCreateDraftTargetAlias

type RenameCurrentEndpoint = stateIntent.RenameCurrentEndpoint

type ToggleStream = stateIntent.ToggleStream
type SetInteractionMode = stateIntent.SetInteractionMode
type SetFocusedRowAffordance = stateIntent.SetFocusedRowAffordance
type FocusNextAfterRebuildRequested = stateIntent.FocusNextAfterRebuildRequested

type WorkspaceCreateRequested = stateIntent.WorkspaceCreateRequested

type WorkspaceRenameRequested = stateIntent.WorkspaceRenameRequested
type WorkspaceDeleteRequested = stateIntent.WorkspaceDeleteRequested

type RoutingSaveStartedAction = stateIntent.RoutingSaveStartedAction

type ClientAccessCheckStarted = stateIntent.ClientAccessCheckStarted

type EndpointCopyRequested = stateIntent.EndpointCopyRequested

type ClientBaseURLCopyRequested = stateIntent.ClientBaseURLCopyRequested

type ClientLaunchRequested = stateIntent.ClientLaunchRequested

type RefreshStatusProjectionRequested = stateIntent.RefreshStatusProjectionRequested
type SetHelpTabOpenAction = stateIntent.SetHelpTabOpenAction
type OpenSupportLinkRequested = stateIntent.OpenSupportLinkRequested
type HelpDiagnosticsCopyRequested = stateIntent.HelpDiagnosticsCopyRequested
type CompatibilityRestartRequested = stateIntent.CompatibilityRestartRequested
type CompatibilityDiagnosticsCopyRequested = stateIntent.CompatibilityDiagnosticsCopyRequested

type SaveSelectedTargetRequested = stateIntent.SaveSelectedTargetRequested

type SaveProviderConfigRequested = stateIntent.SaveProviderConfigRequested
type AddProviderConfigRequested = stateIntent.AddProviderConfigRequested
type DeleteProviderConfigRequested = stateIntent.DeleteProviderConfigRequested
type StoreKeychainCredentialRequested = stateIntent.StoreKeychainCredentialRequested
type LoadCreateDraftModelCatalogRequested = stateIntent.LoadCreateDraftModelCatalogRequested

// Effect command aliases

type RefreshDaemonStatusEffect = stateEffect.RefreshDaemonStatusEffect

type RefreshCatalogEffect = stateEffect.RefreshCatalogEffect

type RefreshEndpointsEffect = stateEffect.RefreshEndpointsEffect

type RefreshStatusProjectionEffect = stateEffect.RefreshStatusProjectionEffect

type ScheduleDaemonRefreshEffect = stateEffect.ScheduleDaemonRefreshEffect

type SaveWorkspaceNameEffect = stateEffect.SaveWorkspaceNameEffect

type SaveNewWorkspaceEffect = stateEffect.SaveNewWorkspaceEffect
type DeleteWorkspaceEffect = stateEffect.DeleteWorkspaceEffect

type SaveSelectedTargetEffect = stateEffect.SaveSelectedTargetEffect

type SaveProviderConfigEffect = stateEffect.SaveProviderConfigEffect
type AddProviderConfigEffect = stateEffect.AddProviderConfigEffect
type DeleteProviderConfigEffect = stateEffect.DeleteProviderConfigEffect
type StoreKeychainCredentialEffect = stateEffect.StoreKeychainCredentialEffect

type CheckClientAccessEffect = stateEffect.CheckClientAccessEffect

type CopyEndpointValueEffect = stateEffect.CopyEndpointValueEffect

type CopyClientBaseURLEffect = stateEffect.CopyClientBaseURLEffect

type LaunchClientEffect = stateEffect.LaunchClientEffect
type FocusNextAfterRebuildEffect = stateEffect.FocusNextAfterRebuildEffect
type LoadCreateDraftModelCatalogEffect = stateEffect.LoadCreateDraftModelCatalogEffect
type CompatibilityRestartHintEffect = stateEffect.CompatibilityRestartHintEffect
type CopyCompatibilityDiagnosticsEffect = stateEffect.CopyCompatibilityDiagnosticsEffect
type OpenSupportLinkEffect = stateEffect.OpenSupportLinkEffect
type CopyHelpDiagnosticsEffect = stateEffect.CopyHelpDiagnosticsEffect

// Effect result action aliases

type DaemonStatusLoadFailed = stateEffect.DaemonStatusLoadFailed

type ReplaceDaemonStatus = stateEffect.ReplaceDaemonStatus
type ControlPlaneIncompatibleDetected = stateEffect.ControlPlaneIncompatibleDetected

type CatalogLoadFailed = stateEffect.CatalogLoadFailed

type ReplaceCatalog = stateEffect.ReplaceCatalog

type EndpointsLoadFailed = stateEffect.EndpointsLoadFailed

type ReplaceEndpoints = stateEffect.ReplaceEndpoints

type TrafficLoadFailed = stateEffect.TrafficLoadFailed

type ReplaceStatusProjection = stateEffect.ReplaceStatusProjection

type DaemonRefreshTick = stateEffect.DaemonRefreshTick
type CompatibilityRecoveryNoted = stateEffect.CompatibilityRecoveryNoted
type SupportLinkNoted = stateEffect.SupportLinkNoted
type HelpDiagnosticsCopyNoted = stateEffect.HelpDiagnosticsCopyNoted

type WorkspaceSaveFailed = stateEffect.WorkspaceSaveFailed

type WorkspaceSaveSucceeded = stateEffect.WorkspaceSaveSucceeded
type WorkspaceDeleteFailed = stateEffect.WorkspaceDeleteFailed
type WorkspaceDeleteSucceeded = stateEffect.WorkspaceDeleteSucceeded

type RoutingSaveFailed = stateEffect.RoutingSaveFailed

type RoutingSaveSucceeded = stateEffect.RoutingSaveSucceeded

type RoutingMutationSaved = stateEffect.RoutingMutationSaved
type KeychainCredentialStored = stateEffect.KeychainCredentialStored

type EndpointCopyNoted = stateEffect.EndpointCopyNoted

type ClientCopyNoted = stateEffect.ClientCopyNoted

type ClientLaunchNoted = stateEffect.ClientLaunchNoted

type ClientAccessCheckFailed = stateEffect.ClientAccessCheckFailed

type ClientAccessChecked = stateEffect.ClientAccessChecked
type CreateDraftModelCatalogLoaded = stateEffect.CreateDraftModelCatalogLoaded
