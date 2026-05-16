package routing

import (
	"strconv"
	"strings"

	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/views"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
	"github.com/swobuforge/swobu/internal/terminalui/view/retained"
)

func providerSpecRow(providerConfig *state.ProviderConfigSnapshot) retained.ViewSpec[state.Model] {
	if providerConfig == nil {
		return views.RowStatic("provider", "not set")
	}
	return views.RowStatic("provider", providerDisplayName(strings.TrimSpace(providerConfig.ProviderSpec))) // trimlowerlint:allow boundary canonicalization
}

func providerDeleteRow(endpointName string, providerConfig *state.ProviderConfigSnapshot) retained.ViewSpec[state.Model] {
	if providerConfig == nil {
		return views.RowStatic("delete model", "")
	}
	return retained.Build[state.Model](func(ctx *retained.Context[state.Model]) retained.ViewSpec[state.Model] {
		model := ctx.Model()
		snapshot := currentSnapshotByName(model, endpointName)
		if snapshot == nil || len(snapshot.ProviderConfigs) <= 1 {
			return views.RowStatic("delete model", "disabled (last model)")
		}
		return views.RowAction("delete model", "", "delete", func() []update.Action {
			return routingDeleteProviderConfigActions(strings.TrimSpace(endpointName), strings.TrimSpace(providerConfig.Ref), "provider/delete") // trimlowerlint:allow boundary canonicalization
		})
	})
}

func currentSnapshotByName(model state.Model, endpointName string) *state.EndpointSnapshot {
	name := strings.TrimSpace(endpointName) // trimlowerlint:allow boundary canonicalization
	for i := range model.EndpointSnapshots {
		if strings.TrimSpace(model.EndpointSnapshots[i].Name) == name { // trimlowerlint:allow boundary canonicalization
			return &model.EndpointSnapshots[i]
		}
	}
	return nil
}

func nextProviderDraftKey(snapshot *state.EndpointSnapshot) string {
	count := 0
	if snapshot != nil {
		count = len(snapshot.ProviderConfigs)
	}
	return "draft-" + strings.TrimSpace(strconv.Itoa(count+1)) // trimlowerlint:allow boundary canonicalization
}
