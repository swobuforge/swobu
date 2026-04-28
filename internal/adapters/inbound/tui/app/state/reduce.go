package state

import (
	"slices"
	"strings"
	"time"

	"github.com/metrofun/swobu/internal/adapters/inbound/tui/engine/update"
)

// Reduce owns the first design-conforming cockpit's durable app-state updates.
func Reduce(model *Model, action update.Action) []update.Effect {
	if model.ControlPlane != nil && !allowWhileControlPlaneIncompatible(action) {
		return nil
	}
	if reduceEndpointSelection(model, action) {
		return nil
	}
	if reduceCatalogState(model, action) {
		return nil
	}
	if reduceDaemonState(model, action) {
		return nil
	}
	if e := reduceClientAndTrafficState(model, action); e != nil {
		return e
	}
	if e := reduceWorkspaceSaveState(model, action); e != nil {
		return e
	}
	if e := reduceRoutingSaveState(model, action); e != nil {
		return e
	}
	if e := reduceBehaviorState(model, action); e != nil {
		return e
	}
	return nil
}

func reduceEndpointSelection(model *Model, action update.Action) bool {
	switch value := action.(type) {
	case SelectEndpoint:
		model.CurrentEndpoint = strings.TrimSpace(value.Name)
		model.InteractionMode = InteractionModeNAV
		model.FooterShowTabs = strings.TrimSpace(model.CurrentEndpoint) != "" || len(model.Endpoints) > 0
		model.WorkspaceCopyNote = ""
		model.ClientCopyNote = ""
		model.ClientLaunchNote = ""
		model.ClientAccessNote = ""
		model.HelpTabOpen = false
		model.HelpNote = ""
		return true
	case CreateEndpoint:
		name := strings.TrimSpace(value.Name)
		if name == "" || containsString(model.Endpoints, name) {
			return true
		}
		model.Endpoints = append(model.Endpoints, name)
		slices.Sort(model.Endpoints)
		model.CurrentEndpoint = name
		model.InteractionMode = InteractionModeNAV
		model.FooterShowTabs = true
		model.WorkspaceCopyNote = ""
		model.ClientCopyNote = ""
		model.ClientLaunchNote = ""
		model.ClientAccessNote = ""
		model.HelpTabOpen = false
		model.HelpNote = ""
		return true
	case SetCreateDraftName:
		model.CreateDraftName = strings.TrimSpace(value.Name)
		model.WorkspaceSaveError = ""
		refreshFirstRunFooterAffordance(model)
		return true
	case SetCreateDraftProviderSpec:
		model.CreateDraftProviderConfig = ProviderConfigForSpec(value.ProviderSpec, model.CreateDraftProviderConfig)
		model.WorkspaceSaveError = ""
		model.CreateDraftModelIDs = nil
		model.CreateDraftModelError = ""
		refreshFirstRunFooterAffordance(model)
		return true
	case SetCreateDraftModelID:
		model.CreateDraftProviderConfig.ModelID = strings.TrimSpace(value.ModelID)
		model.WorkspaceSaveError = ""
		refreshFirstRunFooterAffordance(model)
		return true
	case SetCreateDraftCredentialRef:
		model.CreateDraftProviderConfig.CredentialRef = strings.TrimSpace(value.CredentialRef)
		model.WorkspaceSaveError = ""
		model.CreateDraftModelIDs = nil
		model.CreateDraftModelError = ""
		refreshFirstRunFooterAffordance(model)
		return true
	case SetCreateDraftBaseURL:
		model.CreateDraftProviderConfig.BaseURL = strings.TrimSpace(value.BaseURL)
		model.WorkspaceSaveError = ""
		model.CreateDraftModelIDs = nil
		model.CreateDraftModelError = ""
		refreshFirstRunFooterAffordance(model)
		return true
	case SetCreateDraftTargetAlias:
		model.CreateDraftProviderConfig.TargetAlias = strings.TrimSpace(strings.ToLower(value.TargetAlias))
		model.WorkspaceSaveError = ""
		refreshFirstRunFooterAffordance(model)
		return true
	case RenameCurrentEndpoint:
		next := strings.TrimSpace(value.Name)
		current := strings.TrimSpace(model.CurrentEndpoint)
		if current == "" || next == "" || current == next {
			return true
		}
		applyWorkspaceRename(model, current, next)
		model.WorkspaceCopyNote = ""
		return true
	default:
		return false
	}
}

