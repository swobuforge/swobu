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
		model.CurrentEndpoint = strings.TrimSpace(value.Name) // trimlowerlint:allow boundary canonicalization
		model.InteractionMode = InteractionModeNAV
		model.FooterShowTabs = strings.TrimSpace(model.CurrentEndpoint) != "" || len(model.Endpoints) > 0 // trimlowerlint:allow boundary canonicalization
		model.WorkspaceCopyNote = ""
		model.ClientCopyNote = ""
		model.ClientLaunchNote = ""
		model.ClientAccessNote = ""
		model.HelpTabOpen = false
		model.HelpNote = ""
		return true
	case CreateEndpoint:
		name := strings.TrimSpace(value.Name) // trimlowerlint:allow boundary canonicalization
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
		model.CreateDraftName = strings.TrimSpace(value.Name) // trimlowerlint:allow boundary canonicalization
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
		model.CreateDraftProviderConfig.ModelID = strings.TrimSpace(value.ModelID) // trimlowerlint:allow boundary canonicalization
		model.WorkspaceSaveError = ""
		refreshFirstRunFooterAffordance(model)
		return true
	case SetCreateDraftCredentialRef:
		model.CreateDraftProviderConfig.CredentialRef = strings.TrimSpace(value.CredentialRef) // trimlowerlint:allow boundary canonicalization
		model.WorkspaceSaveError = ""
		model.CreateDraftModelIDs = nil
		model.CreateDraftModelError = ""
		refreshFirstRunFooterAffordance(model)
		return true
	case SetCreateDraftBaseURL:
		model.CreateDraftProviderConfig.BaseURL = strings.TrimSpace(value.BaseURL) // trimlowerlint:allow boundary canonicalization
		model.WorkspaceSaveError = ""
		model.CreateDraftModelIDs = nil
		model.CreateDraftModelError = ""
		refreshFirstRunFooterAffordance(model)
		return true
	case SetCreateDraftTargetAlias:
		model.CreateDraftProviderConfig.TargetAlias = strings.TrimSpace(strings.ToLower(value.TargetAlias)) // trimlowerlint:allow boundary canonicalization
		model.WorkspaceSaveError = ""
		refreshFirstRunFooterAffordance(model)
		return true
	case SetCreateDraftSelectedFrame:
		model.CreateDraftProviderConfig.SelectedFrame = strings.TrimSpace(value.SelectedFrame) // trimlowerlint:allow boundary canonicalization
		model.WorkspaceSaveError = ""
		refreshFirstRunFooterAffordance(model)
		return true
	case RenameCurrentEndpoint:
		next := strings.TrimSpace(value.Name)               // trimlowerlint:allow boundary canonicalization
		current := strings.TrimSpace(model.CurrentEndpoint) // trimlowerlint:allow boundary canonicalization
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
		model.FooterShowTabs = strings.TrimSpace(model.CurrentEndpoint) != "" || len(model.Endpoints) > 0 // trimlowerlint:allow boundary canonicalization
		refreshFirstRunFooterAffordance(model)
		model.WorkspaceCopyNote = ""
		model.ClientCopyNote = ""
		model.ClientLaunchNote = ""
		model.ClientAccessNote = ""
		return true
	case stateeffect.EndpointsLoadFailed:
		if len(model.Endpoints) == 0 {
			model.DaemonHint = strings.TrimSpace(value.Message) // trimlowerlint:allow boundary canonicalization
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
		model.TrafficError = strings.TrimSpace(value.Message) // trimlowerlint:allow boundary canonicalization
		return nil
	case stateeffect.SupportLinkNoted:
		model.HelpNote = strings.TrimSpace(value.Message) // trimlowerlint:allow boundary canonicalization
		return nil
	case stateeffect.HelpDiagnosticsCopyNoted:
		model.HelpNote = strings.TrimSpace(value.Message) // trimlowerlint:allow boundary canonicalization
		return nil
	case stateeffect.ClientCopyNoted:
		model.ClientCopyNote = strings.TrimSpace(value.Message) // trimlowerlint:allow boundary canonicalization
		return nil
	case stateeffect.ClientLaunchNoted:
		model.HeaderStatus = "ready"
		model.InteractionMode = InteractionModeNAV
		model.ClientLaunchNote = strings.TrimSpace(value.Message) // trimlowerlint:allow boundary canonicalization
		return nil
	case ClientAccessCheckStarted:
		model.HeaderStatus = "checking..."
		model.InteractionMode = InteractionModeBusySave
		model.ClientAccessNote = ""
		ep := currentEndpoint(model)
		sp := currentSelectedProviderConfig(model)
		if strings.TrimSpace(ep) == "" || sp == nil { // trimlowerlint:allow boundary canonicalization
			return nil
		}
		return []update.Effect{stateeffect.CheckClientAccessEffect{
			EndpointName:   ep,
			ProviderConfig: *sp,
		}}
	case stateeffect.ClientAccessChecked:
		model.HeaderStatus = "ready"
		model.InteractionMode = InteractionModeNAV
		model.ClientAccessStatus = strings.TrimSpace(value.Status) // trimlowerlint:allow boundary canonicalization
		model.ClientAccessNote = strings.TrimSpace(value.Message)  // trimlowerlint:allow boundary canonicalization
		return []update.Effect{refreshStatusProjectionEffectFor(model)}
	case stateeffect.ClientAccessCheckFailed:
		model.HeaderStatus = "ready"
		model.InteractionMode = InteractionModeNAV
		model.ClientAccessStatus = "check failed"
		model.ClientAccessNote = strings.TrimSpace(value.Message) // trimlowerlint:allow boundary canonicalization
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
			Name:           strings.TrimSpace(value.Name), // trimlowerlint:allow boundary canonicalization
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
			CurrentName: strings.TrimSpace(value.CurrentName), // trimlowerlint:allow boundary canonicalization
			Name:        strings.TrimSpace(value.Name),        // trimlowerlint:allow boundary canonicalization
		}}
	case stateeffect.WorkspaceSaveSucceeded:
		model.HeaderStatus = "saved"
		model.InteractionMode = InteractionModeNAV
		model.FooterVerb = "edit"
		model.FooterAllowSpace = false
		model.FooterShowTabs = true
		model.WorkspaceSaveError = ""
		model.WorkspaceCopyNote = ""
		if strings.TrimSpace(value.PreviousName) == "" { // trimlowerlint:allow boundary canonicalization
			applyWorkspaceCreate(model, strings.TrimSpace(value.Name)) // trimlowerlint:allow boundary canonicalization
		} else {
			applyWorkspaceRename(model, strings.TrimSpace(value.PreviousName), strings.TrimSpace(value.Name)) // trimlowerlint:allow boundary canonicalization
		}
		model.CreateDraftName = ""
		model.CreateDraftProviderConfig = ProviderConfigSnapshot{}
		return []update.Effect{
			stateeffect.RefreshEndpointsEffect{},
			stateeffect.ScheduleDaemonRefreshEffect{Delay: 350 * time.Millisecond},
		}
	case WorkspaceDeleteRequested:
		name := strings.TrimSpace(value.Name) // trimlowerlint:allow boundary canonicalization
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
		applyWorkspaceDelete(model, strings.TrimSpace(value.Name)) // trimlowerlint:allow boundary canonicalization
		return []update.Effect{
			stateeffect.RefreshEndpointsEffect{},
			stateeffect.ScheduleDaemonRefreshEffect{Delay: 350 * time.Millisecond},
		}
	case stateeffect.WorkspaceDeleteFailed:
		model.HeaderStatus = "ready"
		model.InteractionMode = InteractionModeNAV
		model.WorkspaceSaveError = strings.TrimSpace(value.Message) // trimlowerlint:allow boundary canonicalization
		return nil
	case stateeffect.WorkspaceSaveFailed:
		model.HeaderStatus = "ready"
		model.InteractionMode = InteractionModeNAV
		model.WorkspaceSaveError = strings.TrimSpace(value.Message) // trimlowerlint:allow boundary canonicalization
		return nil
	case stateeffect.EndpointCopyNoted:
		model.WorkspaceCopyNote = strings.TrimSpace(value.Message) // trimlowerlint:allow boundary canonicalization
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
		clearSaveErrors(model)
		return nil
	case stateeffect.RoutingSaveSucceeded:
		model.HeaderStatus = "ready"
		model.InteractionMode = InteractionModeNAV
		clearSaveErrors(model)
		applyRoutingSelection(model, strings.TrimSpace(value.EndpointName), strings.TrimSpace(value.ProviderRef)) // trimlowerlint:allow boundary canonicalization
		return []update.Effect{stateeffect.RefreshEndpointsEffect{}}
	case stateeffect.RoutingMutationSaved:
		model.HeaderStatus = "ready"
		model.InteractionMode = InteractionModeNAV
		clearSaveErrors(model)
		return []update.Effect{stateeffect.RefreshEndpointsEffect{}}
	case stateeffect.ProviderConfigAddedSaved:
		model.HeaderStatus = "ready"
		model.InteractionMode = InteractionModeNAV
		clearSaveErrors(model)
		return []update.Effect{stateeffect.RefreshEndpointsEffect{}}
	case stateeffect.RoutingSaveFailed:
		model.HeaderStatus = "ready"
		model.InteractionMode = InteractionModeNAV
		setSaveError(model, strings.TrimSpace(value.ErrorAnchor), strings.TrimSpace(value.Message)) // trimlowerlint:allow boundary canonicalization
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
		clearSaveErrors(model)
		return []update.Effect{stateeffect.StoreKeychainCredentialEffect(value)}
	case StartProviderAuthSessionRequested:
		model.HeaderStatus = "waiting for login…"
		model.InteractionMode = InteractionModeBusySave
		clearSaveErrors(model)
		clearAuthSession(model, strings.TrimSpace(value.OwnerKey)) // trimlowerlint:allow boundary canonicalization
		return []update.Effect{stateeffect.StartProviderAuthSessionEffect{
			EndpointName:   strings.TrimSpace(value.EndpointName), // trimlowerlint:allow boundary canonicalization
			ProviderConfig: value.ProviderConfig,
			OwnerKey:       strings.TrimSpace(value.OwnerKey),  // trimlowerlint:allow boundary canonicalization
			AuthScope:      strings.TrimSpace(value.AuthScope), // trimlowerlint:allow boundary canonicalization
		}}
	case ResetAuthSessionUIRequested:
		clearAuthSessions(model)
		return nil
	case ResetAddModelAuthUIRequested:
		clearAuthSessionsByPrefix(model, stateModel.AuthOwnerPrefixAddModelDraft)
		return nil
	case stateeffect.ProviderAuthSessionStarted:
		model.HeaderStatus = "waiting for login…"
		// Pending auth must keep auth rows actionable (open/copy/switch),
		// so keep operator in manage mode instead of busy-save lockout.
		model.InteractionMode = InteractionModeManageList
		setAuthSession(model, strings.TrimSpace(value.OwnerKey), stateModel.AuthSessionView{ // trimlowerlint:allow boundary canonicalization
			SessionID:    strings.TrimSpace(value.SessionID),    // trimlowerlint:allow boundary canonicalization
			URL:          strings.TrimSpace(value.AuthorizeURL), // trimlowerlint:allow boundary canonicalization
			UserCode:     strings.TrimSpace(value.UserCode),     // trimlowerlint:allow boundary canonicalization
			SessionState: strings.TrimSpace(value.State),        // trimlowerlint:allow boundary canonicalization
			SessionError: "",
			CopyNote:     "",
		})
		loginURL := strings.TrimSpace(value.AuthorizeURL) // trimlowerlint:allow boundary canonicalization
		if loginURL != "" {
			return []update.Effect{stateeffect.OpenSupportLinkEffect{
				Label: "login",
				URL:   loginURL,
			}}
		}
		return nil
	case stateeffect.ProviderAuthSessionFailed:
		model.HeaderStatus = "ready"
		model.InteractionMode = InteractionModeManageList
		clearSaveErrors(model)
		ownerKey := strings.TrimSpace(value.OwnerKey) // trimlowerlint:allow boundary canonicalization
		previous, hadPrevious := authSession(model, ownerKey)
		// Keep any previously issued login URL/session visible so operators can
		// retry in another browser or copy the link when auto-open fails.
		url := ""
		userCode := ""
		sessionID := ""
		if hadPrevious {
			url = strings.TrimSpace(previous.URL)           // trimlowerlint:allow boundary canonicalization
			userCode = strings.TrimSpace(previous.UserCode) // trimlowerlint:allow boundary canonicalization
			sessionID = strings.TrimSpace(previous.SessionID)
		}
		setAuthSession(model, strings.TrimSpace(value.OwnerKey), stateModel.AuthSessionView{ // trimlowerlint:allow boundary canonicalization
			SessionID:    sessionID,
			URL:          url,
			UserCode:     userCode,
			SessionState: "failed",
			SessionError: strings.TrimSpace(value.Message), // trimlowerlint:allow boundary canonicalization
			CopyNote:     "",
		})
		return nil
	case stateeffect.ProviderAuthSessionPolled:
		ownerKey := strings.TrimSpace(value.OwnerKey) // trimlowerlint:allow boundary canonicalization
		session, ok := authSession(model, ownerKey)
		if ok && strings.TrimSpace(session.SessionID) == strings.TrimSpace(value.SessionID) { // trimlowerlint:allow boundary canonicalization
			session.SessionState = strings.TrimSpace(value.State)        // trimlowerlint:allow boundary canonicalization
			session.SessionError = strings.TrimSpace(value.ErrorMessage) // trimlowerlint:allow boundary canonicalization
			setAuthSession(model, ownerKey, session)
		}
		return nil
	case stateeffect.AuthSessionCopyNoted:
		ownerKey := strings.TrimSpace(value.OwnerKey) // trimlowerlint:allow boundary canonicalization
		session, _ := authSession(model, ownerKey)
		session.CopyNote = strings.TrimSpace(value.Message) // trimlowerlint:allow boundary canonicalization
		setAuthSession(model, ownerKey, session)
		return nil
	case stateeffect.PollProviderAuthSessionRequested:
		return []update.Effect{stateeffect.PollProviderAuthSessionEffect{
			EndpointName:   strings.TrimSpace(value.EndpointName), // trimlowerlint:allow boundary canonicalization
			ProviderConfig: value.ProviderConfig,
			OwnerKey:       strings.TrimSpace(value.OwnerKey),  // trimlowerlint:allow boundary canonicalization
			AuthScope:      strings.TrimSpace(value.AuthScope), // trimlowerlint:allow boundary canonicalization
			SessionID:      strings.TrimSpace(value.SessionID), // trimlowerlint:allow boundary canonicalization
			AttemptsLeft:   value.AttemptsLeft,
		}}
	case stateeffect.ProviderAuthSessionCredentialResolved:
		if strings.TrimSpace(value.AuthScope) == stateModel.AuthScopeCreateDraft { // trimlowerlint:allow boundary canonicalization
			model.HeaderStatus = "login complete"
			model.InteractionMode = InteractionModeManageList
			clearSaveErrors(model)
			model.CreateDraftProviderConfig.ProviderSpec = strings.TrimSpace(value.ProviderConfig.ProviderSpec) // trimlowerlint:allow boundary canonicalization
			model.CreateDraftProviderConfig.BaseURL = strings.TrimSpace(value.ProviderConfig.BaseURL)           // trimlowerlint:allow boundary canonicalization
			model.CreateDraftProviderConfig.CredentialRef = strings.TrimSpace(value.CredentialRef)              // trimlowerlint:allow boundary canonicalization
			clearAuthSession(model, strings.TrimSpace(value.OwnerKey))                                          // trimlowerlint:allow boundary canonicalization
			return nil
		}
		if stateModel.AuthOwnerKey(strings.TrimSpace(value.OwnerKey)).IsAddModelDraft() { // trimlowerlint:allow boundary canonicalization
			model.HeaderStatus = "login complete"
			model.InteractionMode = InteractionModeManageList
			clearSaveErrors(model)
			model.AddModelDraftProviderSpec = strings.TrimSpace(value.ProviderConfig.ProviderSpec) // trimlowerlint:allow boundary canonicalization
			model.AddModelDraftBaseURL = strings.TrimSpace(value.ProviderConfig.BaseURL)           // trimlowerlint:allow boundary canonicalization
			model.AddModelDraftCredentialRef = strings.TrimSpace(value.CredentialRef)              // trimlowerlint:allow boundary canonicalization
			clearAuthSession(model, strings.TrimSpace(value.OwnerKey))                             // trimlowerlint:allow boundary canonicalization
			return nil
		}
		next := value.ProviderConfig
		next.CredentialRef = strings.TrimSpace(value.CredentialRef) // trimlowerlint:allow boundary canonicalization
		model.HeaderStatus = "saving…"
		model.InteractionMode = InteractionModeBusySave
		clearSaveErrors(model)
		clearAuthSession(model, strings.TrimSpace(value.OwnerKey)) // trimlowerlint:allow boundary canonicalization
		return []update.Effect{stateeffect.SaveProviderConfigEffect(SaveProviderConfigRequested{
			EndpointName:   strings.TrimSpace(value.EndpointName), // trimlowerlint:allow boundary canonicalization
			ProviderConfig: next,
			ErrorAnchor:    "",
		})}
	case stateeffect.KeychainCredentialStored:
		model.HeaderStatus = "saved"
		model.InteractionMode = InteractionModeNAV
		clearSaveErrors(model)
		model.LastStoredKeyProviderSpec = strings.TrimSpace(value.ProviderSpec) // trimlowerlint:allow boundary canonicalization
		model.LastStoredKeySlotName = strings.TrimSpace(value.KeyName)          // trimlowerlint:allow boundary canonicalization
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
	key := strings.TrimSpace(ownerKey) // trimlowerlint:allow boundary canonicalization
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

