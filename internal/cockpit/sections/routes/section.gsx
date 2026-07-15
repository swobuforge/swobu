package routes

import (
	"fmt"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
)

type SectionView struct {
	Model           readmodel.WorkspaceReadModel
	Expanded        *tui.State[bool]
	ExpandedRoute  *tui.State[readmodel.RouteID]
	OpenTarget     *tui.State[readmodel.TargetID]
	AddTargetRoute *tui.State[readmodel.RouteID]
}

func Section(model readmodel.WorkspaceReadModel) *SectionView {
	return &SectionView{
		Model:           model,
		Expanded:        tui.NewState(true),
		ExpandedRoute:   tui.NewState(readmodel.RouteID("")),
		OpenTarget:      tui.NewState(readmodel.TargetID("")),
		AddTargetRoute: tui.NewState(readmodel.RouteID("")),
	}
}

func (s *SectionView) isExpanded(route readmodel.RouteReadModel) bool {
	return s.ExpandedRoute.Get() == route.ID
}

func (s *SectionView) toggleRoute(route readmodel.RouteReadModel) {
	if s.isExpanded(route) {
		s.ExpandedRoute.Set("")
		return
	}
	s.ExpandedRoute.Set(route.ID)
}

func (s *SectionView) openTarget(target readmodel.TargetReadModel) {
	s.OpenTarget.Set(target.ID)
}

func (s *SectionView) addTarget(route readmodel.RouteReadModel) {
	s.AddTargetRoute.Set(route.ID)
}

func (s *SectionView) Back() bool {
	if s.OpenTarget.Get() != "" {
		s.OpenTarget.Set("")
		return true
	}
	if s.AddTargetRoute.Get() != "" {
		s.AddTargetRoute.Set("")
		return true
	}
	if s.ExpandedRoute.Get() != "" {
		s.ExpandedRoute.Set("")
		return true
	}
	return false
}

templ (s *SectionView) Render() {
	<div class="flex-col w-full">
		@SectionHeader("routes", s.Expanded.Get())
		if s.Expanded.Get() {
			if len(s.Model.Routes) == 0 {
				@InertRow("(no routes)", "", "")
			} else {
				for _, route := range s.Model.Routes {
					@FocusableRow(route.ModelName, route.RowValue(), routeActionLabel(s.isExpanded(route)), false, func() { s.toggleRoute(route) })
					if s.isExpanded(route) {
						for _, target := range route.Targets {
							@FocusableRow("target "+targetRankLabel(target), targetValue(target), "open ↵", false, func() { s.openTarget(target) })
						}
						@FocusableRow("add target", "", "add ↵", false, func() { s.addTarget(route) })
					}
				}
				@InertRow("add route", "", "add ↵")
			}
		}
	</div>
}

templ SectionHeader(label string, expanded bool) {
	<div class="flex-row">
		<span class="w-2"></span>
		if expanded {
			<span>{label + " ▾"}</span>
		} else {
			<span>{label + " ▸"}</span>
		}
	</div>
}

templ FocusableRow(label string, value string, action string, focused bool, activate func()) {
	<div class="flex-row w-full focusable" onActivate={activate}>
		<span class="w-5">{focusMarker(focused)}</span>
		<span class="w-18">{label}</span>
		<span class="w-36">{value}</span>
		<span>{action}</span>
	</div>
}

templ InertRow(label string, value string, action string) {
	<div class="flex-row w-full">
		<span class="w-5"></span>
		<span class="w-18">{label}</span>
		<span class="w-36">{value}</span>
		<span>{action}</span>
	</div>
}

func targetRankLabel(target readmodel.TargetReadModel) string {
	if target.Rank > 0 {
		return fmt.Sprint(target.Rank)
	}
	return string(target.ID)
}

func routeActionLabel(open bool) string {
	if open {
		return "collapse ↵"
	}
	return "expand ↵"
}

func focusMarker(focused bool) string {
	if focused {
		return ">"
	}
	return ""
}

func targetValue(target readmodel.TargetReadModel) string {
	if target.Provider == "" {
		return target.Model
	}
	if target.Model == "" {
		return target.Provider
	}
	return target.Provider + "/" + target.Model
}
