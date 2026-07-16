package routes

import (
	"context"
	"fmt"
	"strings"
	"time"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/features/route_add"
	"github.com/swobuforge/swobu/internal/cockpit/features/target_add"
	"github.com/swobuforge/swobu/internal/cockpit/ports"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/cockpit/ui"
)

// port function aliases so the section package no longer imports route_edit.
type SaveRouteFunc func(context.Context, ports.SaveRouteRequest) (readmodel.RouteReadModel, error)
type DeleteRouteFunc func(context.Context, ports.DeleteRouteRequest) error

// SectionView owns the mutable route section workflow and renders the route
// list. RouteSectionState stays data-only; the section owns all behavior.
type SectionView struct {
	Model                readmodel.WorkspaceReadModel
	Expanded             *tui.State[bool]
	State                *RouteSectionState
	RouteDraft           *route_add.Draft
	RouteDetailRows      map[readmodel.RouteID]*RouteDetailRow
	TargetStringRows     map[string]*TargetStringRow
	TargetAddWorkflows   map[readmodel.RouteID]*target_add.Workflow
	AddRouteRow          *ui.SelectableRow
	addRouteFocusPending bool
	SaveRoute            SaveRouteFunc
	DeleteRoute          DeleteRouteFunc
	SaveTarget           func(context.Context, ports.SaveTargetRequest) (readmodel.TargetReadModel, error)
	DeleteTarget         func(context.Context, ports.DeleteTargetRequest) error
	ListProviders        func(context.Context) ([]readmodel.ProviderOptionReadModel, error)
	TargetSetupQueries   ports.TargetSetupQueries
	TargetAuthCommands   ports.TargetAuthCommands
}

// SectionDraftRouteRowView renders the inline draft route input row.
type SectionDraftRouteRowView struct {
	ModelName *tui.State[string]
	Submit    func(string)
}

// Arrow returns the shared row marker for the active draft route row.
func (r *SectionDraftRouteRowView) Arrow() string {
	return ui.RowArrow(true)
}

func Section(model readmodel.WorkspaceReadModel, commands ports.RouteCommands) *SectionView {
	draft := route_add.NewDraft()
	section := &SectionView{
		Model:              model,
		Expanded:           tui.NewState(true),
		State:              NewRouteSectionState(model.Routes),
		RouteDraft:         draft,
		RouteDetailRows:    make(map[readmodel.RouteID]*RouteDetailRow),
		TargetStringRows:   make(map[string]*TargetStringRow),
		TargetAddWorkflows: make(map[readmodel.RouteID]*target_add.Workflow),
	}
	if commands != nil {
		section.SaveRoute = commands.SaveRoute
		section.DeleteRoute = commands.DeleteRoute
		section.SaveTarget = commands.SaveTarget
		section.DeleteTarget = commands.DeleteTarget
	}
	return section
}

// RequestAddRouteFocus seeds the add-route row until the mounted row owns the
// focus graph and can clear the seed itself.
func (s *SectionView) RequestAddRouteFocus() {
	s.addRouteFocusPending = true
	if s.AddRouteRow != nil {
		s.AddRouteRow.AutoFocus = true
	}
}

func (s *SectionView) KeyMap() tui.KeyMap {
	return tui.KeyMap{
		tui.OnStop(tui.KeyEnter, ui.ActivateFocusedElement),
		tui.OnStop(tui.Rune(' '), ui.ActivateFocusedElement),
	}
}

func (s *SectionView) UpdateProps(fresh tui.Component) {
	f, ok := fresh.(*SectionView)
	if !ok {
		return
	}
	oldID := s.Model.ID
	newID := f.Model.ID
	s.Model = f.Model
	s.SaveRoute = f.SaveRoute
	s.DeleteRoute = f.DeleteRoute
	s.SaveTarget = f.SaveTarget
	s.DeleteTarget = f.DeleteTarget
	s.ListProviders = f.ListProviders
	if s.State == nil {
		s.State = f.State
	} else if f.State != nil {
		s.State.Routes = append([]readmodel.RouteReadModel(nil), f.State.Routes...)
	}
	if s.State != nil && s.State.DeleteConfirmTarget == nil {
		s.State.DeleteConfirmTarget = tui.NewState(readmodel.TargetID(""))
	}
	if s.Expanded == nil {
		s.Expanded = f.Expanded
	}
	if s.RouteDraft == nil {
		s.RouteDraft = f.RouteDraft
	}
	if oldID != newID {
		// Re-key cached TargetStringRows for workspace change.
		s.TargetStringRows = migrateTargetStringRows(s.TargetStringRows, oldID, newID)
	}
	s.refreshTargetAddWorkflows()
}

