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
	variant := providercatalog.AuthVariant(strings.ToLower(strings.TrimSpace(source)))                                                        // trimlowerlint:allow boundary canonicalization
	if !providercatalog.SupportsAuthVariant(strings.TrimSpace(providerSpec), variant) || !providercatalog.IsInteractiveAuthVariant(variant) { // trimlowerlint:allow boundary canonicalization
		return nil
	}
	return interactiveAuthStatusRows(model, interactiveAuthRenderConfig{
		EndpointName: strings.TrimSpace(endpointName), // trimlowerlint:allow boundary canonicalization
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
	if strings.EqualFold(strings.TrimSpace(authState.SessionState), "expired") { // trimlowerlint:allow boundary canonicalization
		return interactiveAuthPhaseExpired
	}
	if variant == providercatalog.AuthVariantChatGPTLogin &&
		strings.EqualFold(strings.TrimSpace(authState.SessionState), "failed") && // trimlowerlint:allow boundary canonicalization
		strings.TrimSpace(authState.SessionID) != "" { // trimlowerlint:allow boundary canonicalization
		return interactiveAuthPhaseStartUnavailable
	}
	sessionActive := strings.TrimSpace(authState.SessionID) != "" // trimlowerlint:allow boundary canonicalization
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
	endpointName := strings.TrimSpace(cfg.EndpointName) // trimlowerlint:allow boundary canonicalization
	draft := cfg.Draft
	variant := cfg.Variant
	authState := addModelAuthStateForDraft(model, endpointName, draft)
	viewState := classifyInteractiveAuthPhase(model, endpointName, draft, variant)
	if viewState == interactiveAuthPhaseInProgress || viewState == interactiveAuthPhaseStartUnavailable {
		stateValue := strings.TrimSpace(authState.SessionState) // trimlowerlint:allow boundary canonicalization
		loginURL := strings.TrimSpace(authState.URL)            // trimlowerlint:allow boundary canonicalization
		userCode := strings.TrimSpace(authState.UserCode)       // trimlowerlint:allow boundary canonicalization
		if variant == providercatalog.AuthVariantChatGPTLogin {
			if viewState == interactiveAuthPhaseStartUnavailable {
				rows = append(rows, views.RowStatic("", "could not open default browser"))
			}
		}
		if loginURL != "" {
			rows = append(rows, interactiveAuthLinkRows(loginURL, stateModel.AddModelDraftAuthOwnerKey(endpointName, strings.TrimSpace(draft.Ref)).String())...) // trimlowerlint:allow boundary canonicalization
		}
		if shouldRenderInteractiveAuthCode(variant, userCode) {
			rows = append(rows, views.RowAction("code", userCode, "copy", func() []update.Action {
				return []update.Action{
					state.AuthSessionURLCopyScopedRequested{
						OwnerKey: stateModel.AddModelDraftAuthOwnerKey(endpointName, strings.TrimSpace(draft.Ref)).String(), // trimlowerlint:allow boundary canonicalization
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
		loginURL := strings.TrimSpace(authState.URL) // trimlowerlint:allow boundary canonicalization
		if loginURL != "" {
			rows = append(rows, interactiveAuthLinkRows(loginURL, stateModel.AddModelDraftAuthOwnerKey(endpointName, strings.TrimSpace(draft.Ref)).String())...) // trimlowerlint:allow boundary canonicalization
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
	if strings.TrimSpace(authState.SessionError) != "" { // trimlowerlint:allow boundary canonicalization
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
	ownerKey := stateModel.AddModelDraftAuthOwnerKey(strings.TrimSpace(endpointName), strings.TrimSpace(draft.Ref)).String() // trimlowerlint:allow boundary canonicalization
	if model.AuthSessions == nil {
		return addModelAuthState{}
	}
	session, ok := model.AuthSessions[strings.TrimSpace(ownerKey)] // trimlowerlint:allow boundary canonicalization
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

func shouldShowAuthStartRetryHint(sessionError string, sessionID string) bool {
	errText := strings.TrimSpace(sessionError)               // trimlowerlint:allow boundary canonicalization
	if errText == "" || strings.TrimSpace(sessionID) != "" { // trimlowerlint:allow boundary canonicalization
		return false
	}
	// Credential store failures happen after auth completion and should not be
	// misreported as auth-start failures.
	if strings.Contains(strings.ToLower(errText), "credential store failed") { // trimlowerlint:allow boundary canonicalization
		return false
	}
	return true
}

func shouldRenderInteractiveAuthCode(variant providercatalog.AuthVariant, userCode string) bool {
	return variant == providercatalog.AuthVariantChatGPTDeviceAuth && strings.TrimSpace(userCode) != "" // trimlowerlint:allow boundary canonicalization
}

func interactiveAuthLinkRows(loginURL string, ownerKey string) []retained.ViewSpec[state.Model] {
	url := strings.TrimSpace(loginURL) // trimlowerlint:allow boundary canonicalization
	if url == "" {
		return nil
	}
	rows := []retained.ViewSpec[state.Model]{
		views.RowActionWideValue("link", "", "copy", func() []update.Action {
			return []update.Action{
				state.AuthSessionURLCopyScopedRequested{
					OwnerKey: strings.TrimSpace(ownerKey), // trimlowerlint:allow boundary canonicalization
					Value:    url,
				},
			}
		}),
	}
	rows = append(rows, views.WrappedDetailRows(url)...)
	return rows
}
