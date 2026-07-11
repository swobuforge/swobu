package routing

import (
	"strings"

	"github.com/swobuforge/swobu/internal/domain/endpointintent"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/views"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
	"github.com/swobuforge/swobu/internal/terminalui/view/retained"
)

const providerProtocolRowLabel = "protocol"

type providerProtocolChoiceRowSpec struct {
	ProviderConfig *state.ProviderConfigSnapshot
	EndpointName   string
	CreateMode     bool
}

func providerProtocolChoiceRow(spec providerProtocolChoiceRowSpec) retained.ViewSpec[state.Model] {
	return retained.Build[state.Model](func(ctx *retained.Context[state.Model]) retained.ViewSpec[state.Model] {
		return buildProviderProtocolChoiceRow(ctx.Model(), spec)
	})
}

func buildProviderProtocolChoiceRow(model state.Model, spec providerProtocolChoiceRowSpec) retained.ViewSpec[state.Model] {
	if spec.ProviderConfig == nil {
		return views.RowStatic(providerProtocolRowLabel, views.ValueRequired)
	}
	protocols := profile.SupportedProviderProtocolsForSpec(spec.ProviderConfig.ProviderSpec)
	if len(protocols) == 0 {
		return views.RowStatic(providerProtocolRowLabel, views.ValueRequired)
	}
	current := strings.TrimSpace(spec.ProviderConfig.ProviderProtocol) // swobu:io-string source=boundary
	if current == "" {
		current = defaultProviderProtocolForProvider(spec.ProviderConfig.ProviderSpec)
	}
	return views.RowActionWithHooks(providerProtocolRowLabel, current, "next", func() []update.Action {
		if spec.CreateMode {
			if actions, ok := firstRunCreateFromProtocolActions(model); ok {
				return actions
			}
		}
		next := nextProviderProtocolSelection(protocols, current)
		if next == "" {
			return nil
		}
		if spec.CreateMode {
			return []update.Action{state.SetCreateDraftProviderProtocol{ProviderProtocol: next}}
		}
		if strings.TrimSpace(spec.EndpointName) == "" { // swobu:io-string source=boundary
			return nil
		}
		updated := *spec.ProviderConfig
		updated.ProviderProtocol = next
		return routingSaveProviderConfigActions(strings.TrimSpace(spec.EndpointName), updated, "provider/protocol") // swobu:io-string source=boundary
	}, nil, views.FocusAffordance("next", false))
}

func firstRunCreateFromProtocolActions(model state.Model) ([]update.Action, bool) {
	name := model.CreateDraftName
	if name == "" {
		return nil, false
	}
	parsed, err := endpointintent.ParseEndpointName(name)
	if err != nil {
		return nil, false
	}
	flow := state.EvaluateCreateDraftRouteSetup(model.CreateDraftProviderConfig)
	if !flow.Ready {
		return nil, false
	}
	canonicalName := parsed.String()
	return []update.Action{
		state.SetCreateDraftName{Name: canonicalName},
		state.WorkspaceCreateRequested{Name: canonicalName},
	}, true
}

func nextProviderProtocolSelection(protocols []string, current string) string {
	if len(protocols) == 0 {
		return ""
	}
	current = strings.TrimSpace(current) // swobu:io-string source=boundary
	for i, protocol := range protocols {
		if protocol != current {
			continue
		}
		return protocols[(i+1)%len(protocols)]
	}
	return protocols[0]
}
