package state

import (
	"strings"

	stateeffect "github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state/effect"
	stateModel "github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state/model"
)

func reduceRoutingSaveState(model *Model, action Action) []EffectOnce {
	if effects, handled := reduceRoutingSaveMutationState(model, action); handled {
		return effects
	}
	if effects, handled := reduceRoutingAuthSessionState(model, action); handled {
		return effects
	}
	if effects, handled := reduceRoutingCredentialState(model, action); handled {
		return effects
	}
	return nil
}

func reduceRoutingSaveMutationState(model *Model, action Action) ([]EffectOnce, bool) {
	switch value := action.(type) {
	case RoutingSaveStartedAction:
		model.HeaderStatus = "saving…"
		model.InteractionMode = InteractionModeBusySave
		clearSaveErrors(model)
		return nil, true
	case stateeffect.RoutingSaveSucceeded:
		model.HeaderStatus = "ready"
		model.InteractionMode = InteractionModeNAV
		clearSaveErrors(model)
		applyRoutingSelection(model, strings.TrimSpace(value.EndpointName), strings.TrimSpace(value.ProviderRef)) // swobu:io-string source=boundary
		return []EffectOnce{stateeffect.RefreshEndpointsEffect{}}, true
	case stateeffect.RoutingMutationSaved, stateeffect.ProviderConfigAddedSaved:
		model.HeaderStatus = "ready"
		model.InteractionMode = InteractionModeNAV
		clearSaveErrors(model)
		return []EffectOnce{stateeffect.RefreshEndpointsEffect{}}, true
	case stateeffect.RoutingSaveFailed:
		model.HeaderStatus = "ready"
		model.InteractionMode = InteractionModeNAV
		setSaveError(model, strings.TrimSpace(value.ErrorAnchor), strings.TrimSpace(value.Message)) // swobu:io-string source=boundary
		return nil, true
	case SaveSelectedTargetRequested:
		return []EffectOnce{stateeffect.SaveSelectedTargetEffect(value)}, true
	case SaveProviderConfigRequested:
		return []EffectOnce{stateeffect.SaveProviderConfigEffect(value)}, true
	case AddProviderConfigRequested:
		return []EffectOnce{stateeffect.AddProviderConfigEffect(value)}, true
	case DeleteProviderConfigRequested:
		return []EffectOnce{stateeffect.DeleteProviderConfigEffect(value)}, true
	case StoreKeychainCredentialRequested:
		model.HeaderStatus = "saving…"
		model.InteractionMode = InteractionModeBusySave
		clearSaveErrors(model)
		return []EffectOnce{stateeffect.StoreKeychainCredentialEffect(value)}, true
	default:
		return nil, false
	}
}

func reduceRoutingAuthSessionState(model *Model, action Action) ([]EffectOnce, bool) {
	switch value := action.(type) {
	case StartProviderAuthSessionRequested:
		model.HeaderStatus = "waiting for login…"
		model.InteractionMode = InteractionModeBusySave
		clearSaveErrors(model)
		clearAuthSession(model, strings.TrimSpace(value.OwnerKey)) // swobu:io-string source=boundary
		return []EffectOnce{stateeffect.StartProviderAuthSessionEffect{
			EndpointName:   strings.TrimSpace(value.EndpointName), // swobu:io-string source=boundary
			ProviderConfig: value.ProviderConfig,
			OwnerKey:       strings.TrimSpace(value.OwnerKey),  // swobu:io-string source=boundary
			AuthScope:      strings.TrimSpace(value.AuthScope), // swobu:io-string source=boundary
		}}, true
	case ResetAuthSessionUIRequestedAction:
		clearAuthSessions(model)
		return nil, true
	case ResetAddModelAuthUIRequested:
		clearAuthSessionsByPrefix(model, stateModel.AuthOwnerPrefixAddModelDraft)
		return nil, true
	case stateeffect.ProviderAuthSessionStarted:
		return reduceProviderAuthSessionStarted(model, value), true
	case stateeffect.ProviderAuthSessionFailedAction:
		reduceProviderAuthSessionFailed(model, value)
		return nil, true
	case stateeffect.ProviderAuthSessionPolledAction:
		reduceProviderAuthSessionPolled(model, value)
		return nil, true
	case stateeffect.AuthSessionCopyNoted:
		reduceAuthSessionCopyNoted(model, value)
		return nil, true
	case stateeffect.PollProviderAuthSessionRequestedAction:
		return []EffectOnce{stateeffect.PollProviderAuthSessionEffect{
			EndpointName:   strings.TrimSpace(value.EndpointName), // swobu:io-string source=boundary
			ProviderConfig: value.ProviderConfig,
			OwnerKey:       strings.TrimSpace(value.OwnerKey),  // swobu:io-string source=boundary
			AuthScope:      strings.TrimSpace(value.AuthScope), // swobu:io-string source=boundary
			SessionID:      strings.TrimSpace(value.SessionID), // swobu:io-string source=boundary
			AttemptsLeft:   value.AttemptsLeft,
		}}, true
	default:
		return nil, false
	}
}