func reduceCatalogState(model *Model, action update.Action) bool {
	switch value := action.(type) {
	case ReplaceCatalog:
		hadEndpoints := len(model.Endpoints) > 0
		model.Catalog = cloneCatalogEntries(value.Entries)
		model.CatalogError = ""
		if len(model.EndpointSnapshots) == 0 {
			model.Endpoints = endpointNames(model.Catalog)
		}
		model.CurrentEndpoint = reconcileCurrentEndpoint(model.CurrentEndpoint, model.Endpoints, hadEndpoints)
		model.FooterShowTabs = strings.TrimSpace(model.CurrentEndpoint) != "" || len(model.Endpoints) > 0
		refreshFirstRunFooterAffordance(model)
		return true
	case ReplaceEndpoints:
		hadEndpoints := len(model.Endpoints) > 0
		model.EndpointSnapshots = cloneEndpointSnapshots(value.Snapshots)
		model.Endpoints = endpointSnapshotNames(model.EndpointSnapshots)
		model.CurrentEndpoint = reconcileCurrentEndpoint(model.CurrentEndpoint, model.Endpoints, hadEndpoints)
		model.FooterShowTabs = strings.TrimSpace(model.CurrentEndpoint) != "" || len(model.Endpoints) > 0
		refreshFirstRunFooterAffordance(model)
		model.WorkspaceCopyNote = ""
		model.ClientCopyNote = ""
		model.ClientLaunchNote = ""
		model.ClientAccessNote = ""
		return true
	case EndpointsLoadFailed:
		if len(model.Endpoints) == 0 {
			model.DaemonHint = strings.TrimSpace(value.Message)
		}
		return true
	case CatalogLoadFailed:
		model.Catalog = nil
		model.CatalogError = value.Message
		if len(model.EndpointSnapshots) == 0 {
			model.Endpoints = nil
			model.CurrentEndpoint = ""
		}
		return true
	default:
		return false
	}
}

func reduceClientAndTrafficState(model *Model, action update.Action) []update.Effect {
	switch value := action.(type) {
	case ReplaceStatusProjection:
		model.TrafficRows = cloneTrafficRows(value.Rows)
		model.TrafficError = ""
		return nil
	case TrafficLoadFailed:
		model.TrafficRows = nil
		model.TrafficError = strings.TrimSpace(value.Message)
		return nil
	case SupportLinkNoted:
		model.HelpNote = strings.TrimSpace(value.Message)
		return nil
	case HelpDiagnosticsCopyNoted:
		model.HelpNote = strings.TrimSpace(value.Message)
		return nil
	case ClientCopyNoted:
		model.ClientCopyNote = strings.TrimSpace(value.Message)
		return nil
	case ClientLaunchNoted:
		model.HeaderStatus = "ready"
		model.InteractionMode = InteractionModeNAV
		model.ClientLaunchNote = strings.TrimSpace(value.Message)
		return nil
	case ClientAccessCheckStarted:
		model.HeaderStatus = "checking..."
		model.InteractionMode = InteractionModeBusySave
		model.ClientAccessNote = ""
		ep := currentEndpoint(model)
		sp := currentSelectedProviderConfig(model)
		if strings.TrimSpace(ep) == "" || sp == nil {
			return nil
		}
		return []update.Effect{CheckClientAccessEffect{
			EndpointName:   ep,
			ProviderConfig: *sp,
		}}
	case ClientAccessChecked:
		model.HeaderStatus = "ready"
		model.InteractionMode = InteractionModeNAV
		model.ClientAccessStatus = strings.TrimSpace(value.Status)
		model.ClientAccessNote = strings.TrimSpace(value.Message)
		return []update.Effect{refreshStatusProjectionEffectFor(model)}
	case ClientAccessCheckFailed:
		model.HeaderStatus = "ready"
		model.InteractionMode = InteractionModeNAV
		model.ClientAccessStatus = "check failed"
		model.ClientAccessNote = strings.TrimSpace(value.Message)
		return nil
	default:
		return nil
	}
}

