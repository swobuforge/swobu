package routes

import (
	"fmt"

	tui "github.com/grindlemire/go-tui"
	route_add "github.com/swobuforge/swobu/internal/cockpit/features/route_add"
	route_edit "github.com/swobuforge/swobu/internal/cockpit/features/route_edit"
	target_edit "github.com/swobuforge/swobu/internal/cockpit/features/target_edit"
	"github.com/swobuforge/swobu/internal/cockpit/ports"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/cockpit/ui"
)

// SectionView owns the mutable route section workflow and renders the route
// list. RouteSectionState stays data-only; the section owns all behavior.
type SectionView struct {
	Model         readmodel.WorkspaceReadModel
	Expanded      *tui.State[bool]
	State         *RouteSectionState
	RouteDraft    *route_add.Draft
	RouteEditors  map[readmodel.RouteID]*route_edit.Workflow
	TargetEditors map[string]*target_edit.Workflow
	SaveRoute     route_edit.SaveFunc
	DeleteRoute   route_edit.DeleteFunc
	SaveTarget    target_edit.SaveFunc
	DeleteTarget  target_edit.DeleteFunc
}

// SectionDraftRouteRowView renders the inline draft route input row.
type SectionDraftRouteRowView struct {
	ModelName *tui.State[string]
	Submit    func(string)
}

func Section(model readmodel.WorkspaceReadModel, commands ports.RouteCommands) *SectionView {
	draft := route_add.NewDraft()
	section := &SectionView{
		Model:         model,
		Expanded:      tui.NewState(true),
		State:         NewRouteSectionState(model.Routes),
		RouteDraft:    draft,
		RouteEditors:  make(map[readmodel.RouteID]*route_edit.Workflow),
		TargetEditors: make(map[string]*target_edit.Workflow),
	}
	if commands != nil {
		section.SaveRoute = commands.SaveRoute
		section.DeleteRoute = commands.DeleteRoute
		section.SaveTarget = commands.SaveTarget
		section.DeleteTarget = commands.DeleteTarget
	}
	return section
}

func (s *SectionView) isExpanded(route readmodel.RouteReadModel) bool {
	return s.State.ExpandedRoute.Get() == route.ID
}

func (s *SectionView) toggleRoute(route readmodel.RouteReadModel) {
	if s.isExpanded(route) {
		s.State.ExpandedRoute.Set("")
		return
	}
	s.OpenRoute(route)
}

func (s *SectionView) OpenRoute(route readmodel.RouteReadModel) {
	s.State.ExpandedRoute.Set(route.ID)
	s.ensureRouteEditor(route)
}

func (s *SectionView) expandedRoute() (readmodel.RouteReadModel, bool) {
	for _, route := range s.State.Routes {
		if route.ID == s.State.ExpandedRoute.Get() {
			return route, true
		}
	}
	return readmodel.RouteReadModel{}, false
}

func (s *SectionView) addRoute() {
	s.RouteDraft.OpenFor(s.State.Routes)
	s.State.OpenTarget.Set("")
	s.State.AddTargetRoute.Set("")
}

func (s *SectionView) createDraftRoute() {
	route := s.RouteDraft.Route(s.State.Routes)
	s.applyRouteUpsert(route)
	s.OpenRoute(route)
	s.State.OpenTarget.Set("")
	s.State.AddTargetRoute.Set(route.ID)
	s.TargetEditors[s.targetWorkflowKey(route, "")] = s.newTargetCreateWorkflow(route)
	s.RouteDraft.Close()
}

// The input owns submission; this callback bridges the mounted input back to
// the section-owned draft route lifecycle.
func (s *SectionView) createDraftRouteFromInput(modelName string) {
	s.RouteDraft.ModelName.Set(modelName)
	s.createDraftRoute()
}

func (s *SectionView) saveRoute(previousID readmodel.RouteID, route readmodel.RouteReadModel) {
	s.applyRouteSaved(previousID, route)
	if route.ID != previousID {
		if workflow := s.RouteEditors[previousID]; workflow != nil {
			delete(s.RouteEditors, previousID)
			s.RouteEditors[route.ID] = workflow
		}
	}
}

func (s *SectionView) deleteRoute(routeID readmodel.RouteID) {
	s.applyRouteDeleted(routeID)
	delete(s.RouteEditors, routeID)
}

func (s *SectionView) OpenTargetEditor(route readmodel.RouteReadModel, target readmodel.TargetReadModel) {
	s.State.ExpandedRoute.Set(route.ID)
	s.State.OpenTarget.Set(target.ID)
	s.State.AddTargetRoute.Set("")
	s.TargetEditors[s.targetWorkflowKey(route, target.ID)] = s.newTargetEditWorkflow(route, target)
}

