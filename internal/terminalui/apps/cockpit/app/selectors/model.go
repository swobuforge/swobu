package selectors

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/swobuforge/swobu/internal/domain/endpointintent"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/apps/shared/daemonstate"
)

func HeaderStatus(model state.Model) string {
	if model.HeaderStatus == "" {
		return daemonstate.HeaderOfflineStale
	}
	return model.HeaderStatus
}

func InteractionMode(model state.Model) string {
	if model.InteractionMode == "" {
		return state.InteractionModeNAV
	}
	return model.InteractionMode
}

func HeaderHint(model state.Model) string {
	if model.DaemonHint != "" {
		return model.DaemonHint
	}
	return "127.0.0.1"
}

func HeaderShell(model state.Model) string {
	if model.ControlPlane != nil {
		return "incompatible   [ daemon mismatch ]"
	}
	rail := WorkspaceRail(model)
	status := HeaderStatus(model)
	if rail == "" {
		return status
	}
	if strings.TrimSpace(status) == "saved" { // trimlowerlint:allow boundary canonicalization
		return status + "   " + rail + "     "
	}
	return status + "   " + rail
}

func FooterHints(model state.Model) string {
	switch InteractionMode(model) {
	case state.InteractionModeEditText:
		return "↵ save   esc close   ? help"
	case state.InteractionModePickOne:
		return "↑↓ move   ↵ select   esc close   ? help"
	case state.InteractionModeManageList:
		return "↑↓ move   ↵ act   esc close   ? help"
	case state.InteractionModeBusyLaunch, state.InteractionModeBusySave:
		return "busy   ↑↓ move   ? help"
	default:
		verb := EmptyOr(model.FooterVerb, "act")
		out := "↑↓ move   ↵ " + verb + "   ? help   esc back"
		if model.FooterShowTabs {
			out += "   tab tabs"
		}
		if model.HeaderStatus == "saved" {
			out = "↑↓ move   ↵ copy   ? help   tab tabs"
		}
		if model.FooterAllowSpace {
			out += "   space toggle"
		}
		return out
	}
}

func DaemonValue(model state.Model) string {
	if model.DaemonState == "" {
		return daemonstate.DaemonStateUnreachable
	}
	if model.DaemonHint == "" || model.DaemonState == daemonstate.DaemonStateUp {
		return model.DaemonState
	}
	return model.DaemonState + " (" + model.DaemonHint + ")"
}

func ClientBaseURL(model state.Model) string {
	if model.CurrentEndpoint == "" {
		return "none"
	}
	current := CurrentEndpoint(model)
	if current == "" {
		return "none"
	}
	return "http://127.0.0.1:7926/c/" + current + "/"
}

func CurrentEndpoint(model state.Model) string {
	if model.CurrentEndpoint != "" {
		return model.CurrentEndpoint
	}
	return firstOrEmpty(model.Endpoints)
}

func CurrentCatalogEntry(model state.Model) *state.CatalogEntry {
	current := CurrentEndpoint(model)
	if current == "" {
		return nil
	}
	for i := range model.Catalog {
		if model.Catalog[i].EndpointName == current {
			return &model.Catalog[i]
		}
	}
	return nil
}

func CurrentEndpointSnapshot(model state.Model) *state.EndpointSnapshot {
	current := CurrentEndpoint(model)
	if current == "" {
		return nil
	}
	for i := range model.EndpointSnapshots {
		if model.EndpointSnapshots[i].Name == current {
			return &model.EndpointSnapshots[i]
		}
	}
	return nil
}

func CreateDraftName(model state.Model) string {
	return model.CreateDraftName
}

func CreateDraftNameDisplay(model state.Model) string {
	return EmptyOr(CreateDraftName(model), "choose a workspace name")
}

func CreateDraftProviderConfig(model state.Model) *state.ProviderConfigSnapshot {
	if model.CreateDraftProviderConfig.ProviderSpec == "" {
		return nil
	}
	return &model.CreateDraftProviderConfig
}

func CreateDraftProviderSummary(model state.Model) string {
	if CreateDraftProviderConfig(model) == nil {
		return "choose a provider"
	}
	return state.DraftProviderRef
}