func sectionHeaderKey(s *SectionView) string {
	if s.Model.ID != "" {
		return "section-header:routes:" + string(s.Model.ID)
	}
	if s.Model.Slug != "" {
		return "section-header:routes:" + s.Model.Slug
	}
	return "section-header:routes:+"
}

func SectionHeaderComponent(s *SectionView) tui.Component {
	return ui.NewSectionDisclosure(sectionHeaderKey(s), "model routes", s.Expanded)
}

// TargetAddWorkflowComponent is a mount shim so the add-target workflow itself
// receives go-tui app binding instead of being rendered as a plain element tree.
func TargetAddWorkflowComponent(wf *target_add.Workflow) *target_add.Workflow {
	return wf
}

func (s *SectionView) isExpanded(route readmodel.RouteReadModel) bool {
	return s.State.ExpandedRoute.Get() == route.ID
}

func (s *SectionView) toggleRoute(route readmodel.RouteReadModel) {
	if s.isExpanded(route) {
		s.State.ExpandedRoute.Set("")
		if row := s.RouteDetailRows[route.ID]; row != nil {
			row.CloseAll()
		}
		return
	}
	s.OpenRoute(route)
}

func (s *SectionView) OpenRoute(route readmodel.RouteReadModel) {
	s.State.ExpandedRoute.Set(route.ID)
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
	s.State.AddTargetRoute.Set("")
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
		if row := s.RouteDetailRows[previousID]; row != nil {
			delete(s.RouteDetailRows, previousID)
			s.RouteDetailRows[route.ID] = row
			row.RouteID = route.ID
		}
		if wf := s.TargetAddWorkflows[previousID]; wf != nil {
			delete(s.TargetAddWorkflows, previousID)
			s.TargetAddWorkflows[route.ID] = wf
			wf.UpdateProps(s.newTargetAddWorkflow(route))
		}
		if s.State.AddTargetRoute.Get() == previousID {
			s.State.AddTargetRoute.Set(route.ID)
		}
		return
	}
	if wf := s.TargetAddWorkflows[route.ID]; wf != nil {
		wf.UpdateProps(s.newTargetAddWorkflow(route))
	}
}

func (s *SectionView) deleteRoute(routeID readmodel.RouteID) {
	s.applyRouteDeleted(routeID)
	delete(s.RouteDetailRows, routeID)
}

func (s *SectionView) OpenTargetEditor(route readmodel.RouteReadModel, target readmodel.TargetReadModel) {
	// Legacy API — sets OpenTarget directly for external callers.
	s.State.ExpandedRoute.Set(route.ID)
	s.State.OpenTarget.Set(target.ID)
	s.State.AddTargetRoute.Set("")
	row := s.targetStringRow(route, target)
	if row != nil {
		row.Open()
	}
}

func (s *SectionView) openTarget(target readmodel.TargetReadModel) {
	route, ok := s.expandedRoute()
	if !ok {
		return
	}
	s.State.OpenTarget.Set(target.ID)
	row := s.targetStringRow(route, target)
	if row != nil {
		row.Open()
	}
}

func (s *SectionView) AddTarget(route readmodel.RouteReadModel) {
	s.State.AddTargetRoute.Set(route.ID)
	s.State.OpenTarget.Set("")
	wf := s.targetAddWorkflow(route)
	wf.Open()
}

func (s *SectionView) targetAddWorkflow(route readmodel.RouteReadModel) *target_add.Workflow {
	wf := s.TargetAddWorkflows[route.ID]
	if wf == nil {
		wf = s.newTargetAddWorkflow(route)
		s.TargetAddWorkflows[route.ID] = wf
	}
	return wf
}

