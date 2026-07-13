package routing

import (
	"github.com/swobuforge/swobu/internal/domain/endpointintent"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/views"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
	"github.com/swobuforge/swobu/internal/terminalui/view/retained"
)

func createDraftProtocolModeRow(model state.Model) retained.ViewSpec[state.Model] {
	spec := model.CreateDraftProviderConfig.ProviderSpec
	if spec == "" {
		return views.RowStatic("protocol", views.ValueRequired)
	}
	deployment, _ := deploymentForModelID(model.CreateDraftModelDeployments, model.CreateDraftProviderConfig.ModelID)
	protocols := deploymentProtocolOptions(deployment, spec)
	if len(protocols) == 0 {
		return views.RowStatic("protocol", views.ValueRequired)
	}
	current := deploymentSelectedProtocol(deployment, spec, model.CreateDraftProviderConfig.ProviderProtocol)
	if current == "" {
		current = views.ValueRequired
	}
	return views.RowActionWithHooks("protocol", current, "next", func() []update.Action {
		next := nextProviderProtocolSelection(protocols, current)
		return []update.Action{
			state.SetCreateDraftProviderProtocol{ProviderProtocol: next},
		}
	}, nil, views.FocusAffordance("next", false))
}

func createDraftTestOrCreateRow(model state.Model) retained.ViewSpec[state.Model] {
	flow := state.EvaluateCreateDraftRouteSetup(model.CreateDraftProviderConfig)
	if !flow.Ready {
		return views.RowKVWithHooks("create", views.ValueBlocked, "", nil, nil, views.FocusAffordance("", false))
	}
	name := model.CreateDraftName
	parsed, err := endpointintent.ParseEndpointName(name)
	if err != nil {
		return views.RowKVWithHooks("create", views.ValueBlocked, "", nil, nil, views.FocusAffordance("", false))
	}
	if model.CreateDraftProviderConfig.ModelID == "" {
		return views.RowKVWithHooks("create", views.ValueRequired, "", nil, nil, views.FocusAffordance("", false))
	}
	canonicalName := parsed.String()
	return views.RowActionWithHooks("create", "ready", "create", func() []update.Action {
		return []update.Action{
			state.SetCreateDraftName{Name: canonicalName},
			state.WorkspaceCreateRequested{Name: canonicalName},
		}
	}, nil, views.FocusAffordance("create", false))
}
