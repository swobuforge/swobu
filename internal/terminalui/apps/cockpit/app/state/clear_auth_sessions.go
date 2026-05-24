package state

import (
	"strings"

	stateModel "github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state/model"
)

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
