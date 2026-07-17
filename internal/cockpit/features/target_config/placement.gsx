package target_config

import (
	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/ui"
)

func PlacementSelect(w *TargetConfig) *ui.Select {
	return ui.NewSelect(ui.SelectProps{
		ID: TargetAddMountKey(w, "placement-display"), Label: "routing", Value: w.Placement.Get().Summary(), Action: "change ↵",
		CanEnter: func() bool { return w.Phase.Get() == PhaseReadyToCreate },
		Body: func(backout func()) tui.Component { return PlacementPicker(w, backout) },
	})
}

templ FixedPlacementControl(w *TargetConfig) {
	<div class="flex-row w-full">
		<span class="w-2">  </span>
		<span class="w-18">routing</span>
		<span flexGrow={1.0}>{w.Placement.Get().Summary()}</span>
	</div>
}

func PlacementPicker(w *TargetConfig, backout func()) *ui.SearchPicker {
	opts := w.placementOptions()
	items := make([]ui.SearchOption, 0, len(opts))
	for _, opt := range opts { items = append(items, ui.SearchOption{ID: placementOptionID(opt), Label: opt.Summary()}) }
	picker := ui.NewSearchPicker(TargetAddMountKey(w, "placement-picker"), "routing", items, func(sel ui.Selection) {
		for _, opt := range opts {
			if placementOptionID(opt) == sel.Value { w.SelectPlacement(opt); break }
		}
		if backout != nil { backout() }
	}, func() { if backout != nil { backout() } })
	picker.AutoFocus = true
	return picker
}
