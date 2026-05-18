package routing

import (
	"strings"

	"github.com/swobuforge/swobu/internal/domain/providercatalog"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	stateModel "github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state/model"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/views"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
	"github.com/swobuforge/swobu/internal/terminalui/view/retained"
)

func interactiveAddModelCredentialRows(
	model state.Model,
	providerSpec string,
	endpointName string,
	draft state.ProviderConfigSnapshot,
	source string,
) []retained.ViewSpec[state.Model] {
	variant := providercatalog.AuthVariant(strings.ToLower(strings.TrimSpace(source)))                                                        // swobu:io-string source=boundary
	if !providercatalog.SupportsAuthVariant(strings.TrimSpace(providerSpec), variant) || !providercatalog.IsInteractiveAuthVariant(variant) { // swobu:io-string source=boundary
		return nil
	}
	return interactiveAuthStatusRows(model, interactiveAuthRenderConfig{
		EndpointName: strings.TrimSpace(endpointName), // swobu:io-string source=boundary
		Draft:        draft,
		Variant:      variant,
		StartAuth: func(next state.ProviderConfigSnapshot) []update.Action {
			return startAuthActionsForAddModel(endpointName, next)
		},
		SwitchToDeviceAuth: func(next state.ProviderConfigSnapshot) []update.Action {
			return append([]update.Action{state.ResetAddModelAuthUIRequested{}}, startAuthActionsForAddModel(endpointName, next)...)
		},
	})
}

type interactiveAuthPhase string

const (
	interactiveAuthPhaseNone             interactiveAuthPhase = "none"
	interactiveAuthPhaseStartRequired    interactiveAuthPhase = "start_required"
	interactiveAuthPhaseInProgress       interactiveAuthPhase = "in_progress"
	interactiveAuthPhaseStartUnavailable interactiveAuthPhase = "start_unavailable"
	interactiveAuthPhaseExpired          interactiveAuthPhase = "expired"
	interactiveAuthPhaseResolved         interactiveAuthPhase = "resolved"
)

func classifyInteractiveAuthPhase(model state.Model, endpointName string, draft state.ProviderConfigSnapshot, variant providercatalog.AuthVariant) interactiveAuthPhase {
	authState := addModelAuthStateForDraft(model, endpointName, draft)
	if strings.EqualFold(strings.TrimSpace(authState.SessionState), "expired") { // swobu:io-string source=boundary
		return interactiveAuthPhaseExpired
	}
	if variant == providercatalog.AuthVariantChatGPTLogin &&
		strings.EqualFold(strings.TrimSpace(authState.SessionState), "failed") && // swobu:io-string source=boundary
		strings.TrimSpace(authState.SessionID) != "" { // swobu:io-string source=boundary
		return interactiveAuthPhaseStartUnavailable
	}
	sessionActive := strings.TrimSpace(authState.SessionID) != "" // swobu:io-string source=boundary
	if sessionActive {
		return interactiveAuthPhaseInProgress
	}
	if variant == providercatalog.AuthVariantChatGPTLogin {
		if strings.EqualFold(addModelCredentialSummary(model, draft), "signed in") {
			return interactiveAuthPhaseResolved
		}
		return interactiveAuthPhaseStartRequired
	}
	if strings.EqualFold(addModelCredentialSummary(model, draft), "signed in") {
		return interactiveAuthPhaseResolved
	}
	return interactiveAuthPhaseNone
}

type interactiveAuthRenderConfig struct {
	EndpointName       string
	Draft              state.ProviderConfigSnapshot
	Variant            providercatalog.AuthVariant
	StartAuth          func(next state.ProviderConfigSnapshot) []update.Action
	SwitchToDeviceAuth func(next state.ProviderConfigSnapshot) []update.Action
}

