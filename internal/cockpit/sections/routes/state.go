package routes

import (
	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
)

// RouteSectionState stores the mutable route projection shown by SectionView.
// SectionView owns the workflow behavior and mutates this projection after
// route and target commands complete.
type RouteSectionState struct {
	Routes              []readmodel.RouteReadModel
	ExpandedRoute       *tui.State[readmodel.RouteID]
	OpenTarget          *tui.State[readmodel.TargetID]
	AddTargetRoute      *tui.State[readmodel.RouteID]
	DeleteConfirmTarget *tui.State[readmodel.TargetID]
	FocusRoute          *tui.State[readmodel.RouteID]
	ShareExpiryRoute    *tui.State[readmodel.RouteID]
	SharePendingRoute   *tui.State[readmodel.RouteID]
	ShareCopiedRoute    *tui.State[readmodel.RouteID]
}

func NewRouteSectionState(routes []readmodel.RouteReadModel) *RouteSectionState {
	return &RouteSectionState{
		Routes:              append([]readmodel.RouteReadModel(nil), routes...),
		ExpandedRoute:       tui.NewState(readmodel.RouteID("")),
		OpenTarget:          tui.NewState(readmodel.TargetID("")),
		AddTargetRoute:      tui.NewState(readmodel.RouteID("")),
		DeleteConfirmTarget: tui.NewState(readmodel.TargetID("")),
		FocusRoute:          tui.NewState(readmodel.RouteID("")),
		ShareExpiryRoute:    tui.NewState(readmodel.RouteID("")),
		SharePendingRoute:   tui.NewState(readmodel.RouteID("")),
		ShareCopiedRoute:    tui.NewState(readmodel.RouteID("")),
	}
}
