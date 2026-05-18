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
		model.CurrentEndpoint = strings.TrimSpace(value.Name) // swobu:io-string source=boundary
		model.InteractionMode = InteractionModeNAV
		model.FooterShowTabs = strings.TrimSpace(model.CurrentEndpoint) != "" || len(model.Endpoints) > 0 // swobu:io-string source=boundary
		model.WorkspaceCopyNote = ""
		model.ClientCopyNote = ""
		model.ClientLaunchNote = ""
		model.ClientAccessNote = ""
		model.HelpTabOpen = false
		model.HelpNote = ""
		return true
	case CreateEndpoint:
		name := strings.TrimSpace(value.Name) // swobu:io-string source=boundary
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
		model.CreateDraftName = strings.TrimSpace(value.Name) // swobu:io-string source=boundary
		model.WorkspaceSaveError = ""
		refreshFirstRunFooterAffordance(model)
		return true
	case SetCreateDraftProviderSpec:
		model.CreateDraftProviderConfig = ProviderConfigForSpec(value.ProviderSpec, model.CreateDraftProviderConfig)
		if !strings.EqualFold(strings.TrimSpace(model.CreateDraftProviderConfig.ProviderSpec), "bedrock") { // swobu:io-string source=boundary
			model.CreateDraftProviderConfig.Region = ""
		}
		model.WorkspaceSaveError = ""
		model.CreateDraftModelIDs = nil
		model.CreateDraftModelError = ""
		refreshFirstRunFooterAffordance(model)
		return true
	case SetCreateDraftModelIDAction:
		model.CreateDraftProviderConfig.ModelID = strings.TrimSpace(value.ModelID) // swobu:io-string source=boundary
		model.WorkspaceSaveError = ""
		refreshFirstRunFooterAffordance(model)
		return true
	case SetCreateDraftCredentialRef:
		model.CreateDraftProviderConfig.CredentialRef = strings.TrimSpace(value.CredentialRef) // swobu:io-string source=boundary
		model.WorkspaceSaveError = ""
		model.CreateDraftModelIDs = nil
		model.CreateDraftModelError = ""
		refreshFirstRunFooterAffordance(model)
		return true
	case SetCreateDraftBaseURL:
		model.CreateDraftProviderConfig.BaseURL = strings.TrimSpace(value.BaseURL)                         // swobu:io-string source=boundary
		if strings.EqualFold(strings.TrimSpace(model.CreateDraftProviderConfig.ProviderSpec), "bedrock") { // swobu:io-string source=boundary
			model.CreateDraftProviderConfig.Region = stateModel.BedrockRegionFromBaseURL(model.CreateDraftProviderConfig.BaseURL)
		}
		model.WorkspaceSaveError = ""
		model.CreateDraftModelIDs = nil
		model.CreateDraftModelError = ""
		refreshFirstRunFooterAffordance(model)
		return true
	case SetCreateDraftTargetAlias:
		model.CreateDraftProviderConfig.TargetAlias = strings.TrimSpace(strings.ToLower(value.TargetAlias)) // swobu:io-string source=boundary
		model.WorkspaceSaveError = ""
		refreshFirstRunFooterAffordance(model)
		return true
	case SetCreateDraftSelectedFrame:
		model.CreateDraftProviderConfig.SelectedFrame = strings.TrimSpace(value.SelectedFrame) // swobu:io-string source=boundary
		model.WorkspaceSaveError = ""
		refreshFirstRunFooterAffordance(model)
		return true
	case RenameCurrentEndpoint:
		next := strings.TrimSpace(value.Name)               // swobu:io-string source=boundary
		current := strings.TrimSpace(model.CurrentEndpoint) // swobu:io-string source=boundary
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
		model.FooterShowTabs = strings.TrimSpace(model.CurrentEndpoint) != "" || len(model.Endpoints) > 0 // swobu:io-string source=boundary
		refreshFirstRunFooterAffordance(model)
		model.WorkspaceCopyNote = ""
		model.ClientCopyNote = ""
		model.ClientLaunchNote = ""
		model.ClientAccessNote = ""
		return true
	case stateeffect.EndpointsLoadFailed:
		if len(model.Endpoints) == 0 {
			model.DaemonHint = strings.TrimSpace(value.Message) // swobu:io-string source=boundary
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
		model.TrafficError = strings.TrimSpace(value.Message) // swobu:io-string source=boundary
		return nil
	case stateeffect.SupportLinkNoted:
		model.HelpNote = strings.TrimSpace(value.Message) // swobu:io-string source=boundary
		return nil
	case stateeffect.HelpDiagnosticsCopyNoted:
		model.HelpNote = strings.TrimSpace(value.Message) // swobu:io-string source=boundary
		return nil
	case stateeffect.ClientCopyNoted:
		model.ClientCopyNote = strings.TrimSpace(value.Message) // swobu:io-string source=boundary
		return nil
	case stateeffect.ClientLaunchNoted:
		model.HeaderStatus = "ready"
		model.InteractionMode = InteractionModeNAV
		model.ClientLaunchNote = strings.TrimSpace(value.Message) // swobu:io-string source=boundary
		return nil
	case ClientAccessCheckStarted:
		model.HeaderStatus = "checking..."
		model.InteractionMode = InteractionModeBusySave
		model.ClientAccessNote = ""
		ep := currentEndpoint(model)
		sp := currentSelectedProviderConfig(model)
		if strings.TrimSpace(ep) == "" || sp == nil { // swobu:io-string source=boundary
			return nil
		}
		return []update.Effect{stateeffect.CheckClientAccessEffect{
			EndpointName:   ep,
			ProviderConfig: *sp,
		}}
	case stateeffect.ClientAccessChecked:
		model.HeaderStatus = "ready"
		model.InteractionMode = InteractionModeNAV
		model.ClientAccessStatus = strings.TrimSpace(value.Status) // swobu:io-string source=boundary
		model.ClientAccessNote = strings.TrimSpace(value.Message)  // swobu:io-string source=boundary
		return []update.Effect{refreshStatusProjectionEffectFor(model)}
	case stateeffect.ClientAccessCheckFailed:
		model.HeaderStatus = "ready"
		model.InteractionMode = InteractionModeNAV
		model.ClientAccessStatus = "check failed"
		model.ClientAccessNote = strings.TrimSpace(value.Message) // swobu:io-string source=boundary
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
		clearSaveErrors(model)
		model.ClientAccessNote = ""
		return []update.Effect{stateeffect.SaveNewWorkspaceEffect{
			Name:           strings.TrimSpace(value.Name), // swobu:io-string source=boundary
			ProviderConfig: model.CreateDraftProviderConfig,
		}}
	case WorkspaceRenameRequested:
		model.HeaderStatus = "saving…"
		model.InteractionMode = InteractionModeBusySave
		model.WorkspaceSaveError = ""
		model.WorkspaceCopyNote = ""
		clearSaveErrors(model)
		model.ClientAccessNote = ""
		return []update.Effect{stateeffect.SaveWorkspaceNameEffect{
			CurrentName: strings.TrimSpace(value.CurrentName), // swobu:io-string source=boundary
			Name:        strings.TrimSpace(value.Name),        // swobu:io-string source=boundary
		}}
	case stateeffect.WorkspaceSaveSucceeded:
		model.HeaderStatus = "saved"
		model.InteractionMode = InteractionModeNAV
		model.FooterVerb = "edit"
		model.FooterAllowSpace = false
		model.FooterShowTabs = true
		model.WorkspaceSaveError = ""
		model.WorkspaceCopyNote = ""
		if strings.TrimSpace(value.PreviousName) == "" { // swobu:io-string source=boundary
			applyWorkspaceCreate(model, strings.TrimSpace(value.Name)) // swobu:io-string source=boundary
		} else {
			applyWorkspaceRename(model, strings.TrimSpace(value.PreviousName), strings.TrimSpace(value.Name)) // swobu:io-string source=boundary
		}
		model.CreateDraftName = ""
		model.CreateDraftProviderConfig = ProviderConfigSnapshot{}
		return []update.Effect{
			stateeffect.RefreshEndpointsEffect{},
			stateeffect.ScheduleDaemonRefreshEffect{Delay: 350 * time.Millisecond},
		}
	case WorkspaceDeleteRequested:
		name := strings.TrimSpace(value.Name) // swobu:io-string source=boundary
		if name == "" {
			return nil
		}
		model.HeaderStatus = "saving…"
		model.InteractionMode = InteractionModeBusySave
		model.WorkspaceSaveError = ""
		model.WorkspaceCopyNote = ""
		clearSaveErrors(model)
		model.ClientAccessNote = ""
		return []update.Effect{stateeffect.DeleteWorkspaceEffect{Name: name}}
	case stateeffect.WorkspaceDeleteSucceeded:
		model.HeaderStatus = "saved"
		model.InteractionMode = InteractionModeNAV
		model.WorkspaceSaveError = ""
		model.WorkspaceCopyNote = ""
		applyWorkspaceDelete(model, strings.TrimSpace(value.Name)) // swobu:io-string source=boundary
		return []update.Effect{
			stateeffect.RefreshEndpointsEffect{},
			stateeffect.ScheduleDaemonRefreshEffect{Delay: 350 * time.Millisecond},
		}
	case stateeffect.WorkspaceDeleteFailed:
		model.HeaderStatus = "ready"
		model.InteractionMode = InteractionModeNAV
		model.WorkspaceSaveError = strings.TrimSpace(value.Message) // swobu:io-string source=boundary
		return nil
	case stateeffect.WorkspaceSaveFailed:
		model.HeaderStatus = "ready"
		model.InteractionMode = InteractionModeNAV
		model.WorkspaceSaveError = strings.TrimSpace(value.Message) // swobu:io-string source=boundary
		return nil
	case stateeffect.EndpointCopyNoted:
		model.WorkspaceCopyNote = strings.TrimSpace(value.Message) // swobu:io-string source=boundary
		return nil
	default:
		return nil
	}
}