func interactiveAuthStatusRows(model state.Model, cfg interactiveAuthRenderConfig) []retained.ViewSpec[state.Model] {
	rows := make([]retained.ViewSpec[state.Model], 0, 6)
	endpointName := strings.TrimSpace(cfg.EndpointName) // swobu:io-string source=boundary
	draft := cfg.Draft
	variant := cfg.Variant
	authState := addModelAuthStateForDraft(model, endpointName, draft)
	viewState := classifyInteractiveAuthPhase(model, endpointName, draft, variant)
	if viewState == interactiveAuthPhaseInProgress || viewState == interactiveAuthPhaseStartUnavailable {
		stateValue := strings.TrimSpace(authState.SessionState) // swobu:io-string source=boundary
		loginURL := strings.TrimSpace(authState.URL)            // swobu:io-string source=boundary
		userCode := strings.TrimSpace(authState.UserCode)       // swobu:io-string source=boundary
		if variant == providercatalog.AuthVariantChatGPTLogin {
			if viewState == interactiveAuthPhaseStartUnavailable {
				rows = append(rows, views.RowStatic("", "could not open default browser"))
			}
		}
		if loginURL != "" {
			rows = append(rows, interactiveAuthLinkRows(loginURL, interactiveAuthOwnerKey(endpointName, strings.TrimSpace(draft.Ref)))...) // swobu:io-string source=boundary
		}
		if shouldRenderInteractiveAuthCode(variant, userCode) {
			rows = append(rows, views.RowAction("code", userCode, "copy", func() []update.Action {
				return []update.Action{
					state.AuthSessionURLCopyScopedRequested{
						OwnerKey: interactiveAuthOwnerKey(endpointName, strings.TrimSpace(draft.Ref)), // swobu:io-string source=boundary
						Value:    userCode,
					},
				}
			}))
		}
		if strings.EqualFold(stateValue, "pending") {
			rows = append(rows, views.RowStatic("", "waiting for sign-in..."))
		}
	} else if viewState == interactiveAuthPhaseStartRequired {
		rows = append(rows, views.RowAction("sign in", "open default browser", "open", func() []update.Action {
			if cfg.StartAuth == nil {
				return nil
			}
			return cfg.StartAuth(draft)
		}))
		loginURL := strings.TrimSpace(authState.URL) // swobu:io-string source=boundary
		if loginURL != "" {
			rows = append(rows, interactiveAuthLinkRows(loginURL, interactiveAuthOwnerKey(endpointName, strings.TrimSpace(draft.Ref)))...) // swobu:io-string source=boundary
		}
	} else if viewState == interactiveAuthPhaseResolved && variant == providercatalog.AuthVariantChatGPTLogin {
		rows = append(rows, views.RowAction("sign in", "sign in another account", "open", func() []update.Action {
			if cfg.StartAuth == nil {
				return nil
			}
			return cfg.StartAuth(draft)
		}))
	}
	if viewState == interactiveAuthPhaseExpired {
		rows = append(rows, views.RowAction("code expired", "", "refresh", func() []update.Action {
			if cfg.StartAuth == nil {
				return nil
			}
			return cfg.StartAuth(draft)
		}))
	}
	if variant == providercatalog.AuthVariantChatGPTLogin &&
		(viewState == interactiveAuthPhaseStartRequired || viewState == interactiveAuthPhaseInProgress || viewState == interactiveAuthPhaseStartUnavailable) {
		rows = append(rows, views.RowAction("fallback", "use device code", "switch", func() []update.Action {
			next := draft
			next.CredentialRef = string(providercatalog.AuthVariantChatGPTDeviceAuth)
			if cfg.SwitchToDeviceAuth != nil {
				return cfg.SwitchToDeviceAuth(next)
			}
			if cfg.StartAuth != nil {
				return cfg.StartAuth(next)
			}
			return nil
		}))
	}
	if strings.TrimSpace(authState.SessionError) != "" { // swobu:io-string source=boundary
		rows = append(rows, views.DisclosureNoteRows(authState.SessionError)...)
	}
	if shouldShowAuthStartRetryHint(authState.SessionError, authState.SessionID) {
		rows = append(rows, views.DisclosureNoteRows("auth start failed; retry or switch auth method")...)
	}
	return rows
}

type addModelAuthState struct {
	SessionID    string
	URL          string
	UserCode     string
	SessionState string
	SessionError string
}

func addModelAuthStateForDraft(model state.Model, endpointName string, draft state.ProviderConfigSnapshot) addModelAuthState {
	ownerKey := interactiveAuthOwnerKey(strings.TrimSpace(endpointName), strings.TrimSpace(draft.Ref)) // swobu:io-string source=boundary
	if model.AuthSessions == nil {
		return addModelAuthState{}
	}
	session, ok := model.AuthSessions[strings.TrimSpace(ownerKey)] // swobu:io-string source=boundary
	if !ok {
		return addModelAuthState{}
	}
	return addModelAuthState{
		SessionID:    session.SessionID,
		URL:          session.URL,
		UserCode:     session.UserCode,
		SessionState: session.SessionState,
		SessionError: session.SessionError,
	}
}

func interactiveAuthOwnerKey(endpointName string, providerRef string) string {
	if strings.TrimSpace(endpointName) == "" { // swobu:io-string source=boundary
		return stateModel.CreateDraftAuthOwnerKey(strings.TrimSpace(providerRef)).String() // swobu:io-string source=boundary
	}
	return stateModel.AddModelDraftAuthOwnerKey(strings.TrimSpace(endpointName), strings.TrimSpace(providerRef)).String() // swobu:io-string source=boundary
}

func shouldShowAuthStartRetryHint(sessionError string, sessionID string) bool {
	errText := strings.TrimSpace(sessionError)               // swobu:io-string source=boundary
	if errText == "" || strings.TrimSpace(sessionID) != "" { // swobu:io-string source=boundary
		return false
	}
	// Credential store failures happen after auth completion and should not be
	// misreported as auth-start failures.
	if strings.Contains(strings.ToLower(errText), "credential store failed") { // swobu:io-string source=boundary
		return false
	}
	return true
}

func shouldRenderInteractiveAuthCode(variant providercatalog.AuthVariant, userCode string) bool {
	return variant == providercatalog.AuthVariantChatGPTDeviceAuth && strings.TrimSpace(userCode) != "" // swobu:io-string source=boundary
}

func interactiveAuthLinkRows(loginURL string, ownerKey string) []retained.ViewSpec[state.Model] {
	url := strings.TrimSpace(loginURL) // swobu:io-string source=boundary
	if url == "" {
		return nil
	}
	rows := []retained.ViewSpec[state.Model]{
		views.RowActionWideValue("link", "", "copy", func() []update.Action {
			return []update.Action{
				state.AuthSessionURLCopyScopedRequested{
					OwnerKey: strings.TrimSpace(ownerKey), // swobu:io-string source=boundary
					Value:    url,
				},
			}
		}),
	}
	rows = append(rows, views.WrappedDetailRows(url)...)
	return rows
}