func CreateDraftEndpointValue(model state.Model) string {
	name := deriveEndpointSlug(CreateDraftName(model))
	if name == "" {
		return "/c/<slug>/"
	}
	if _, err := endpointintent.ParseEndpointName(name); err != nil {
		return "invalid"
	}
	return "/c/" + name + "/"
}

func deriveEndpointSlug(raw string) string {
	value := strings.TrimSpace(strings.ToLower(raw)) // trimlowerlint:allow boundary canonicalization
	if value == "" {
		return ""
	}
	var b strings.Builder
	prevDash := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
			prevDash = false
		case r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		case r == '-' || unicode.IsSpace(r) || r == '_':
			if b.Len() == 0 || prevDash {
				continue
			}
			b.WriteRune('-')
			prevDash = true
		}
	}
	derived := strings.Trim(b.String(), "-")
	return derived
}

func SelectedProviderConfig(model state.Model, snapshot *state.EndpointSnapshot) *state.ProviderConfigSnapshot {
	if snapshot == nil {
		return nil
	}
	for i := range snapshot.ProviderConfigs {
		if snapshot.ProviderConfigs[i].Ref == snapshot.SelectedProviderConfigRef {
			return &snapshot.ProviderConfigs[i]
		}
	}
	return nil
}

func CurrentSelectedProviderConfig(model state.Model) *state.ProviderConfigSnapshot {
	return SelectedProviderConfig(model, CurrentEndpointSnapshot(model))
}

func EmptyOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func Field(entry *state.CatalogEntry, pick func(*state.CatalogEntry) string) string {
	if entry == nil {
		return "not selected"
	}
	return EmptyOr(pick(entry), "not set")
}

func CredentialSummary(entry *state.CatalogEntry) string {
	if entry == nil {
		return "not selected"
	}
	if entry.Error != "" {
		return "missing"
	}
	return "configured"
}

func CredentialSummaryFromProviderConfig(providerConfig *state.ProviderConfigSnapshot) string {
	if providerConfig == nil {
		return "not selected"
	}
	if providerConfig.CredentialRef == "" {
		return "missing"
	}
	return providerConfig.CredentialRef
}

func CatalogSummary(model state.Model) string {
	if model.CatalogError != "" {
		return "refresh failed"
	}
	if len(model.Catalog) == 0 {
		return "not loaded"
	}
	return EmptyOr(CurrentEndpoint(model), "loaded")
}

func WorkspaceRail(model state.Model) string {
	names := make([]string, 0, len(model.Endpoints)+1)
	if model.HelpTabOpen {
		names = append(names, "[› ?]")
	} else {
		names = append(names, "[ ? ]")
	}
	current := model.CurrentEndpoint
	for _, endpoint := range model.Endpoints {
		name := strings.TrimSpace(endpoint) // trimlowerlint:allow boundary canonicalization
		if name == "" {
			continue
		}
		if name == current && current != "" {
			names = append(names, "[› "+name+"]")
		} else {
			names = append(names, "[ "+name+" ]")
		}
	}
	if len(model.Endpoints) == 0 {
		names = append(names, "[ + new workspace ]")
	} else {
		if current == "" {
			names = append(names, "[› +]")
		} else {
			names = append(names, "[ + ]")
		}
	}
	return strings.Join(names, " ")
}

func StreamValue(model state.Model) bool {
	return model.StreamEnabled
}

func ClientAccessSummary(model state.Model) string {
	if model.ClientAccessStatus != "" {
		return model.ClientAccessStatus
	}
	return "not tested"
}

func TrafficSummary(model state.Model) string {
	if model.TrafficError != "" {
		return "refresh failed"
	}
	if len(model.TrafficRows) == 0 {
		return "no runtime evidence yet"
	}
	return fmt.Sprintf("%d recent", len(model.TrafficRows))
}

func ModelDisclosureLines(entry *state.CatalogEntry) []string {
	if entry == nil {
		return []string{"-> select one provider to inspect models"}
	}
	if entry.Error != "" {
		return []string{"-> " + entry.Error}
	}
	if len(entry.ModelIDs) == 0 {
		return []string{"-> no models reported"}
	}
	lines := make([]string, 0, len(entry.ModelIDs))
	for idx, modelID := range entry.ModelIDs {
		lines = append(lines, fmt.Sprintf("-> %d %s", idx+1, modelID))
	}
	return lines
}

func firstOrEmpty(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}
