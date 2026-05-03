package routing

import (
	"fmt"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/providercatalog"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/views"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/view"
)

func providerSpecRow(providerConfig *state.ProviderConfigSnapshot) view.ViewSpec[state.Model] {
	if providerConfig == nil {
		return views.RowStatic("provider", "not set")
	}
	return views.RowStatic("provider", providercatalog.DisplayName(strings.TrimSpace(providerConfig.ProviderSpec)))
}

func providerDeleteRow(endpointName string, providerConfig *state.ProviderConfigSnapshot) view.ViewSpec[state.Model] {
	if providerConfig == nil {
		return views.RowStatic("delete model", "")
	}
	return view.Build[state.Model](func(ctx *view.Context[state.Model]) view.ViewSpec[state.Model] {
		model := ctx.Model()
		snapshot := currentSnapshotByName(model, endpointName)
		if snapshot == nil || len(snapshot.ProviderConfigs) <= 1 {
			return views.RowStatic("delete model", "disabled (last model)")
		}
		return views.RowAction("delete model", "", "delete", func() []update.Action {
			return []update.Action{
				state.RoutingSaveStartedAction{},
				state.DeleteProviderConfigRequested{
					EndpointName: strings.TrimSpace(endpointName),
					ProviderRef:  strings.TrimSpace(providerConfig.Ref),
				},
			}
		})
	})
}

func currentSnapshotByName(model state.Model, endpointName string) *state.EndpointSnapshot {
	name := strings.TrimSpace(endpointName)
	for i := range model.EndpointSnapshots {
		if strings.TrimSpace(model.EndpointSnapshots[i].Name) == name {
			return &model.EndpointSnapshots[i]
		}
	}
	return nil
}

func nextProviderRef(snapshot *state.EndpointSnapshot) string {
	if snapshot == nil {
		return "model-1"
	}
	used := map[string]struct{}{}
	for _, pc := range snapshot.ProviderConfigs {
		used[strings.TrimSpace(pc.Ref)] = struct{}{}
	}
	for i := 1; i < 1000; i++ {
		ref := fmt.Sprintf("model-%d", i)
		if _, exists := used[ref]; !exists {
			return ref
		}
	}
	return fmt.Sprintf("model-%d", len(snapshot.ProviderConfigs)+1)
}