func reduceWorkspaceSaveState(model *Model, action update.Action) []update.Effect {
	switch value := action.(type) {
	case WorkspaceCreateRequested:
		model.HeaderStatus = "saving…"
		model.InteractionMode = InteractionModeBusySave
		model.WorkspaceSaveError = ""
		model.WorkspaceCopyNote = ""
		model.RoutingSaveError = ""
		model.ClientAccessNote = ""
		return []update.Effect{SaveNewWorkspaceEffect{
			Name:           strings.TrimSpace(value.Name),
			ProviderConfig: model.CreateDraftProviderConfig,
		}}
	case WorkspaceRenameRequested:
		model.HeaderStatus = "saving…"
		model.InteractionMode = InteractionModeBusySave
		model.WorkspaceSaveError = ""
		model.WorkspaceCopyNote = ""
		model.RoutingSaveError = ""
		model.ClientAccessNote = ""
		return []update.Effect{SaveWorkspaceNameEffect{
			CurrentName: strings.TrimSpace(value.CurrentName),
			Name:        strings.TrimSpace(value.Name),
		}}
	case WorkspaceSaveSucceeded:
		model.HeaderStatus = "saved"
		model.InteractionMode = InteractionModeNAV
		model.FooterVerb = "edit"
		model.FooterAllowSpace = false
		model.FooterShowTabs = true
		model.WorkspaceSaveError = ""
		model.WorkspaceCopyNote = ""
		if strings.TrimSpace(value.PreviousName) == "" {
			applyWorkspaceCreate(model, strings.TrimSpace(value.Name))
		} else {
			applyWorkspaceRename(model, strings.TrimSpace(value.PreviousName), strings.TrimSpace(value.Name))
		}
		model.CreateDraftName = ""
		model.CreateDraftProviderConfig = ProviderConfigSnapshot{}
		return []update.Effect{
			RefreshEndpointsEffect{},
			RefreshCatalogEffect{},
			ScheduleDaemonRefreshEffect{Delay: 350 * time.Millisecond},
		}
	case WorkspaceDeleteRequested:
		name := strings.TrimSpace(value.Name)
		if name == "" {
			return nil
		}
		model.HeaderStatus = "saving…"
		model.InteractionMode = InteractionModeBusySave
		model.WorkspaceSaveError = ""
		model.WorkspaceCopyNote = ""
		model.RoutingSaveError = ""
		model.ClientAccessNote = ""
		return []update.Effect{DeleteWorkspaceEffect{Name: name}}
	case WorkspaceDeleteSucceeded:
		model.HeaderStatus = "saved"
		model.InteractionMode = InteractionModeNAV
		model.WorkspaceSaveError = ""
		model.WorkspaceCopyNote = ""
		applyWorkspaceDelete(model, strings.TrimSpace(value.Name))
		return []update.Effect{
			RefreshEndpointsEffect{},
			RefreshCatalogEffect{},
			ScheduleDaemonRefreshEffect{Delay: 350 * time.Millisecond},
		}
	case WorkspaceDeleteFailed:
		model.HeaderStatus = "ready"
		model.InteractionMode = InteractionModeNAV
		model.WorkspaceSaveError = strings.TrimSpace(value.Message)
		return nil
	case WorkspaceSaveFailed:
		model.HeaderStatus = "ready"
		model.InteractionMode = InteractionModeNAV
		model.WorkspaceSaveError = strings.TrimSpace(value.Message)
		return nil
	case EndpointCopyNoted:
		model.WorkspaceCopyNote = strings.TrimSpace(value.Message)
		return nil
	default:
		return nil
	}
}