func (s *SectionView) newTargetAddWorkflow(route readmodel.RouteReadModel) *target_add.Workflow {
	var opts []readmodel.ProviderOptionReadModel
	if s.ListProviders != nil {
		ctx, cancel := context.WithTimeout(context.Background(), portCallTimeout)
		defer cancel()
		if loaded, err := s.ListProviders(ctx); err == nil {
			opts = loaded
		}
	}
	wf := target_add.NewWorkflow(
		s.Model.ID,
		route,
		func(ctx context.Context, req ports.SaveTargetRequest) (readmodel.TargetReadModel, error) {
			if s.SaveTarget == nil {
				return readmodel.TargetReadModel{}, errTargetSaveNotWired
			}
			return s.SaveTarget(ctx, req)
		},
		func() { s.State.AddTargetRoute.Set("") },
		target_add.WithProviderOptions(opts),
	)
	wf.TargetSetupQueries = s.TargetSetupQueries
	wf.TargetAuthCommands = s.TargetAuthCommands
	// OnCreated saves the target into section state before OnClose clears AddTargetRoute.
	wf.OnCreated = func(t readmodel.TargetReadModel) { s.saveTarget(route.ID, t) }
	return wf
}

func (s *SectionView) refreshTargetAddWorkflows() {
	if len(s.TargetAddWorkflows) == 0 {
		return
	}
	routesByID := make(map[readmodel.RouteID]readmodel.RouteReadModel, len(s.State.Routes))
	for _, route := range s.State.Routes {
		routesByID[route.ID] = route
	}
	for routeID, wf := range s.TargetAddWorkflows {
		route, found := routesByID[routeID]
		if !found {
			delete(s.TargetAddWorkflows, routeID)
			if s.State.AddTargetRoute.Get() == routeID {
				s.State.AddTargetRoute.Set("")
			}
			continue
		}
		wf.UpdateProps(s.newTargetAddWorkflow(route))
	}
}

const portCallTimeout = 5 * time.Second

func (s *SectionView) saveTarget(routeID readmodel.RouteID, target readmodel.TargetReadModel) {
	s.applyTargetSaved(routeID, target)
}

func (s *SectionView) deleteTargetAndClose(routeID readmodel.RouteID, targetID readmodel.TargetID) {
	if s.DeleteTarget != nil {
		_ = s.DeleteTarget(context.Background(), ports.DeleteTargetRequest{
			WorkspaceID: s.Model.ID,
			RouteID:     routeID,
			TargetID:    targetID,
		})
	}
	s.applyDeleteConfirmed(routeID, targetID)
}

func (s *SectionView) confirmDeleteTarget(targetID readmodel.TargetID) {
	s.State.DeleteConfirmTarget.Set(targetID)
}

func (s *SectionView) closeDeleteTargetConfirm() {
	s.State.DeleteConfirmTarget.Set("")
}

func (s *SectionView) applyDeleteConfirmed(routeID readmodel.RouteID, targetID readmodel.TargetID) {
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
		// Renumber steps after removal so ranks stay contiguous.
		renumberSteps(&s.State.Routes[i])
		syncRouteSummary(&s.State.Routes[i])
		s.State.OpenTarget.Set("")
		s.State.DeleteConfirmTarget.Set("")
		s.State.AddTargetRoute.Set("")
		return
	}
}

// renumberSteps collapses ranks so they are 1,2,3... and removes empty steps
// except step 1 which is kept as an empty affordance.
func renumberSteps(route *readmodel.RouteReadModel) {
	if len(route.Targets) == 0 {
		return
	}
	// Sort by current rank.
	// Build unique rank list, assign new contiguous 1-based ranks.
	seen := make([]int, 0)
	for _, t := range route.Targets {
		found := false
		for _, r := range seen {
			if r == t.Rank {
				found = true
				break
			}
		}
		if !found {
			seen = append(seen, t.Rank)
		}
	}
	// Sort ascending.
	for i := 0; i < len(seen)-1; i++ {
		for j := i + 1; j < len(seen); j++ {
			if seen[j] < seen[i] {
				seen[i], seen[j] = seen[j], seen[i]
			}
		}
	}
	rankMap := make(map[int]int, len(seen))
	for i, old := range seen {
		rankMap[old] = i + 1
	}
	for i := range route.Targets {
		route.Targets[i].Rank = rankMap[route.Targets[i].Rank]
	}
}

