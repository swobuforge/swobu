package state

import (
	"fmt"
	"strings"
	"time"

	"github.com/swobuforge/swobu/internal/domain/endpointintent"
	stateeffect "github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state/effect"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
)

const daemonRefreshInterval = 2 * time.Second

func reduceBehaviorState(model *Model, action update.Action) []update.Effect {
	if effects, handled := reduceSupportActions(model, action); handled {
		return effects
	}
	switch value := action.(type) {
	case stateeffect.DaemonRefreshTick:
		if model.ControlPlane != nil {
			return []update.Effect{
				stateeffect.RefreshDaemonStatusEffect{},
				stateeffect.ScheduleDaemonRefreshEffect{Delay: daemonRefreshInterval},
			}
		}
		return []update.Effect{
			stateeffect.RefreshDaemonStatusEffect{},
			stateeffect.RefreshEndpointsEffect{},
			refreshStatusProjectionEffectFor(model),
			stateeffect.ScheduleDaemonRefreshEffect{Delay: daemonRefreshInterval},
		}
	case ToggleStream:
		model.StreamEnabled = !model.StreamEnabled
		return nil
	case SetInteractionMode:
		mode := strings.TrimSpace(value.Mode) // swobu:io-string source=boundary
		if mode == "" {
			mode = InteractionModeNAV
		}
		model.InteractionMode = mode
		return nil
	case SetFocusedRowAffordance:
		applyFocusedRowFooterAffordance(model, value)
		return nil
	case LoadRoutingModelCatalogRequestedAction:
		scope := strings.TrimSpace(value.Scope)                 // swobu:io-string source=boundary
		spec := strings.TrimSpace(value.ProviderSpec)           // swobu:io-string source=boundary
		baseURL := strings.TrimSpace(value.BaseURL)             // swobu:io-string source=boundary
		credentialRef := strings.TrimSpace(value.CredentialRef) // swobu:io-string source=boundary
		if scope == RoutingModelCatalogScopeCreateDraft {
			if spec == "" {
				model.CreateDraftModelIDs = nil
				model.CreateDraftModelError = ""
				return nil
			}
			model.CreateDraftModelIDs = nil
			model.CreateDraftModelError = ""
		} else if scope == RoutingModelCatalogScopeAddModelDraft {
			if spec == "" {
				model.AddModelDraftModelIDs = nil
				model.AddModelDraftModelError = ""
				model.AddModelDraftProviderSpec = ""
				model.AddModelDraftBaseURL = ""
				model.AddModelDraftCredentialRef = ""
				return nil
			}
			model.AddModelDraftProviderSpec = spec
			model.AddModelDraftBaseURL = baseURL
			model.AddModelDraftCredentialRef = credentialRef
			model.AddModelDraftModelIDs = nil
			model.AddModelDraftModelError = ""
		} else {
			return nil
		}
		return []update.Effect{stateeffect.LoadRoutingModelCatalogEffect{
			Scope:         scope,
			ProviderSpec:  spec,
			BaseURL:       baseURL,
			CredentialRef: credentialRef, // swobu:io-string source=boundary
		}}
	case stateeffect.RoutingModelCatalogLoaded:
		scope := strings.TrimSpace(value.Scope)                                                                                                                             // swobu:io-string source=boundary
		if !matchesRoutingModelCatalogLoad(model, scope, strings.TrimSpace(value.ProviderSpec), strings.TrimSpace(value.BaseURL), strings.TrimSpace(value.CredentialRef)) { // swobu:io-string source=boundary
			return nil
		}
		if scope == RoutingModelCatalogScopeCreateDraft {
			model.CreateDraftModelIDs = append([]string(nil), value.ModelIDs...)
			model.CreateDraftModelError = strings.TrimSpace(value.Error) // swobu:io-string source=boundary
		} else if scope == RoutingModelCatalogScopeAddModelDraft {
			model.AddModelDraftModelIDs = append([]string(nil), value.ModelIDs...)
			model.AddModelDraftModelError = strings.TrimSpace(value.Error) // swobu:io-string source=boundary
		}
		return nil
	case FocusNextAfterRebuildRequested:
		return []update.Effect{stateeffect.FocusNextAfterRebuildEffect{Delay: 2 * time.Millisecond}}
	case EndpointCopyRequested:
		return []update.Effect{stateeffect.CopyEndpointValueEffect(value)}
	case AuthSessionURLCopyRequested:
		return []update.Effect{stateeffect.CopyAuthSessionURLEffect{Value: strings.TrimSpace(value.Value)}} // swobu:io-string source=boundary
	case AuthSessionURLCopyScopedRequested:
		return []update.Effect{stateeffect.CopyAuthSessionURLEffect{
			OwnerKey: strings.TrimSpace(value.OwnerKey), // swobu:io-string source=boundary
			Value:    strings.TrimSpace(value.Value),    // swobu:io-string source=boundary
		}}
	case ClientBaseURLCopyRequestedAction:
		return []update.Effect{stateeffect.CopyClientBaseURLEffect(value)}
	case ClientLaunchRequestedAction:
		model.HeaderStatus = "running…"
		model.InteractionMode = InteractionModeBusyLaunch
		return []update.Effect{stateeffect.LaunchClientEffect(value)}
	case RefreshStatusProjectionRequested:
		return []update.Effect{refreshStatusProjectionEffectFor(model)}
	default:
		return nil
	}
}