func setAuthSession(model *Model, ownerKey string, session stateModel.AuthSessionView) {
	if model == nil {
		return
	}
	key := strings.TrimSpace(ownerKey) // trimlowerlint:allow boundary canonicalization
	if key == "" {
		return
	}
	if model.AuthSessions == nil {
		model.AuthSessions = map[string]stateModel.AuthSessionView{}
	}
	model.AuthSessions[key] = session
}

func authSession(model *Model, ownerKey string) (stateModel.AuthSessionView, bool) {
	if model == nil || model.AuthSessions == nil {
		return stateModel.AuthSessionView{}, false
	}
	session, ok := model.AuthSessions[strings.TrimSpace(ownerKey)] // trimlowerlint:allow boundary canonicalization
	return session, ok
}

func clearSaveErrors(model *Model) {
	model.SaveErrors = nil
}

func setSaveError(model *Model, anchor, message string) {
	model.SaveErrors = nil
	anchor = strings.TrimSpace(anchor)   // trimlowerlint:allow boundary canonicalization
	message = strings.TrimSpace(message) // trimlowerlint:allow boundary canonicalization
	if anchor == "" || message == "" {
		return
	}
	model.SaveErrors = map[string]string{anchor: message}
}

func currentEndpoint(model *Model) string {
	if strings.TrimSpace(model.CurrentEndpoint) != "" { // trimlowerlint:allow boundary canonicalization
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
	trimmed := strings.TrimSpace(current) // trimlowerlint:allow boundary canonicalization
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