func clearAuthSessions(model *Model) {
	model.AuthSessions = nil
}

func clearAuthSession(model *Model, ownerKey string) {
	if model == nil || model.AuthSessions == nil {
		return
	}
	key := strings.TrimSpace(ownerKey) // swobu:io-string source=boundary
	if key == "" {
		return
	}
	delete(model.AuthSessions, key)
	if len(model.AuthSessions) == 0 {
		model.AuthSessions = nil
	}
}

func clearAuthSessionsByPrefix(model *Model, prefix stateModel.AuthOwnerKey) {
	if model == nil || model.AuthSessions == nil {
		return
	}
	targetPrefix := prefix.String()
	for key := range model.AuthSessions {
		if stateModel.AuthOwnerKey(key).Prefix() == targetPrefix {
			delete(model.AuthSessions, key)
		}
	}
	if len(model.AuthSessions) == 0 {
		model.AuthSessions = nil
	}
}

func setAuthSession(model *Model, ownerKey string, session stateModel.AuthSessionViewState) {
	if model == nil {
		return
	}
	key := strings.TrimSpace(ownerKey) // swobu:io-string source=boundary
	if key == "" {
		return
	}
	if model.AuthSessions == nil {
		model.AuthSessions = map[string]stateModel.AuthSessionViewState{}
	}
	model.AuthSessions[key] = session
}

func authSession(model *Model, ownerKey string) (stateModel.AuthSessionViewState, bool) {
	if model == nil || model.AuthSessions == nil {
		return stateModel.AuthSessionViewState{}, false
	}
	session, ok := model.AuthSessions[strings.TrimSpace(ownerKey)] // swobu:io-string source=boundary
	return session, ok
}

func clearSaveErrors(model *Model) {
	model.SaveErrors = nil
}

func setSaveError(model *Model, anchor, message string) {
	model.SaveErrors = nil
	anchor = strings.TrimSpace(anchor)   // swobu:io-string source=boundary
	message = strings.TrimSpace(message) // swobu:io-string source=boundary
	if anchor == "" || message == "" {
		return
	}
	model.SaveErrors = map[string]string{anchor: message}
}

func currentEndpoint(model *Model) string {
	if strings.TrimSpace(model.CurrentEndpoint) != "" { // swobu:io-string source=boundary
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