func (s *SectionView) Back() bool {
	if s.State.DeleteConfirmTarget.Get() != "" {
		s.State.DeleteConfirmTarget.Set("")
		return true
	}
	if s.State.OpenTarget.Get() != "" {
		s.State.OpenTarget.Set("")
		return true
	}
	if s.State.AddTargetRoute.Get() != "" {
		s.State.AddTargetRoute.Set("")
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

// TargetCreateRow helpers ---------------------------------------------------

func (s *SectionView) targetCreateRowMountKey(route readmodel.RouteReadModel) string {
	return "target-create:" + string(s.Model.ID) + ":" + string(route.ID)
}

func (s *SectionView) targetCreateRow(route readmodel.RouteReadModel) *TargetStringRow {
	key := "target-create:" + string(s.Model.ID) + ":" + string(route.ID)
	row := s.TargetStringRows[key]
	if row == nil {
		row = NewTargetCreateRow(
			key,
			func(raw string) { s.submitTargetCreate(route.ID, raw) },
			func() { s.State.AddTargetRoute.Set("") },
		)
		s.TargetStringRows[key] = row
	}
	return row
}

func (s *SectionView) submitTargetCreate(routeID readmodel.RouteID, raw string) {
	provider, model, err := ParseTarget(raw)
	if err != nil {
		s.setTargetCreateError(routeID, err)
		return
	}

	var route readmodel.RouteReadModel
	for _, r := range s.State.Routes {
		if r.ID == routeID {
			route = r
			break
		}
	}
	if route.ID == "" {
		s.State.AddTargetRoute.Set("")
		return
	}

	if s.SaveTarget == nil {
		s.setTargetCreateError(routeID, errTargetSaveNotWired)
		return
	}

	saved, err := s.SaveTarget(context.Background(), ports.SaveTargetRequest{
		WorkspaceID: s.Model.ID,
		RouteID:     routeID,
		Provider:    provider,
		Model:       model,
		Rank:        nextRankForRoute(route),
		Weight:      1,
	})
	if err != nil {
		s.setTargetCreateError(routeID, err)
		return
	}

	key := "target-create:" + string(s.Model.ID) + ":" + string(routeID)
	if row := s.TargetStringRows[key]; row != nil {
		row.Close()
	}
	s.saveTarget(routeID, saved)
}

// nextRankForRoute returns a new rank higher than any existing target rank.
func nextRankForRoute(route readmodel.RouteReadModel) int {
	maxRank := 0
	for _, t := range route.Targets {
		if t.Rank > maxRank {
			maxRank = t.Rank
		}
	}
	return maxRank + 1
}

func (s *SectionView) setTargetCreateError(routeID readmodel.RouteID, err error) {
	if err == nil {
		return
	}
	key := "target-create:" + string(s.Model.ID) + ":" + string(routeID)
	if row := s.TargetStringRows[key]; row != nil {
		row.errorText.Set(err.Error())
	}
}

// RouteDetailRow helpers ----------------------------------------------------

func (s *SectionView) routeDetailRowKey(route readmodel.RouteReadModel) string {
	return "route-detail:" + string(s.Model.ID) + ":" + string(route.ID)
}

func RouteDetailRowComponent(s *SectionView, route readmodel.RouteReadModel) *RouteDetailRow {
	return s.routeDetailRow(route)
}

func (s *SectionView) routeDetailRow(route readmodel.RouteReadModel) *RouteDetailRow {
	row := s.RouteDetailRows[route.ID]
	if row == nil {
		row = NewRouteDetailRow(
			s.routeDetailRowKey(route),
			route.ModelName,
			route.Default,
			func(name string) { s.submitRouteName(route.ID, name) },
			func() { s.setRouteDefault(route.ID) },
			func() { s.confirmDeleteRoute(route.ID) },
		)
		s.RouteDetailRows[route.ID] = row
	}
	row.RouteID = route.ID
	row.SetModelName(route.ModelName)
	row.IsDefault = route.Default
	return row
}

func (s *SectionView) submitRouteName(routeID readmodel.RouteID, name string) {
	name = strings.TrimSpace(name)
	if name == "" {
		if row := s.RouteDetailRows[routeID]; row != nil {
			row.errorText.Set("enter a route model")
		}
		return
	}
	var existing readmodel.RouteReadModel
	for _, route := range s.State.Routes {
		if route.ID == routeID {
			existing = route
			break
		}
	}
	if existing.ID == "" {
		return
	}
	if s.SaveRoute == nil {
		if row := s.RouteDetailRows[routeID]; row != nil {
			row.errorText.Set("route save is not wired yet")
		}
		return
	}
	saved, err := s.SaveRoute(context.Background(), ports.SaveRouteRequest{
		WorkspaceID: s.Model.ID,
		RouteID:     routeID,
		ModelName:   name,
		Enabled:     existing.Enabled,
	})
	if err != nil {
		if row := s.RouteDetailRows[routeID]; row != nil {
			row.errorText.Set(err.Error())
		}
		return
	}
	if row := s.RouteDetailRows[routeID]; row != nil {
		row.CloseNameEdit()
	}
	s.saveRoute(routeID, saved)
}

func (s *SectionView) setRouteDefault(routeID readmodel.RouteID) {
	var existing readmodel.RouteReadModel
	for _, route := range s.State.Routes {
		if route.ID == routeID {
			existing = route
			break
		}
	}
	if existing.ID == "" || existing.Default {
		return
	}
	if s.SaveRoute == nil {
		return
	}
	saved, err := s.SaveRoute(context.Background(), ports.SaveRouteRequest{
		WorkspaceID: s.Model.ID,
		RouteID:     routeID,
		ModelName:   existing.ModelName,
		Enabled:     existing.Enabled,
		Default:     true,
	})
	if err != nil {
		if row := s.RouteDetailRows[routeID]; row != nil {
			row.errorText.Set(err.Error())
		}
		return
	}
	saved.Default = true
	s.saveRoute(routeID, saved)
}

func (s *SectionView) confirmDeleteRoute(routeID readmodel.RouteID) {
	if s.DeleteRoute == nil {
		if row := s.RouteDetailRows[routeID]; row != nil {
			row.errorText.Set("route delete is not wired yet")
		}
		return
	}
	if err := s.DeleteRoute(context.Background(), ports.DeleteRouteRequest{
		WorkspaceID: s.Model.ID,
		RouteID:     routeID,
	}); err != nil {
		if row := s.RouteDetailRows[routeID]; row != nil {
			row.errorText.Set(err.Error())
		}
		return
	}
	if row := s.RouteDetailRows[routeID]; row != nil {
		row.CloseDeleteConfirm()
	}
	s.deleteRoute(routeID)
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
	delete(s.TargetAddWorkflows, routeID)
	delete(s.RouteDetailRows, routeID)
}

func (s *SectionView) applyTargetSaved(routeID readmodel.RouteID, target readmodel.TargetReadModel) {
	for i, route := range s.State.Routes {
		if route.ID != routeID {
			continue
		}
		routeChanged := false
		for j, existing := range route.Targets {
			if existing.ID == target.ID {
				s.State.Routes[i].Targets[j] = target
				routeChanged = true
				break
			}
		}
		if !routeChanged {
			s.State.Routes[i].Targets = append(s.State.Routes[i].Targets, target)
		}
		syncRouteSummary(&s.State.Routes[i])
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
		syncRouteSummary(&s.State.Routes[i])
		s.State.OpenTarget.Set("")
		s.State.AddTargetRoute.Set("")
		return
	}
}

// syncRouteSummary keeps the row status aligned with the current target slice.
// Zero-target rows stay ordinary route rows; the draft row owns the create-only grammar.
func syncRouteSummary(route *readmodel.RouteReadModel) {
	route.State = routeStateFromTargets(route.Targets)
}

func routeStateFromTargets(targets []readmodel.TargetReadModel) readmodel.RouteState {
	return readmodel.RouteNormal
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

// groupedTargets returns targets grouped by Rank, sorted by Rank ascending.
// Each inner slice is one step.
func groupedTargets(route readmodel.RouteReadModel) [][]readmodel.TargetReadModel {
	order := make(map[int]int) // rank -> step index
	var steps [][]readmodel.TargetReadModel
	for _, t := range route.Targets {
		idx, ok := order[t.Rank]
		if !ok {
			idx = len(steps)
			order[t.Rank] = idx
			steps = append(steps, nil)
		}
		steps[idx] = append(steps[idx], t)
	}
	return steps
}

func contractRowValue(route readmodel.RouteReadModel) string {
	return "model = " + route.ModelName
}

func stepHeaderText(stepNum int, balanced bool) string {
	if balanced {
		return fmt.Sprintf("step %d        balance", stepNum)
	}
	return fmt.Sprintf("step %d", stepNum)
}

func modelSendsRowValue(route readmodel.RouteReadModel) string { return "model = " + route.ModelName }

func targetMountKey(route readmodel.RouteReadModel, target readmodel.TargetReadModel) string {
	return "target:" + string(route.ID) + ":" + string(target.ID)
}

func addTargetMountKey(route readmodel.RouteReadModel) string {
	return "add-target:" + string(route.ID)
}

func targetAddMountKey(route readmodel.RouteReadModel) string {
	return "target-add:" + string(route.ID)
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

// TargetRowComponent mounts a selectable target row indented as a route child.
// Target rows show provider/model and share% only inside balanced steps.
func TargetRowComponent(s *SectionView, route readmodel.RouteReadModel, target readmodel.TargetReadModel) *ui.SelectableRow {
	value := targetValue(target)
	stepTargets := targetsAtRank(route.Targets, target.Rank)
	if len(stepTargets) > 1 {
		value = value + " · " + sharePercent(target, stepTargets) + "%"
	}
	return ui.NewSelectableRow(
		targetMountKey(route, target),
		value,
		"",
		"edit ↵",
		func() { s.openTarget(target) },
	)
}

// targetsAtRank returns all targets sharing the same rank.
func targetsAtRank(targets []readmodel.TargetReadModel, rank int) []readmodel.TargetReadModel {
	var out []readmodel.TargetReadModel
	for _, t := range targets {
		if t.Rank == rank {
			out = append(out, t)
		}
	}
	return out
}

// sharePercent returns the display share for a target within its step.
func sharePercent(target readmodel.TargetReadModel, stepTargets []readmodel.TargetReadModel) string {
	total := 0
	for _, t := range stepTargets {
		w := t.Weight
		if w <= 0 {
			w = 1
		}
		total += w
	}
	if total == 0 {
		return "100"
	}
	w := target.Weight
	if w <= 0 {
		w = 1
	}
	// Round to nearest integer to avoid fractional percentages in UI.
	pct := (w * 100) / total
	return fmt.Sprint(pct)
}

func routeMountKey(route readmodel.RouteReadModel) string { return "route:" + string(route.ID) }

// AddTargetRowComponent mounts an "add target" selectable row.
func AddTargetRowComponent(s *SectionView, route readmodel.RouteReadModel) *ui.SelectableRow {
	return ui.NewSelectableRow(
		addTargetMountKey(route),
		"add target",
		"",
		"add ↵",
		func() { s.AddTarget(route) },
	)
}

// AddRouteRowComponent mounts an "add model route" selectable row.
func AddRouteRowComponent(s *SectionView) *ui.SelectableRow {
	row := s.AddRouteRow
	if row == nil {
		row = ui.NewSelectableRow(
			addRouteMountKey(),
			"add model route",
			"",
			"add ↵",
			func() { s.addRoute() },
		)
		s.AddRouteRow = row
	}
	row.Label = "add model route"
	row.Value = ""
	row.Action = "add ↵"
	row.Activate = func() { s.addRoute() }

	if s.addRouteFocusPending {
		row.AutoFocus = true
	}
	if row.AutoFocus && row.IsFocused() {
		s.addRouteFocusPending = false
		row.AutoFocus = false
	}
	return row
}

// SectionDraftRouteRow mounts the inline draft route input row.
func SectionDraftRouteRow(s *SectionView) *SectionDraftRouteRowView {
	return &SectionDraftRouteRowView{
		ModelName: s.RouteDraft.ModelName,
		Submit:    s.createDraftRouteFromInput,
	}
}

// migrateTargetStringRows re-keys cached string rows when the workspace ID
// changes.
func migrateTargetStringRows(rows map[string]*TargetStringRow, oldID, newID readmodel.WorkspaceID) map[string]*TargetStringRow {
	if len(rows) == 0 {
		return rows
	}
	next := make(map[string]*TargetStringRow, len(rows))
	oldPrefix := "target-string:" + string(oldID) + ":"
	newPrefix := "target-string:" + string(newID) + ":"
	for key, row := range rows {
		if strings.HasPrefix(key, oldPrefix) {
			key = newPrefix + strings.TrimPrefix(key, oldPrefix)
		}
		next[key] = row
	}
	return next
}

// TargetStringRow helpers ---------------------------------------------------

func (s *SectionView) targetStringRowKey(route readmodel.RouteReadModel, target readmodel.TargetReadModel) string {
	return "target-string:" + string(s.Model.ID) + ":" + string(route.ID) + ":" + string(target.ID)
}

// TargetStringRowComponent returns the cached or newly created
// TargetStringRow for a target. The row is cached by
// workspace:route:target so its editing state survives re-renders.
func TargetStringRowComponent(s *SectionView, route readmodel.RouteReadModel, target readmodel.TargetReadModel) *TargetStringRow {
	return s.targetStringRow(route, target)
}

func (s *SectionView) targetStringRow(route readmodel.RouteReadModel, target readmodel.TargetReadModel) *TargetStringRow {
	key := s.targetStringRowKey(route, target)
	row := s.TargetStringRows[key]
	if row == nil {
		row = NewTargetStringRow(
			targetMountKey(route, target),
			targetLabel(route, target),
			FormatTarget(target.Provider, target.Model),
			func(raw string) {
				s.submitTargetEdit(route.ID, target.ID, raw)
			},
			func() {
				s.confirmDeleteTarget(target.ID)
			},
		)
		s.TargetStringRows[key] = row
	}
	row.SetSaved(FormatTarget(target.Provider, target.Model))
	row.Label = targetLabel(route, target)
	return row
}

// submitTargetEdit validates the raw string, builds a request that preserves
// all existing target fields, and calls the port.
func (s *SectionView) submitTargetEdit(routeID readmodel.RouteID, targetID readmodel.TargetID, raw string) {
	provider, model, err := ParseTarget(raw)
	if err != nil {
		s.setTargetStringError(routeID, targetID, err)
		return
	}

	var existing readmodel.TargetReadModel
	found := false
	for _, route := range s.State.Routes {
		if route.ID != routeID {
			continue
		}
		for _, t := range route.Targets {
			if t.ID == targetID {
				existing = t
				found = true
				break
			}
		}
		break
	}
	if !found {
		s.State.OpenTarget.Set("")
		return
	}

	if s.SaveTarget == nil {
		s.setTargetStringError(routeID, targetID, errTargetSaveNotWired)
		return
	}

	req := ports.SaveTargetRequest{
		WorkspaceID:      s.Model.ID,
		RouteID:          routeID,
		TargetID:         targetID,
		Name:             existing.Name,
		Provider:         provider,
		Model:            model,
		ProviderProtocol: existing.ProviderProtocol,
		BaseURL:          existing.BaseURL,
		CredentialRef:    existing.CredentialRef,
		Rank:             existing.Rank,
		Weight:           existing.Weight,
	}

	saved, err := s.SaveTarget(context.Background(), req)
	if err != nil {
		s.setTargetStringError(routeID, targetID, err)
		return
	}

	s.closeTargetStringRow(routeID, targetID)
	s.saveTarget(routeID, saved)
}

func (s *SectionView) closeTargetStringRow(routeID readmodel.RouteID, targetID readmodel.TargetID) {
	key := "target-string:" + string(s.Model.ID) + ":" + string(routeID) + ":" + string(targetID)
	if row := s.TargetStringRows[key]; row != nil {
		row.Close()
	}
}

func (s *SectionView) setTargetStringError(routeID readmodel.RouteID, targetID readmodel.TargetID, err error) {
	if err == nil {
		return
	}
	key := "target-string:" + string(s.Model.ID) + ":" + string(routeID) + ":" + string(targetID)
	if row := s.TargetStringRows[key]; row != nil {
		row.errorText.Set(err.Error())
	}
}

var errTargetSaveNotWired = errStr("target save is not wired")

type errStr string

func (e errStr) Error() string { return string(e) }