func reduceRoutingCredentialState(model *Model, action Action) ([]EffectOnce, bool) {
	switch value := action.(type) {
	case stateeffect.ProviderAuthSessionCredentialResolvedAction:
		return reduceProviderAuthSessionCredentialResolved(model, value), true
	case stateeffect.KeychainCredentialStored:
		model.HeaderStatus = "saved"
		model.InteractionMode = InteractionModeNAV
		clearSaveErrors(model)
		model.LastStoredKeyProviderSpec = strings.TrimSpace(value.ProviderSpec) // swobu:io-string source=boundary
		model.LastStoredKeySlotName = strings.TrimSpace(value.KeyName)          // swobu:io-string source=boundary
		if effects := reloadRoutingModelCatalogAfterKeychainStore(model, value.ProviderSpec); len(effects) != 0 {
			return effects, true
		}
		return nil, true
	default:
		return nil, false
	}
}

func reloadRoutingModelCatalogAfterKeychainStore(model *Model, providerSpec string) []EffectOnce {
	providerSpec = strings.TrimSpace(providerSpec) // swobu:io-string source=boundary
	if providerSpec == "" {
		return nil
	}
	isKeychainRef := func(ref string) bool {
		trimmed := strings.TrimSpace(ref) // swobu:io-string source=boundary
		if trimmed == "" {
			return false
		}
		lowered := strings.ToLower(trimmed) // swobu:io-string source=domain
		return lowered == "keychain" || strings.HasPrefix(lowered, "keychain:")
	}
	if strings.TrimSpace(model.CurrentEndpoint) == "" { // swobu:io-string source=domain
		draft := model.CreateDraftProviderConfig
		if !strings.EqualFold(strings.TrimSpace(draft.ProviderSpec), providerSpec) || !isKeychainRef(draft.CredentialRef) { // swobu:io-string source=domain
			return nil
		}
		return []EffectOnce{stateeffect.LoadRoutingModelCatalogEffect{
			Scope:            RoutingModelCatalogScopeCreateDraft,
			ProviderSpec:     strings.TrimSpace(draft.ProviderSpec),     // swobu:io-string source=boundary
			BaseURL:          strings.TrimSpace(draft.BaseURL),          // swobu:io-string source=boundary
			AuthHeader:       strings.TrimSpace(draft.AuthHeader),       // swobu:io-string source=boundary
			CredentialRef:    strings.TrimSpace(draft.CredentialRef),    // swobu:io-string source=boundary
			ProviderProtocol: strings.TrimSpace(draft.ProviderProtocol), // swobu:io-string source=domain
		}}
	}
	if !strings.EqualFold(strings.TrimSpace(model.AddModelDraftProviderSpec), providerSpec) || !isKeychainRef(model.AddModelDraftCredentialRef) { // swobu:io-string source=domain
		return nil
	}
	return []EffectOnce{stateeffect.LoadRoutingModelCatalogEffect{
		Scope:            RoutingModelCatalogScopeAddModelDraft,
		ProviderSpec:     strings.TrimSpace(model.AddModelDraftProviderSpec),     // swobu:io-string source=boundary
		BaseURL:          strings.TrimSpace(model.AddModelDraftBaseURL),          // swobu:io-string source=boundary
		AuthHeader:       strings.TrimSpace(model.AddModelDraftAuthHeader),       // swobu:io-string source=boundary
		CredentialRef:    strings.TrimSpace(model.AddModelDraftCredentialRef),    // swobu:io-string source=boundary
		ProviderProtocol: strings.TrimSpace(model.AddModelDraftProviderProtocol), // swobu:io-string source=domain
	}}
}

