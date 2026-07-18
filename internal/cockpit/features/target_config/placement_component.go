package target_config

import (
	"fmt"

	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
)

func defaultPlacementForRoute(route readmodel.RouteReadModel) readmodel.PlacementOptionReadModel {
	var anchor readmodel.TargetID
	if len(route.Tiers) > 0 && len(route.Tiers[len(route.Tiers)-1].Targets) > 0 {
		anchor = route.Tiers[len(route.Tiers)-1].Targets[0].ID
	}
	return readmodel.PlacementOptionReadModel{Label: "new fallback tier", PeerTargetID: anchor, Kind: readmodel.PlacementFallback}
}

func (w *TargetConfig) placementOptions() []readmodel.PlacementOptionReadModel {
	opts := make([]readmodel.PlacementOptionReadModel, 0, len(w.Route.Tiers)+1)
	for tierIndex, tier := range w.Route.Tiers {
		for _, target := range tier.Targets {
			if w.mode == targetConfigModeEdit && target.ID == w.Target.ID {
				continue
			}
			opts = append(opts, readmodel.PlacementOptionReadModel{Label: "same as " + tierLabel(tierIndex), PeerTargetID: target.ID, Kind: readmodel.PlacementBalance})
			break
		}
	}
	opts = append(opts, defaultPlacementForRoute(w.Route))
	return opts
}
func tierLabel(index int) string {
	if index == 0 {
		return "primary"
	}
	return fmt.Sprintf("fallback %d", index)
}
func (w *TargetConfig) routeHasTargets() bool { return w.Route.TargetCount() > 0 }
func (w *TargetConfig) CanChangePlacement() bool {
	return w.mode == targetConfigModeCreate && w.routeHasTargets()
}
func placementOptionID(opt readmodel.PlacementOptionReadModel) string {
	return fmt.Sprintf("placement-%s-%d", opt.PeerTargetID, opt.Kind)
}
