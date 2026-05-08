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
		mode := strings.TrimSpace(value.Mode)
		if mode == "" {
			mode = InteractionModeNAV
		}
		model.InteractionMode = mode
		return nil
	case SetFocusedRowAffordance:
		applyFocusedRowFooterAffordance(model, value)
		return nil
	case LoadRoutingModelCatalogRequested:
		scope := strings.TrimSpace(value.Scope)
		spec := strings.TrimSpace(value.ProviderSpec)
		baseURL := strings.TrimSpace(value.BaseURL)
		credentialRef := strings.TrimSpace(value.CredentialRef)
		switch scope {
		case RoutingModelCatalogScopeCreateDraft:
			if spec == "" {
				model.CreateDraftModelIDs = nil
				model.CreateDraftModelError = ""
				return nil
			}
			model.CreateDraftModelIDs = nil
			model.CreateDraftModelError = ""
		case RoutingModelCatalogScopeAddModelDraft:
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
		default:
			return nil
		}
		return []update.Effect{stateeffect.LoadRoutingModelCatalogEffect{
			Scope:         scope,
			ProviderSpec:  spec,
			BaseURL:       baseURL,
			CredentialRef: credentialRef,
			ProtocolKind:  strings.TrimSpace(value.ProtocolKind),
		}}
	case stateeffect.RoutingModelCatalogLoaded:
		scope := strings.TrimSpace(value.Scope)
		if !matchesRoutingModelCatalogLoad(model, scope, strings.TrimSpace(value.ProviderSpec), strings.TrimSpace(value.BaseURL), strings.TrimSpace(value.CredentialRef)) {
			return nil
		}
		switch scope {
		case RoutingModelCatalogScopeCreateDraft:
			model.CreateDraftModelIDs = append([]string(nil), value.ModelIDs...)
			model.CreateDraftModelError = strings.TrimSpace(value.Error)
		case RoutingModelCatalogScopeAddModelDraft:
			model.AddModelDraftModelIDs = append([]string(nil), value.ModelIDs...)
			model.AddModelDraftModelError = strings.TrimSpace(value.Error)
		}
		return nil
	case FocusNextAfterRebuildRequested:
		return []update.Effect{stateeffect.FocusNextAfterRebuildEffect{Delay: 2 * time.Millisecond}}
	case EndpointCopyRequested:
		return []update.Effect{stateeffect.CopyEndpointValueEffect(value)}
	case ClientBaseURLCopyRequested:
		return []update.Effect{stateeffect.CopyClientBaseURLEffect(value)}
	case ClientLaunchRequested:
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
			Label: strings.TrimSpace(value.Label),
			URL:   strings.TrimSpace(value.URL),
		}}, true
	case HelpDiagnosticsCopyRequested:
		return []update.Effect{stateeffect.CopyHelpDiagnosticsEffect{Text: strings.TrimSpace(value.Text)}}, true
	case CompatibilityRestartRequested:
		if model.ControlPlane == nil {
			return nil, true
		}
		return []update.Effect{stateeffect.CompatibilityRestartHintEffect{
			Command: strings.TrimSpace(model.ControlPlane.RecoveryCommand),
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
		model.ControlPlane.Note = strings.TrimSpace(value.Message)
		model.ControlPlane.NoteAction = strings.TrimSpace(value.Action)
		return nil, true
	default:
		return nil, false
	}
}

func firstRunFooterVerb(model *Model, fallback string) string {
	verb := strings.TrimSpace(fallback)
	if strings.TrimSpace(model.CreateDraftProviderConfig.ProviderSpec) != "" && strings.TrimSpace(model.CreateDraftProviderConfig.ModelID) == "" {
		verb = "edit"
	}
	if firstRunCreateReady(model) {
		verb = "create"
	}
	if strings.TrimSpace(model.CreateDraftName) == "" {
		verb = "edit"
	} else if strings.TrimSpace(model.CreateDraftProviderConfig.ProviderSpec) == "" {
		verb = "open"
	}
	return strings.TrimSpace(verb)
}

func applyFocusedRowFooterAffordance(model *Model, value SetFocusedRowAffordance) {
	verb := strings.TrimSpace(value.Verb)
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
	if strings.TrimSpace(model.CurrentEndpoint) != "" || len(model.Endpoints) > 0 {
		return
	}
	model.FooterVerb = firstRunFooterVerb(model, model.FooterVerb)
	model.FooterShowTabs = false
}

func firstRunCreateReady(model *Model) bool {
	name := strings.TrimSpace(model.CreateDraftName)
	if name == "" {
		return false
	}
	if _, err := endpointintent.ParseEndpointName(name); err != nil {
		return false
	}
	for _, existing := range model.Endpoints {
		if strings.TrimSpace(existing) == name {
			return false
		}
	}
	provider := strings.TrimSpace(model.CreateDraftProviderConfig.ProviderSpec)
	if provider == "" {
		return false
	}
	if provider == "custom" && strings.TrimSpace(model.CreateDraftProviderConfig.BaseURL) == "" {
		return false
	}
	if strings.TrimSpace(model.CreateDraftProviderConfig.ModelID) == "" {
		return false
	}
	if ProviderRequiresCredential(provider, strings.TrimSpace(model.CreateDraftProviderConfig.BaseURL)) && strings.TrimSpace(model.CreateDraftProviderConfig.CredentialRef) == "" {
		return false
	}
	return true
}

func compatibilityDiagnostics(mismatch ControlPlaneMismatch) string {
	daemonVersion := strings.TrimSpace(mismatch.DaemonVersion)
	tuiVersion := strings.TrimSpace(mismatch.TUIVersion)
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
	switch strings.TrimSpace(scope) {
	case RoutingModelCatalogScopeCreateDraft:
		if strings.TrimSpace(model.CreateDraftProviderConfig.ProviderSpec) != providerSpec {
			return false
		}
		if strings.TrimSpace(model.CreateDraftProviderConfig.BaseURL) != baseURL {
			return false
		}
		if strings.TrimSpace(model.CreateDraftProviderConfig.CredentialRef) != credentialRef {
			return false
		}
		return true
	case RoutingModelCatalogScopeAddModelDraft:
		if strings.TrimSpace(model.AddModelDraftProviderSpec) != providerSpec {
			return false
		}
		if strings.TrimSpace(model.AddModelDraftBaseURL) != baseURL {
			return false
		}
		if strings.TrimSpace(model.AddModelDraftCredentialRef) != credentialRef {
			return false
		}
		return true
	default:
		return false
	}
}

func refreshStatusProjectionEffectFor(model *Model) stateeffect.RefreshStatusProjectionEffect {
	if model == nil {
		return stateeffect.RefreshStatusProjectionEffect{}
	}
	return stateeffect.RefreshStatusProjectionEffect{EndpointName: strings.TrimSpace(model.CurrentEndpoint)}
}