func reduceProviderAuthSessionStarted(model *Model, value stateeffect.ProviderAuthSessionStarted) []EffectOnce {
	model.HeaderStatus = "waiting for login…"
	model.InteractionMode = InteractionModeManageList
	setAuthSession(model, strings.TrimSpace(value.OwnerKey), stateModel.AuthSessionViewState{ // swobu:io-string source=boundary
		SessionID:    strings.TrimSpace(value.SessionID),    // swobu:io-string source=boundary
		URL:          strings.TrimSpace(value.AuthorizeURL), // swobu:io-string source=boundary
		UserCode:     strings.TrimSpace(value.UserCode),     // swobu:io-string source=boundary
		SessionState: strings.TrimSpace(value.State),        // swobu:io-string source=boundary
		SessionError: "",
		CopyNote:     "",
	})
	loginURL := strings.TrimSpace(value.AuthorizeURL) // swobu:io-string source=boundary
	if loginURL == "" {
		return nil
	}
	return []EffectOnce{stateeffect.OpenSupportLinkEffect{Label: "login", URL: loginURL}}
}

func reduceProviderAuthSessionFailed(model *Model, value stateeffect.ProviderAuthSessionFailedAction) {
	model.HeaderStatus = "ready"
	model.InteractionMode = InteractionModeManageList
	clearSaveErrors(model)
	ownerKey := strings.TrimSpace(value.OwnerKey) // swobu:io-string source=boundary
	previous, hadPrevious := authSession(model, ownerKey)
	url := ""
	userCode := ""
	sessionID := ""
	if hadPrevious {
		url = strings.TrimSpace(previous.URL)             // swobu:io-string source=boundary
		userCode = strings.TrimSpace(previous.UserCode)   // swobu:io-string source=boundary
		sessionID = strings.TrimSpace(previous.SessionID) // swobu:io-string source=domain
	}
	setAuthSession(model, ownerKey, stateModel.AuthSessionViewState{
		SessionID:    sessionID,
		URL:          url,
		UserCode:     userCode,
		SessionState: "failed",
		SessionError: strings.TrimSpace(value.Message), // swobu:io-string source=boundary
		CopyNote:     "",
	})
}

func reduceProviderAuthSessionPolled(model *Model, value stateeffect.ProviderAuthSessionPolledAction) {
	ownerKey := strings.TrimSpace(value.OwnerKey) // swobu:io-string source=boundary
	session, ok := authSession(model, ownerKey)
	if ok && strings.TrimSpace(session.SessionID) == strings.TrimSpace(value.SessionID) { // swobu:io-string source=boundary
		session.SessionState = strings.TrimSpace(value.State)        // swobu:io-string source=boundary
		session.SessionError = strings.TrimSpace(value.ErrorMessage) // swobu:io-string source=boundary
		setAuthSession(model, ownerKey, session)
	}
}

func reduceAuthSessionCopyNoted(model *Model, value stateeffect.AuthSessionCopyNoted) {
	ownerKey := strings.TrimSpace(value.OwnerKey) // swobu:io-string source=boundary
	session, _ := authSession(model, ownerKey)
	session.CopyNote = strings.TrimSpace(value.Message) // swobu:io-string source=boundary
	setAuthSession(model, ownerKey, session)
}

