package target_config

import "github.com/swobuforge/swobu/internal/cockpit/readmodel"

// SelectPlacement commits the routing choice selected by the placement picker.
func (w *TargetConfig) SelectPlacement(p readmodel.PlacementOptionReadModel) {
	w.Placement.Set(p)
	w.Phase.Set(PhaseReadyToCreate)
	w.CommitEdit(w.actionContext())
}
