package routes

import (
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/cockpit/ui"
)

// TargetDeleteConfirmRow is a transient inline confirmation shown beneath a
// target row when the operator chooses delete. It supports:
//   - activate (Enter) to confirm deletion and call DeleteTarget port
//   - Escape to cancel
//   - automatic collapse when focus leaves the route
//
// The component mounts at a unique key derived from the target ID so it is
// focusable and participates in tab order.
func TargetDeleteConfirmRow(section *SectionView, route readmodel.RouteReadModel, target readmodel.TargetReadModel) *ui.SelectableRow {
	row := ui.NewSelectableRow(
		"confirm-del:"+string(route.ID)+":"+string(target.ID),
		"delete "+targetValue(target),
		"",
		"confirm ↵",
		func() {
			section.deleteTargetAndClose(route.ID, target.ID)
		},
	)
	row.OnEscape = section.closeDeleteTargetConfirm
	return row
}
