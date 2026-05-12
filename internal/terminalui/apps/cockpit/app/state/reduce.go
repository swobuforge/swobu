package state

import (
	"slices"
	"strings"
	"time"

	stateeffect "github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state/effect"
	stateModel "github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state/model"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
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
	case stateeffect.ReplaceEndpoints:
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
	case stateeffect.EndpointsLoadFailed:
		if len(model.Endpoints) == 0 {
			model.DaemonHint = strings.TrimSpace(value.Message)
		}
		return true
	default:
		return false
	}
}

func reduceClientAndTrafficState(model *Model, action update.Action) []update.Effect {
	switch value := action.(type) {
	case stateeffect.ReplaceStatusProjection:
		model.TrafficRows = cloneTrafficRows(value.Rows)
		model.TrafficError = ""
		return nil
	case stateeffect.TrafficLoadFailed:
		model.TrafficRows = nil
		model.TrafficError = strings.TrimSpace(value.Message)
		return nil
	case stateeffect.SupportLinkNoted:
		model.HelpNote = strings.TrimSpace(value.Message)
		return nil
	case stateeffect.HelpDiagnosticsCopyNoted:
		model.HelpNote = strings.TrimSpace(value.Message)
		return nil
	case stateeffect.ClientCopyNoted:
		model.ClientCopyNote = strings.TrimSpace(value.Message)
		return nil
	case stateeffect.ClientLaunchNoted:
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
		return []update.Effect{stateeffect.CheckClientAccessEffect{
			EndpointName:   ep,
			ProviderConfig: *sp,
		}}
	case stateeffect.ClientAccessChecked:
		model.HeaderStatus = "ready"
		model.InteractionMode = InteractionModeNAV
		model.ClientAccessStatus = strings.TrimSpace(value.Status)
		model.ClientAccessNote = strings.TrimSpace(value.Message)
		return []update.Effect{refreshStatusProjectionEffectFor(model)}
	case stateeffect.ClientAccessCheckFailed:
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
		return []update.Effect{stateeffect.SaveNewWorkspaceEffect{
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
		return []update.Effect{stateeffect.SaveWorkspaceNameEffect{
			CurrentName: strings.TrimSpace(value.CurrentName),
			Name:        strings.TrimSpace(value.Name),
		}}
	case stateeffect.WorkspaceSaveSucceeded:
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
			stateeffect.RefreshEndpointsEffect{},
			stateeffect.ScheduleDaemonRefreshEffect{Delay: 350 * time.Millisecond},
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
		return []update.Effect{stateeffect.DeleteWorkspaceEffect{Name: name}}
	case stateeffect.WorkspaceDeleteSucceeded:
		model.HeaderStatus = "saved"
		model.InteractionMode = InteractionModeNAV
		model.WorkspaceSaveError = ""
		model.WorkspaceCopyNote = ""
		applyWorkspaceDelete(model, strings.TrimSpace(value.Name))
		return []update.Effect{
			stateeffect.RefreshEndpointsEffect{},
			stateeffect.ScheduleDaemonRefreshEffect{Delay: 350 * time.Millisecond},
		}
	case stateeffect.WorkspaceDeleteFailed:
		model.HeaderStatus = "ready"
		model.InteractionMode = InteractionModeNAV
		model.WorkspaceSaveError = strings.TrimSpace(value.Message)
		return nil
	case stateeffect.WorkspaceSaveFailed:
		model.HeaderStatus = "ready"
		model.InteractionMode = InteractionModeNAV
		model.WorkspaceSaveError = strings.TrimSpace(value.Message)
		return nil
	case stateeffect.EndpointCopyNoted:
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
	case stateeffect.RoutingSaveSucceeded:
		model.HeaderStatus = "ready"
		model.InteractionMode = InteractionModeNAV
		model.RoutingSaveError = ""
		applyRoutingSelection(model, strings.TrimSpace(value.EndpointName), strings.TrimSpace(value.ProviderRef))
		return []update.Effect{stateeffect.RefreshEndpointsEffect{}}
	case stateeffect.RoutingMutationSaved:
		model.HeaderStatus = "ready"
		model.InteractionMode = InteractionModeNAV
		model.RoutingSaveError = ""
		clearAuthLoginState(model)
		return []update.Effect{stateeffect.RefreshEndpointsEffect{}}
	case stateeffect.ProviderConfigAddedSaved:
		model.HeaderStatus = "ready"
		model.InteractionMode = InteractionModeNAV
		model.RoutingSaveError = ""
		clearAuthLoginState(model)
		return []update.Effect{stateeffect.RefreshEndpointsEffect{}}
	case stateeffect.RoutingSaveFailed:
		model.HeaderStatus = "ready"
		model.InteractionMode = InteractionModeNAV
		model.RoutingSaveError = strings.TrimSpace(value.Message)
		if model.AuthLoginSessionID != "" && model.AuthLoginURL == "" {
			// Preserve interactive login row only when we still have a URL to open.
			clearAuthLoginState(model)
		}
		return nil
	case SaveSelectedTargetRequested:
		return []update.Effect{stateeffect.SaveSelectedTargetEffect(value)}
	case SaveProviderConfigRequested:
		return []update.Effect{stateeffect.SaveProviderConfigEffect(value)}
	case AddProviderConfigRequested:
		return []update.Effect{stateeffect.AddProviderConfigEffect(value)}
	case DeleteProviderConfigRequested:
		return []update.Effect{stateeffect.DeleteProviderConfigEffect(value)}
	case StoreKeychainCredentialRequested:
		model.HeaderStatus = "saving…"
		model.InteractionMode = InteractionModeBusySave
		model.RoutingSaveError = ""
		return []update.Effect{stateeffect.StoreKeychainCredentialEffect(value)}
	case StartProviderAuthSessionRequested:
		model.HeaderStatus = "waiting for login…"
		model.InteractionMode = InteractionModeBusySave
		model.RoutingSaveError = ""
		clearAuthLoginState(model)
		return []update.Effect{stateeffect.StartProviderAuthSessionEffect{
			EndpointName:   strings.TrimSpace(value.EndpointName),
			ProviderConfig: value.ProviderConfig,
			AuthSubject:    strings.TrimSpace(value.AuthSubject),
			AuthScope:      strings.TrimSpace(value.AuthScope),
		}}
	case ResetAuthLoginUIRequested:
		clearAuthLoginState(model)
		return nil
	case stateeffect.ProviderAuthSessionStarted:
		model.HeaderStatus = "waiting for login…"
		// Pending auth must keep auth rows actionable (open/copy/switch),
		// so keep operator in manage mode instead of busy-save lockout.
		model.InteractionMode = InteractionModeManageList
		model.AuthLoginEndpointName = strings.TrimSpace(value.EndpointName)
		model.AuthLoginProviderRef = strings.TrimSpace(value.ProviderConfig.Ref)
		model.AuthLoginSessionID = strings.TrimSpace(value.SessionID)
		model.AuthLoginURL = strings.TrimSpace(value.AuthorizeURL)
		model.AuthLoginUserCode = strings.TrimSpace(value.UserCode)
		model.AuthLoginSessionState = strings.TrimSpace(value.State)
		model.AuthLoginSessionError = ""
		model.AuthLoginCopyNote = ""
		if model.AuthLoginURL != "" {
			return []update.Effect{stateeffect.OpenSupportLinkEffect{
				Label: "login",
				URL:   model.AuthLoginURL,
			}}
		}
		return nil
	case stateeffect.ProviderAuthSessionFailed:
		model.HeaderStatus = "ready"
		model.InteractionMode = InteractionModeManageList
		model.RoutingSaveError = ""
		model.AuthLoginEndpointName = strings.TrimSpace(value.EndpointName)
		model.AuthLoginProviderRef = strings.TrimSpace(value.ProviderConfig.Ref)
		model.AuthLoginSessionID = ""
		model.AuthLoginURL = ""
		model.AuthLoginUserCode = ""
		model.AuthLoginSessionState = "failed"
		model.AuthLoginSessionError = strings.TrimSpace(value.Message)
		model.AuthLoginCopyNote = ""
		return nil
	case stateeffect.ProviderAuthSessionPolled:
		if strings.TrimSpace(model.AuthLoginSessionID) == strings.TrimSpace(value.SessionID) {
			model.AuthLoginSessionState = strings.TrimSpace(value.State)
			model.AuthLoginSessionError = strings.TrimSpace(value.ErrorMessage)
		}
		return nil
	case stateeffect.AuthLoginCopyNoted:
		model.AuthLoginCopyNote = strings.TrimSpace(value.Message)
		return nil
	case stateeffect.PollProviderAuthSessionRequested:
		return []update.Effect{stateeffect.PollProviderAuthSessionEffect{
			EndpointName:   strings.TrimSpace(value.EndpointName),
			ProviderConfig: value.ProviderConfig,
			AuthSubject:    strings.TrimSpace(value.AuthSubject),
			AuthScope:      strings.TrimSpace(value.AuthScope),
			SessionID:      strings.TrimSpace(value.SessionID),
			AttemptsLeft:   value.AttemptsLeft,
		}}
	case stateeffect.ProviderAuthSessionCredentialResolved:
		if strings.TrimSpace(value.AuthScope) == stateModel.AuthScopeCreateDraft {
			model.HeaderStatus = "login complete"
			model.InteractionMode = InteractionModeManageList
			model.RoutingSaveError = ""
			model.CreateDraftProviderConfig.ProviderSpec = strings.TrimSpace(value.ProviderConfig.ProviderSpec)
			model.CreateDraftProviderConfig.BaseURL = strings.TrimSpace(value.ProviderConfig.BaseURL)
			model.CreateDraftProviderConfig.CredentialRef = strings.TrimSpace(value.CredentialRef)
			clearAuthLoginState(model)
			return nil
		}
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(value.AuthSubject)), "subject:") {
			model.HeaderStatus = "login complete"
			model.InteractionMode = InteractionModeManageList
			model.RoutingSaveError = ""
			model.AddModelDraftProviderSpec = strings.TrimSpace(value.ProviderConfig.ProviderSpec)
			model.AddModelDraftBaseURL = strings.TrimSpace(value.ProviderConfig.BaseURL)
			model.AddModelDraftCredentialRef = strings.TrimSpace(value.CredentialRef)
			clearAuthLoginState(model)
			return nil
		}
		next := value.ProviderConfig
		next.CredentialRef = strings.TrimSpace(value.CredentialRef)
		model.HeaderStatus = "saving…"
		model.InteractionMode = InteractionModeBusySave
		model.RoutingSaveError = ""
		clearAuthLoginState(model)
		return []update.Effect{stateeffect.SaveProviderConfigEffect(SaveProviderConfigRequested{
			EndpointName:   strings.TrimSpace(value.EndpointName),
			ProviderConfig: next,
		})}
	case stateeffect.KeychainCredentialStored:
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

func clearAuthLoginState(model *Model) {
	model.AuthLoginEndpointName = ""
	model.AuthLoginProviderRef = ""
	model.AuthLoginSessionID = ""
	model.AuthLoginURL = ""
	model.AuthLoginUserCode = ""
	model.AuthLoginSessionState = ""
	model.AuthLoginSessionError = ""
	model.AuthLoginCopyNote = ""
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