func reduceSupportActions(model *Model, action update.Action) ([]update.Effect, bool) {
	switch value := action.(type) {
	case SetHelpTabOpenAction:
		model.HelpTabOpen = value.Open
		if !model.HelpTabOpen {
			model.HelpNote = ""
		}
		return nil, true
	case OpenSupportLinkRequested:
		return []update.Effect{stateeffect.OpenSupportLinkEffect{
			Label: strings.TrimSpace(value.Label), // swobu:io-string source=boundary
			URL:   strings.TrimSpace(value.URL),   // swobu:io-string source=boundary
		}}, true
	case HelpDiagnosticsCopyRequested:
		return []update.Effect{stateeffect.CopyHelpDiagnosticsEffect{Text: strings.TrimSpace(value.Text)}}, true // swobu:io-string source=boundary
	case CompatibilityRestartRequested:
		if model.ControlPlane == nil {
			return nil, true
		}
		return []update.Effect{stateeffect.CompatibilityRestartHintEffect{
			Command: strings.TrimSpace(model.ControlPlane.RecoveryCommand), // swobu:io-string source=boundary
		}}, true
	case CompatibilityDiagnosticsCopyRequested:
		if model.ControlPlane == nil {
			return nil, true
		}
		return []update.Effect{stateeffect.CopyCompatibilityDiagnosticsEffect{
			Text: compatibilityDiagnostics(*model.ControlPlane),
		}}, true
	case stateeffect.CompatibilityRecoveryNoted:
		if model.ControlPlane == nil {
			return nil, true
		}
		model.ControlPlane.Note = strings.TrimSpace(value.Message)      // swobu:io-string source=boundary
		model.ControlPlane.NoteAction = strings.TrimSpace(value.Action) // swobu:io-string source=boundary
		return nil, true
	default:
		return nil, false
	}
}

func firstRunFooterVerb(model *Model, fallback string) string {
	verb := strings.TrimSpace(fallback) // swobu:io-string source=boundary
	if firstRunCreateReady(model) && firstRunPrimaryCreateEligible(verb) {
		verb = "create"
	}
	if strings.TrimSpace(model.CreateDraftName) == "" { // swobu:io-string source=boundary
		verb = "edit"
	} else if strings.TrimSpace(model.CreateDraftProviderConfig.ProviderSpec) == "" { // swobu:io-string source=boundary
		verb = "open"
	}
	return strings.TrimSpace(verb) // swobu:io-string source=boundary
}

func firstRunPrimaryCreateEligible(focusVerb string) bool {
	verb := strings.TrimSpace(focusVerb) // swobu:io-string source=boundary
	return verb == "choose" || verb == "next" || verb == "create"
}

func applyFocusedRowFooterAffordance(model *Model, value SetFocusedRowAffordance) {
	verb := strings.TrimSpace(value.Verb) // swobu:io-string source=boundary
	model.FooterBaseVerb = verb
	if model.ControlPlane != nil {
		model.FooterVerb = "run/copy"
		model.FooterAllowSpace = value.AllowSpace
		model.FooterShowTabs = false
		return
	}
	if model.CurrentEndpoint == "" && len(model.Endpoints) == 0 {
		model.FooterVerb = firstRunFooterVerb(model, verb)
		model.FooterAllowSpace = value.AllowSpace
		model.FooterShowTabs = false
		return
	}
	model.FooterVerb = verb
	model.FooterAllowSpace = value.AllowSpace
	model.FooterShowTabs = true
}