func reduceProviderAuthSessionCredentialResolved(model *Model, value stateeffect.ProviderAuthSessionCredentialResolvedAction) []EffectOnce {
	if strings.TrimSpace(value.AuthScope) == stateModel.AuthScopeCreateDraft { // swobu:io-string source=boundary
		model.HeaderStatus = "login complete"
		model.InteractionMode = InteractionModeManageList
		clearSaveErrors(model)
		model.CreateDraftProviderConfig.ProviderSpec = strings.TrimSpace(value.ProviderConfig.ProviderSpec)    // swobu:io-string source=boundary
		model.CreateDraftProviderConfig.BaseURL = strings.TrimSpace(value.ProviderConfig.BaseURL)              // swobu:io-string source=boundary
		model.CreateDraftProviderConfig.CredentialRef = strings.TrimSpace(value.CredentialRef)                 // swobu:io-string source=boundary
		model.CreateDraftModelProviderSpec = strings.TrimSpace(model.CreateDraftProviderConfig.ProviderSpec)   // swobu:io-string source=boundary
		model.CreateDraftModelBaseURL = strings.TrimSpace(model.CreateDraftProviderConfig.BaseURL)             // swobu:io-string source=boundary
		model.CreateDraftModelCredentialRef = strings.TrimSpace(model.CreateDraftProviderConfig.CredentialRef) // swobu:io-string source=domain
		model.CreateDraftModelError = ""
		model.CreateDraftModelProbePending = true
		clearAuthSession(model, strings.TrimSpace(value.OwnerKey)) // swobu:io-string source=boundary
		return []EffectOnce{stateeffect.LoadRoutingModelCatalogEffect{
			Scope:            RoutingModelCatalogScopeCreateDraft,
			ProviderSpec:     strings.TrimSpace(model.CreateDraftProviderConfig.ProviderSpec),     // swobu:io-string source=boundary
			BaseURL:          strings.TrimSpace(model.CreateDraftProviderConfig.BaseURL),          // swobu:io-string source=boundary
			AuthHeader:       strings.TrimSpace(model.CreateDraftProviderConfig.AuthHeader),       // swobu:io-string source=boundary
			CredentialRef:    strings.TrimSpace(model.CreateDraftProviderConfig.CredentialRef),    // swobu:io-string source=boundary
			ProviderProtocol: strings.TrimSpace(model.CreateDraftProviderConfig.ProviderProtocol), // swobu:io-string source=domain
		}}
	}
	if stateModel.AuthOwnerKey(strings.TrimSpace(value.OwnerKey)).IsAddModelDraft() { // swobu:io-string source=boundary
		model.HeaderStatus = "login complete"
		model.InteractionMode = InteractionModeManageList
		clearSaveErrors(model)
		model.AddModelDraftProviderSpec = strings.TrimSpace(value.ProviderConfig.ProviderSpec) // swobu:io-string source=boundary
		model.AddModelDraftBaseURL = strings.TrimSpace(value.ProviderConfig.BaseURL)           // swobu:io-string source=boundary
		model.AddModelDraftCredentialRef = strings.TrimSpace(value.CredentialRef)              // swobu:io-string source=boundary
		model.AddModelDraftModelError = ""
		model.AddModelDraftModelProbePending = true
		clearAuthSession(model, strings.TrimSpace(value.OwnerKey)) // swobu:io-string source=boundary
		return []EffectOnce{stateeffect.LoadRoutingModelCatalogEffect{
			Scope:            RoutingModelCatalogScopeAddModelDraft,
			ProviderSpec:     strings.TrimSpace(model.AddModelDraftProviderSpec),     // swobu:io-string source=boundary
			BaseURL:          strings.TrimSpace(model.AddModelDraftBaseURL),          // swobu:io-string source=boundary
			AuthHeader:       strings.TrimSpace(model.AddModelDraftAuthHeader),       // swobu:io-string source=boundary
			CredentialRef:    strings.TrimSpace(model.AddModelDraftCredentialRef),    // swobu:io-string source=boundary
			ProviderProtocol: strings.TrimSpace(model.AddModelDraftProviderProtocol), // swobu:io-string source=boundary
		}}
	}
	next := value.ProviderConfig
	next.CredentialRef = strings.TrimSpace(value.CredentialRef) // swobu:io-string source=boundary
	model.HeaderStatus = "saving…"
	model.InteractionMode = InteractionModeBusySave
	clearSaveErrors(model)
	clearAuthSession(model, strings.TrimSpace(value.OwnerKey)) // swobu:io-string source=boundary
	return []EffectOnce{stateeffect.SaveProviderConfigEffect(SaveProviderConfigRequested{
		EndpointName:   strings.TrimSpace(value.EndpointName), // swobu:io-string source=boundary
		ProviderConfig: next,
		ErrorAnchor:    "",
	})}
}