func (s *SectionView) openTarget(target readmodel.TargetReadModel) {
	route, ok := s.expandedRoute()
	if !ok {
		return
	}
	s.OpenTargetEditor(route, target)
}

func (s *SectionView) addTarget(route readmodel.RouteReadModel) {
	s.State.AddTargetRoute.Set(route.ID)
	s.State.OpenTarget.Set("")
	s.TargetEditors[s.targetWorkflowKey(route, "")] = s.newTargetCreateWorkflow(route)
}

func (s *SectionView) saveTarget(routeID readmodel.RouteID, target readmodel.TargetReadModel) {
	s.applyTargetSaved(routeID, target)
}

func (s *SectionView) deleteTarget(routeID readmodel.RouteID, targetID readmodel.TargetID) {
	s.applyTargetDeleted(routeID, targetID)
}

func (s *SectionView) Back() bool {
	if s.State.OpenTarget.Get() != "" {
		s.State.OpenTarget.Set("")
		return true
	}
	if s.State.AddTargetRoute.Get() != "" {
		s.State.AddTargetRoute.Set("")
		return true
	}
	if s.RouteDraft.Back() {
		return true
	}
	if s.State.ExpandedRoute.Get() != "" {
		s.State.ExpandedRoute.Set("")
		return true
	}
	return false
}

func (s *SectionView) targetWorkflowKey(route readmodel.RouteReadModel, targetID readmodel.TargetID) string {
	return "target-edit:" + string(s.Model.ID) + ":" + string(route.ID) + ":" + string(targetID)
}

func (s *SectionView) routeWorkflowKey(route readmodel.RouteReadModel) string {
	return "route-edit:" + string(s.Model.ID) + ":" + string(route.ID)
}

func (s *SectionView) routeEditor(route readmodel.RouteReadModel) *route_edit.Workflow {
	workflow := s.RouteEditors[route.ID]
	if workflow == nil {
		return nil
	}
	workflow.UpdateProps(s.newRouteEditWorkflow(route))
	return workflow
}

func (s *SectionView) ensureRouteEditor(route readmodel.RouteReadModel) *route_edit.Workflow {
	if workflow := s.RouteEditors[route.ID]; workflow != nil {
		workflow.UpdateProps(s.newRouteEditWorkflow(route))
		return workflow
	}
	workflow := s.newRouteEditWorkflow(route)
	s.RouteEditors[route.ID] = workflow
	return workflow
}

func (s *SectionView) newRouteEditWorkflow(route readmodel.RouteReadModel) *route_edit.Workflow {
	return route_edit.NewWorkflow(
		s.Model.ID,
		route,
		s.SaveRoute,
		s.DeleteRoute,
		func(saved readmodel.RouteReadModel) { s.saveRoute(route.ID, saved) },
		s.deleteRoute,
		nil,
	)
}

func (s *SectionView) targetEditor(route readmodel.RouteReadModel, target readmodel.TargetReadModel) *target_edit.Workflow {
	key := s.targetWorkflowKey(route, target.ID)
	workflow := s.TargetEditors[key]
	if workflow == nil {
		return nil
	}
	workflow.UpdateProps(s.newTargetEditWorkflow(route, target))
	return workflow
}

func (s *SectionView) newTargetEditWorkflow(route readmodel.RouteReadModel, target readmodel.TargetReadModel) *target_edit.Workflow {
	return target_edit.NewEditWorkflow(
		s.Model.ID,
		route,
		target,
		s.SaveTarget,
		s.DeleteTarget,
		func(saved readmodel.TargetReadModel) { s.saveTarget(route.ID, saved) },
		func(targetID readmodel.TargetID) { s.deleteTarget(route.ID, targetID) },
		func() { s.State.OpenTarget.Set("") },
	)
}

func (s *SectionView) targetCreator(route readmodel.RouteReadModel) *target_edit.Workflow {
	key := s.targetWorkflowKey(route, "")
	workflow := s.TargetEditors[key]
	if workflow == nil {
		return nil
	}
	workflow.UpdateProps(s.newTargetCreateWorkflow(route))
	return workflow
}

func (s *SectionView) newTargetCreateWorkflow(route readmodel.RouteReadModel) *target_edit.Workflow {
	return target_edit.NewCreateWorkflow(
		s.Model.ID,
		route,
		s.SaveTarget,
		s.DeleteTarget,
		func(saved readmodel.TargetReadModel) { s.saveTarget(route.ID, saved) },
		func() { s.State.AddTargetRoute.Set("") },
	)
}

func (s *SectionView) applyRouteUpsert(route readmodel.RouteReadModel) {
	for i, existing := range s.State.Routes {
		if existing.ID != route.ID {
			continue
		}
		s.State.Routes[i] = route
		return
	}
	s.State.Routes = append(s.State.Routes, route)
}