func reduceRoutingSaveState(model *Model, action update.Action) []update.Effect {
	switch value := action.(type) {
	case RoutingSaveStartedAction:
		model.HeaderStatus = "saving…"
		model.InteractionMode = InteractionModeBusySave
		model.RoutingSaveError = ""
		return nil
	case RoutingSaveSucceeded:
		model.HeaderStatus = "ready"
		model.InteractionMode = InteractionModeNAV
		model.RoutingSaveError = ""
		applyRoutingSelection(model, strings.TrimSpace(value.EndpointName), strings.TrimSpace(value.ProviderRef))
		return []update.Effect{RefreshEndpointsEffect{}, RefreshCatalogEffect{}}
	case RoutingMutationSaved:
		model.HeaderStatus = "ready"
		model.InteractionMode = InteractionModeNAV
		model.RoutingSaveError = ""
		return []update.Effect{RefreshEndpointsEffect{}, RefreshCatalogEffect{}}
	case RoutingSaveFailed:
		model.HeaderStatus = "ready"
		model.InteractionMode = InteractionModeNAV
		model.RoutingSaveError = strings.TrimSpace(value.Message)
		return nil
	case SaveSelectedTargetRequested:
		return []update.Effect{SaveSelectedTargetEffect(value)}
	case SaveProviderConfigRequested:
		return []update.Effect{SaveProviderConfigEffect(value)}
	case AddProviderConfigRequested:
		return []update.Effect{AddProviderConfigEffect(value)}
	case DeleteProviderConfigRequested:
		return []update.Effect{DeleteProviderConfigEffect(value)}
	case StoreKeychainCredentialRequested:
		model.HeaderStatus = "saving…"
		model.InteractionMode = InteractionModeBusySave
		model.RoutingSaveError = ""
		return []update.Effect{StoreKeychainCredentialEffect(value)}
	case KeychainCredentialStored:
		model.HeaderStatus = "saved"
		model.InteractionMode = InteractionModeNAV
		model.RoutingSaveError = ""
		model.LastStoredKeyProviderSpec = strings.TrimSpace(value.ProviderSpec)
		model.LastStoredKeySlotName = strings.TrimSpace(value.KeyName)
		return nil
	default:
		return nil
	}
}

func currentEndpoint(model *Model) string {
	if strings.TrimSpace(model.CurrentEndpoint) != "" {
		return model.CurrentEndpoint
	}
	if len(model.Endpoints) > 0 {
		return model.Endpoints[0]
	}
	return ""
}

func currentSelectedProviderConfig(model *Model) *ProviderConfigSnapshot {
	ep := currentEndpoint(model)
	if ep == "" {
		return nil
	}
	for _, snap := range model.EndpointSnapshots {
		if snap.Name == ep {
			if snap.SelectedProviderConfigRef == "" || len(snap.ProviderConfigs) == 0 {
				return nil
			}
			for _, pc := range snap.ProviderConfigs {
				if pc.Ref == snap.SelectedProviderConfigRef {
					return &pc
				}
			}
			return nil
		}
	}
	if model.CreateDraftProviderConfig.ProviderSpec != "" {
		return &model.CreateDraftProviderConfig
	}
	return nil
}

func reconcileCurrentEndpoint(current string, endpoints []string, hadEndpoints bool) string {
	trimmed := strings.TrimSpace(current)
	if trimmed == "" {
		if hadEndpoints {
			// Preserve explicit create-lane selection chosen in the rail.
			return ""
		}
		return firstOrEmpty(endpoints)
	}
	if containsString(endpoints, trimmed) {
		return trimmed
	}
	return firstOrEmpty(endpoints)
}
