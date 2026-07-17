package target_config

import (
	"fmt"
	"sort"

	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
)

func defaultPlacementForRoute(route readmodel.RouteReadModel) readmodel.PlacementOptionReadModel {
	maxRank := 0
	for _, t := range route.Targets {
		if t.Rank > maxRank {
			maxRank = t.Rank
		}
	}
	if maxRank == 0 {
		return readmodel.PlacementOptionReadModel{
			Label:  "step 1",
			Rank:   1,
			Weight: 1,
			Kind:   readmodel.PlacementFallback,
		}
	}
	return readmodel.PlacementOptionReadModel{
		Label:  fallbackPlacementLabel(maxRank),
		Rank:   maxRank + 1,
		Weight: 1,
		Kind:   readmodel.PlacementFallback,
	}
}

func placementForTarget(target readmodel.TargetReadModel) readmodel.PlacementOptionReadModel {
	rank := target.Rank
	if rank <= 0 {
		rank = 1
	}
	weight := target.Weight
	if weight <= 0 {
		weight = 1
	}
	return readmodel.PlacementOptionReadModel{
		Label:  fmt.Sprintf("step %d", rank),
		Rank:   rank,
		Weight: weight,
		Kind:   readmodel.PlacementFallback,
	}
}

// placementOptions projects the placement choices for the current route into a
// fresh slice each call (no feature-level cache): one "balance with step N" row
// per existing step, plus a single "fallback after step N" row for the new
// target. It is pure projection — the ui.Select body (PlacementPicker) is built
// fresh per render, so stable mount keys preserve local picker state instead.
func (w *TargetConfig) placementOptions() []readmodel.PlacementOptionReadModel {
	ranks := make([]int, 0, len(w.Route.Targets))
	seen := make(map[int]struct{})
	maxRank := 0
	for _, t := range w.Route.Targets {
		if w.mode == targetConfigModeEdit && t.ID == w.Target.ID {
			continue
		}
		if t.Rank > maxRank {
			maxRank = t.Rank
		}
		if _, ok := seen[t.Rank]; ok {
			continue
		}
		seen[t.Rank] = struct{}{}
		ranks = append(ranks, t.Rank)
	}
	sort.Ints(ranks)

	opts := make([]readmodel.PlacementOptionReadModel, 0, len(ranks)+1)
	for _, rank := range ranks {
		opts = append(opts, readmodel.PlacementOptionReadModel{
			Label:  fmt.Sprintf("balance with step %d", rank),
			Rank:   rank,
			Weight: 1,
			Kind:   readmodel.PlacementBalance,
		})
	}
	opts = append(opts, readmodel.PlacementOptionReadModel{
		Label:  fallbackPlacementLabel(maxRank),
		Rank:   maxRank + 1,
		Weight: 1,
		Kind:   readmodel.PlacementFallback,
	})
	return opts
}

func fallbackPlacementLabel(maxRank int) string {
	if maxRank <= 0 {
		return "fallback after last step"
	}
	return fmt.Sprintf("fallback after step %d", maxRank)
}

func (w *TargetConfig) routeHasTargets() bool {
	return len(w.Route.Targets) > 0
}

func (w *TargetConfig) CanChangePlacement() bool {
	if w.mode == targetConfigModeEdit {
		return len(w.Route.Targets) > 1
	}
	return w.routeHasTargets()
}

func placementOptionID(opt readmodel.PlacementOptionReadModel) string {
	return fmt.Sprintf("placement-%d-%d", opt.Rank, opt.Kind)
}