func (s *SectionView) applyRouteSaved(previousID readmodel.RouteID, route readmodel.RouteReadModel) {
	for i, existing := range s.State.Routes {
		if existing.ID != previousID {
			continue
		}
		s.State.Routes[i] = route
		if route.Default {
			s.markOnlyDefault(route.ID)
		}
		if s.State.ExpandedRoute.Get() == previousID {
			s.State.ExpandedRoute.Set(route.ID)
		}
		return
	}
}

func (s *SectionView) applyRouteDeleted(routeID readmodel.RouteID) {
	next := make([]readmodel.RouteReadModel, 0, len(s.State.Routes))
	for _, route := range s.State.Routes {
		if route.ID == routeID {
			continue
		}
		next = append(next, route)
	}
	s.State.Routes = next
	if s.State.ExpandedRoute.Get() == routeID {
		s.State.ExpandedRoute.Set("")
	}
	s.State.OpenTarget.Set("")
	s.State.AddTargetRoute.Set("")
}

func (s *SectionView) applyTargetSaved(routeID readmodel.RouteID, target readmodel.TargetReadModel) {
	for i, route := range s.State.Routes {
		if route.ID != routeID {
			continue
		}
		for j, existing := range route.Targets {
			if existing.ID == target.ID {
				s.State.Routes[i].Targets[j] = target
				s.State.OpenTarget.Set("")
				return
			}
		}
		s.State.Routes[i].Targets = append(s.State.Routes[i].Targets, target)
		s.State.OpenTarget.Set("")
		s.State.AddTargetRoute.Set("")
		return
	}
}

func (s *SectionView) applyTargetDeleted(routeID readmodel.RouteID, targetID readmodel.TargetID) {
	for i, route := range s.State.Routes {
		if route.ID != routeID {
			continue
		}
		next := make([]readmodel.TargetReadModel, 0, len(route.Targets))
		for _, target := range route.Targets {
			if target.ID == targetID {
				continue
			}
			next = append(next, target)
		}
		s.State.Routes[i].Targets = next
		s.State.OpenTarget.Set("")
		s.State.AddTargetRoute.Set("")
		return
	}
}

func (s *SectionView) markOnlyDefault(routeID readmodel.RouteID) {
	for i := range s.State.Routes {
		s.State.Routes[i].Default = s.State.Routes[i].ID == routeID
	}
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

func targetValue(target readmodel.TargetReadModel) string {
	if target.Provider == "" {
		return target.Model
	}
	if target.Model == "" {
		return target.Provider
	}
	return target.Provider + "/" + target.Model
}

func routeMountKey(route readmodel.RouteReadModel) string { return "route:" + string(route.ID) }

func targetMountKey(route readmodel.RouteReadModel, target readmodel.TargetReadModel) string {
	return "target:" + string(route.ID) + ":" + string(target.ID)
}

func addTargetMountKey(route readmodel.RouteReadModel) string {
	return "add-target:" + string(route.ID)
}
func addRouteMountKey() string { return "add-route" }

// RouteRowComponent mounts a selectable route disclosure row.
func RouteRowComponent(s *SectionView, route readmodel.RouteReadModel) *ui.SelectableRow {
	return ui.NewSelectableRow(
		routeMountKey(route),
		route.ModelName,
		route.RowValue(),
		routeActionLabel(s.isExpanded(route)),
		func() { s.toggleRoute(route) },
	)
}

// TargetRowComponent mounts a selectable target row.
func TargetRowComponent(s *SectionView, route readmodel.RouteReadModel, target readmodel.TargetReadModel) *ui.SelectableRow {
	return ui.NewSelectableRow(
		targetMountKey(route, target),
		"target "+targetRankLabel(target),
		targetValue(target),
		"open ↵",
		func() { s.openTarget(target) },
	)
}

// AddTargetRowComponent mounts an "add target" selectable row.
func AddTargetRowComponent(s *SectionView, route readmodel.RouteReadModel) *ui.SelectableRow {
	return ui.NewSelectableRow(
		addTargetMountKey(route),
		"add target",
		"",
		"add ↵",
		func() { s.addTarget(route) },
	)
}

// AddRouteRowComponent mounts an "add route" selectable row.
func AddRouteRowComponent(s *SectionView) *ui.SelectableRow {
	return ui.NewSelectableRow(
		addRouteMountKey(),
		"add route",
		"",
		"add ↵",
		func() { s.addRoute() },
	)
}

// SectionDraftRouteRow mounts the inline draft route input row.
func SectionDraftRouteRow(s *SectionView) *SectionDraftRouteRowView {
	return &SectionDraftRouteRowView{
		ModelName: s.RouteDraft.ModelName,
		Submit:    s.createDraftRouteFromInput,
	}
}