func refreshFirstRunFooterAffordance(model *Model) {
	if model == nil {
		return
	}
	if model.ControlPlane != nil {
		return
	}
	if strings.TrimSpace(model.CurrentEndpoint) != "" || len(model.Endpoints) > 0 { // swobu:io-string source=boundary
		return
	}
	model.FooterVerb = firstRunFooterVerb(model, model.FooterVerb)
	model.FooterShowTabs = false
}

func firstRunCreateReady(model *Model) bool {
	name := strings.TrimSpace(model.CreateDraftName) // swobu:io-string source=boundary
	if name == "" {
		return false
	}
	if _, err := endpointintent.ParseEndpointName(name); err != nil {
		return false
	}
	for _, existing := range model.Endpoints {
		if strings.TrimSpace(existing) == name { // swobu:io-string source=boundary
			return false
		}
	}
	provider := strings.TrimSpace(model.CreateDraftProviderConfig.ProviderSpec) // swobu:io-string source=boundary
	if provider == "" {
		return false
	}
	if provider == "openai_compatible" && strings.TrimSpace(model.CreateDraftProviderConfig.BaseURL) == "" { // swobu:io-string source=boundary
		return false
	}
	if strings.TrimSpace(model.CreateDraftProviderConfig.ModelID) == "" { // swobu:io-string source=boundary
		return false
	}
	if ProviderRequiresCredential(provider, strings.TrimSpace(model.CreateDraftProviderConfig.BaseURL)) && strings.TrimSpace(model.CreateDraftProviderConfig.CredentialRef) == "" { // swobu:io-string source=boundary
		return false
	}
	return true
}

func compatibilityDiagnostics(mismatch ControlPlaneMismatch) string {
	daemonVersion := strings.TrimSpace(mismatch.DaemonVersion) // swobu:io-string source=boundary
	tuiVersion := strings.TrimSpace(mismatch.TUIVersion)       // swobu:io-string source=boundary
	protocolGot := "missing"
	if mismatch.HasDaemonProtocol {
		protocolGot = fmt.Sprintf("%d", mismatch.DaemonProtocol)
	}
	return strings.Join([]string{
		"swobu " + tuiVersion,
		"daemon " + daemonVersion,
		fmt.Sprintf("protocol mismatch: expected %d, got %s", mismatch.ExpectedProtocol, protocolGot),
	}, "\n")
}

func matchesRoutingModelCatalogLoad(model *Model, scope, providerSpec, baseURL, credentialRef string) bool {
	normalizedScope := strings.TrimSpace(scope)
	if normalizedScope == RoutingModelCatalogScopeCreateDraft {
		if strings.TrimSpace(model.CreateDraftProviderConfig.ProviderSpec) != providerSpec { // swobu:io-string source=boundary
			return false
		}
		if strings.TrimSpace(model.CreateDraftProviderConfig.BaseURL) != baseURL { // swobu:io-string source=boundary
			return false
		}
		if strings.TrimSpace(model.CreateDraftProviderConfig.CredentialRef) != credentialRef { // swobu:io-string source=boundary
			return false
		}
		return true
	}
	if normalizedScope == RoutingModelCatalogScopeAddModelDraft {
		if strings.TrimSpace(model.AddModelDraftProviderSpec) != providerSpec { // swobu:io-string source=boundary
			return false
		}
		if strings.TrimSpace(model.AddModelDraftBaseURL) != baseURL { // swobu:io-string source=boundary
			return false
		}
		if strings.TrimSpace(model.AddModelDraftCredentialRef) != credentialRef { // swobu:io-string source=boundary
			return false
		}
		return true
	}
	return false
}

func refreshStatusProjectionEffectFor(model *Model) stateeffect.RefreshStatusProjectionEffect {
	if model == nil {
		return stateeffect.RefreshStatusProjectionEffect{}
	}
	return stateeffect.RefreshStatusProjectionEffect{EndpointName: strings.TrimSpace(model.CurrentEndpoint)} // swobu:io-string source=boundary
}
