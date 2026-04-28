package state

import (
	"fmt"
	"strings"
	"time"

	"github.com/metrofun/swobu/internal/adapters/inbound/tui/engine/update"
	"github.com/metrofun/swobu/internal/domain/endpointintent"
)

const daemonRefreshInterval = 2 * time.Second

func reduceBehaviorState(model *Model, action update.Action) []update.Effect {
	if effects, handled := reduceSupportActions(model, action); handled {
		return effects
	}
	switch value := action.(type) {
	case DaemonRefreshTick:
		if model.ControlPlane != nil {
			return []update.Effect{
				RefreshDaemonStatusEffect{},
				ScheduleDaemonRefreshEffect{Delay: daemonRefreshInterval},
			}
		}
		return []update.Effect{
			RefreshDaemonStatusEffect{},
			RefreshEndpointsEffect{},
			RefreshCatalogEffect{},
			refreshStatusProjectionEffectFor(model),
			ScheduleDaemonRefreshEffect{Delay: daemonRefreshInterval},
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
	case LoadCreateDraftModelCatalogRequested:
		spec := strings.TrimSpace(value.ProviderSpec)
		if spec == "" {
			model.CreateDraftModelIDs = nil
			model.CreateDraftModelError = ""
			return nil
		}
		model.CreateDraftModelIDs = nil
		model.CreateDraftModelError = ""
		return []update.Effect{LoadCreateDraftModelCatalogEffect{
			ProviderSpec:  spec,
			BaseURL:       strings.TrimSpace(value.BaseURL),
			CredentialRef: strings.TrimSpace(value.CredentialRef),
			ProtocolKind:  strings.TrimSpace(value.ProtocolKind),
		}}
	case CreateDraftModelCatalogLoaded:
		if !matchesCreateDraftModelCatalogLoad(model, strings.TrimSpace(value.ProviderSpec), strings.TrimSpace(value.BaseURL), strings.TrimSpace(value.CredentialRef)) {
			return nil
		}
		model.CreateDraftModelIDs = append([]string(nil), value.ModelIDs...)
		model.CreateDraftModelError = strings.TrimSpace(value.Error)
		return nil
	case FocusNextAfterRebuildRequested:
		return []update.Effect{FocusNextAfterRebuildEffect{Delay: 2 * time.Millisecond}}
	case EndpointCopyRequested:
		return []update.Effect{CopyEndpointValueEffect(value)}
	case ClientBaseURLCopyRequested:
		return []update.Effect{CopyClientBaseURLEffect(value)}
	case ClientLaunchRequested:
		model.HeaderStatus = "running…"
		model.InteractionMode = InteractionModeBusyLaunch
		return []update.Effect{LaunchClientEffect(value)}
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
		return []update.Effect{OpenSupportLinkEffect{
			Label: strings.TrimSpace(value.Label),
			URL:   strings.TrimSpace(value.URL),
		}}, true
	case HelpDiagnosticsCopyRequested:
		return []update.Effect{CopyHelpDiagnosticsEffect{Text: strings.TrimSpace(value.Text)}}, true
	case CompatibilityRestartRequested:
		if model.ControlPlane == nil {
			return nil, true
		}
		return []update.Effect{CompatibilityRestartHintEffect{
			Command: strings.TrimSpace(model.ControlPlane.RecoveryCommand),
		}}, true
	case CompatibilityDiagnosticsCopyRequested:
		if model.ControlPlane == nil {
			return nil, true
		}
		return []update.Effect{CopyCompatibilityDiagnosticsEffect{
			Text: compatibilityDiagnostics(*model.ControlPlane),
		}}, true
	case CompatibilityRecoveryNoted:
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

func matchesCreateDraftModelCatalogLoad(model *Model, providerSpec, baseURL, credentialRef string) bool {
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
}

func refreshStatusProjectionEffectFor(model *Model) RefreshStatusProjectionEffect {
	if model == nil {
		return RefreshStatusProjectionEffect{}
	}
	return RefreshStatusProjectionEffect{EndpointName: strings.TrimSpace(model.CurrentEndpoint)}
}
