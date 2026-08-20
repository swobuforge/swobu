package routes

import (
	"context"
	"fmt"
	"strings"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/features/target_config"
	"github.com/swobuforge/swobu/internal/cockpit/ports"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/cockpit/ui"
)

// port function aliases keep route mutation behind the section boundary.
type SaveRouteFunc func(context.Context, ports.SaveRouteRequest) (ports.RouteMutationResult, error)
type DeleteRouteFunc func(context.Context, ports.DeleteRouteRequest) (ports.RouteMutationResult, error)

// SectionView owns the mutable route section workflow and renders the route
// list. RouteSectionState stays data-only; the section owns all behavior.
type SectionView struct {
	Model      readmodel.WorkspaceReadModel
	Expanded   *tui.State[bool]
	State      *RouteSectionState
	DraftRoute *DraftRoute
	// TargetConfigs holds mounted add/edit target config instances in this section.
	// ProviderOptions must be set from workspace readmodel before the section is
	// mounted; mounts must not call out to query ports for static catalog data.
	TargetConfigs        *TargetConfigMounts
	AddRouteRow          *ui.SelectableRow
	addRouteFocusPending bool
	SaveRoute            SaveRouteFunc
	DeleteRoute          DeleteRouteFunc
	ApplyRouteDraft      func(context.Context, ports.ApplyRouteDraftRequest) (ports.RouteMutationResult, error)
	OnWorkspacePersisted func(readmodel.WorkspaceReadModel)
	OnWorkspaceCommitted func(readmodel.WorkspaceReadModel)
}

func Section(model readmodel.WorkspaceReadModel, commands ports.RouteCommands) *SectionView {
	section := &SectionView{
		Model:         model,
		Expanded:      tui.NewState(true),
		State:         NewRouteSectionState(model.Routes),
		TargetConfigs: NewTargetConfigMounts(model.RoutingWorkspaceID()),
	}
	section.TargetConfigs.ProviderOptions = model.ProviderOptions
	section.configureTargetConfigMounts()
	if commands != nil {
		section.SaveRoute = commands.SaveRoute
		section.DeleteRoute = commands.DeleteRoute
		section.ApplyRouteDraft = commands.ApplyRouteDraft
		section.TargetConfigs.Commands.SaveTarget = commands.SaveTarget
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
		tui.OnStop(tui.KeyEnter, ui.ActivateCurrentSelection),
		tui.OnStop(tui.Rune(' '), ui.ActivateCurrentSelection),
	}
}

// BindApp binds the route section's semantic state so route backout redraws
// the mounted section immediately instead of leaving stale inline detail on
// screen.
func (s *SectionView) BindApp(app *tui.App) {
	if s == nil || s.State == nil {
		return
	}
	s.State.ExpandedRoute.BindApp(app)
	s.State.OpenTarget.BindApp(app)
	s.State.AddTargetRoute.BindApp(app)
	s.State.DeleteConfirmTarget.BindApp(app)
	s.State.FocusRoute.BindApp(app)
}

func (s *SectionView) UpdateProps(fresh tui.Component) {
	f, ok := fresh.(*SectionView)
	if !ok {
		return
	}
	s.Model = f.Model
	s.SaveRoute = f.SaveRoute
	s.DeleteRoute = f.DeleteRoute
	s.ApplyRouteDraft = f.ApplyRouteDraft
	s.OnWorkspacePersisted = f.OnWorkspacePersisted
	s.OnWorkspaceCommitted = f.OnWorkspaceCommitted
	if s.State == nil {
		s.State = f.State
	} else if f.State != nil {
		s.State.Routes = append([]readmodel.RouteReadModel(nil), f.State.Routes...)
	}
	if s.State != nil && s.State.DeleteConfirmTarget == nil {
		s.State.DeleteConfirmTarget = tui.NewState(readmodel.TargetID(""))
	}
	if s.State != nil && s.State.FocusRoute == nil {
		s.State.FocusRoute = tui.NewState(readmodel.RouteID(""))
	}
	if s.Expanded == nil {
		s.Expanded = f.Expanded
	}
	if s.DraftRoute == nil && f.DraftRoute != nil {
		s.DraftRoute = f.DraftRoute
	}
	if s.TargetConfigs == nil {
		s.TargetConfigs = NewTargetConfigMounts(s.Model.RoutingWorkspaceID())
	}
	s.TargetConfigs.UpdateFrom(f.TargetConfigs)
	// ProviderOptions are readmodel-props, not query results. They must flow from
	// the workspace model through Page → Section → Host. If the model itself
	// changed, overwrite whatever the prior host held.
	if f.Model.ProviderOptions != nil {
		s.TargetConfigs.ProviderOptions = f.Model.ProviderOptions
	}
	s.configureTargetConfigMounts()
	s.refreshTargetConfigs()
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

// TargetConfigComponent is a mount shim so the target config itself
// receives go-tui app binding instead of being rendered as a plain element tree.
func TargetConfigComponent(config *target_config.TargetConfig) *target_config.TargetConfig {
	return config
}

func (s *SectionView) configureTargetConfigMounts() {
	if s.TargetConfigs == nil {
		s.TargetConfigs = NewTargetConfigMounts(s.Model.RoutingWorkspaceID())
	}
	s.TargetConfigs.WorkspaceID = s.Model.RoutingWorkspaceID()
	s.TargetConfigs.Callbacks.OnCreated = func(result ports.SaveTargetResult) {
		s.applyCommittedWorkspace(result.Workspace)
		s.State.AddTargetRoute.Set("")
		if s.Model.IsOnboarding() && result.Workspace.ID != "" && s.OnWorkspacePersisted != nil {
			s.OnWorkspacePersisted(result.Workspace)
		}
	}
	s.TargetConfigs.Callbacks.OnSaved = func(result ports.SaveTargetResult) {
		s.applyCommittedWorkspace(result.Workspace)
		s.State.OpenTarget.Set("")
	}
	s.TargetConfigs.Callbacks.OnDeleteConfirmed = func(routeID readmodel.RouteID, targetID readmodel.TargetID) error {
		return s.deleteTargetAndClose(routeID, targetID)
	}
	s.TargetConfigs.Callbacks.OnAddClose = func(routeID readmodel.RouteID) {
		if s.State != nil && s.State.AddTargetRoute.Get() == routeID {
			s.State.AddTargetRoute.Set("")
		}
	}
	s.TargetConfigs.Callbacks.OnEditClose = func(targetID readmodel.TargetID) {
		if s.State != nil && s.State.OpenTarget.Get() == targetID {
			s.State.OpenTarget.Set("")
		}
	}
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
	s.DraftRoute = NewDraftRoute(s.Model.RoutingWorkspaceID())
	s.DraftRoute.Open()
	if s.AddRouteRow != nil {
		s.AddRouteRow.AutoFocus = false
		s.AddRouteRow.Blur()
	}
	s.State.OpenTarget.Set("")
	s.State.AddTargetRoute.Set("")
}

func (s *SectionView) closeDraftRoute() {
	if s.DraftRoute == nil {
		return
	}
	s.DraftRoute = nil
}

func (s *SectionView) createDraftRoute(name string) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "route-new"
	}
	route := readmodel.RouteReadModel{
		ID:        readmodel.RouteID(name),
		ModelName: name,
		Enabled:   true,
	}
	s.applyRouteUpsert(route)
	s.OpenRoute(route)
	s.State.OpenTarget.Set("")
	s.State.AddTargetRoute.Set("")
	s.DraftRoute = nil
}

func (s *SectionView) saveRoute(previousID readmodel.RouteID, route readmodel.RouteReadModel) {
	s.applyRouteSaved(previousID, route)
	if route.ID != previousID {
		s.TargetConfigs.RekeyRoute(previousID, route)
		if s.State.AddTargetRoute.Get() == previousID {
			s.State.AddTargetRoute.Set(route.ID)
		}
		return
	}
	s.TargetConfigs.RefreshAdd(route)
}

func (s *SectionView) deleteRoute(routeID readmodel.RouteID) {
	s.applyRouteDeleted(routeID)
}

func (s *SectionView) OpenTargetEditor(route readmodel.RouteReadModel, target readmodel.TargetReadModel) {
	s.State.ExpandedRoute.Set(route.ID)
	s.State.OpenTarget.Set(target.ID)
	s.State.AddTargetRoute.Set("")
	s.TargetConfigs.OpenEdit(route, target)
}

func (s *SectionView) openTarget(target readmodel.TargetReadModel) {
	route, ok := s.expandedRoute()
	if !ok {
		return
	}
	s.State.OpenTarget.Set(target.ID)
	s.State.AddTargetRoute.Set("")
	s.TargetConfigs.OpenEdit(route, target)
}

func (s *SectionView) AddTarget(route readmodel.RouteReadModel) {
	s.State.AddTargetRoute.Set(route.ID)
	s.State.OpenTarget.Set("")
	s.TargetConfigs.OpenAdd(route)
}

func (s *SectionView) targetAddConfig(route readmodel.RouteReadModel) *target_config.TargetConfig {
	return s.TargetConfigs.Add(route)
}

func (s *SectionView) targetEditConfig(route readmodel.RouteReadModel, target readmodel.TargetReadModel) *target_config.TargetConfig {
	return s.TargetConfigs.Edit(route, target)
}

func (s *SectionView) refreshTargetConfigs() {
	if s.TargetConfigs == nil || s.State == nil {
		return
	}
	s.TargetConfigs.Refresh(s.State.Routes, func(routeID readmodel.RouteID) {
		if s.State.AddTargetRoute.Get() == routeID {
			s.State.AddTargetRoute.Set("")
		}
	})
}

func (s *SectionView) deleteTargetAndClose(routeID readmodel.RouteID, targetID readmodel.TargetID) error {
	if s.ApplyRouteDraft == nil {
		return errTargetDeleteNotWired
	}
	route, ok := s.routeWithoutTarget(routeID, targetID)
	if !ok {
		return fmt.Errorf("target %q is missing", targetID)
	}
	committed, err := s.ApplyRouteDraft(context.Background(), ports.ApplyRouteDraftRequest{WorkspaceID: s.Model.RoutingWorkspaceID(), Route: route})
	if err != nil {
		return err
	}
	s.applyCommittedWorkspace(committed.Workspace)
	s.State.OpenTarget.Set("")
	s.State.DeleteConfirmTarget.Set("")
	return nil
}

func (s *SectionView) routeWithoutTarget(routeID readmodel.RouteID, targetID readmodel.TargetID) (readmodel.RouteReadModel, bool) {
	for _, route := range s.State.Routes {
		if route.ID != routeID {
			continue
		}
		found := false
		tiers := make([]readmodel.TierReadModel, 0, len(route.Tiers))
		for _, tier := range route.Tiers {
			targets := make([]readmodel.TargetReadModel, 0, len(tier.Targets))
			for _, target := range tier.Targets {
				if target.ID == targetID {
					found = true
					continue
				}
				targets = append(targets, target)
			}
			if len(targets) > 0 {
				tiers = append(tiers, readmodel.TierReadModel{Targets: targets})
			}
		}
		route.Tiers = tiers
		return route, found
	}
	return readmodel.RouteReadModel{}, false
}

func (s *SectionView) confirmDeleteTarget(targetID readmodel.TargetID) {
	s.State.OpenTarget.Set("")
	s.State.DeleteConfirmTarget.Set(targetID)
}

func (s *SectionView) closeDeleteTargetConfirm() {
	s.State.DeleteConfirmTarget.Set("")
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
	if s.DraftRoute != nil {
		s.closeDraftRoute()
		return true
	}
	if s.State.ExpandedRoute.Get() != "" {
		s.State.ExpandedRoute.Set("")
		return true
	}
	return false
}

func (s *SectionView) targetConfigKey(route readmodel.RouteReadModel, targetID readmodel.TargetID) string {
	return s.TargetConfigs.MountKey(route, targetID)
}

func (s *SectionView) targetConfigMountKey(route readmodel.RouteReadModel, targetID readmodel.TargetID) string {
	return s.targetConfigKey(route, targetID)
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
	return
}

func (s *SectionView) applyCommittedWorkspace(workspace readmodel.WorkspaceReadModel) {
	s.commitWorkspace(workspace)
}

func (s *SectionView) commitWorkspace(workspace readmodel.WorkspaceReadModel) {
	if s.Model.IsOnboarding() {
		return
	}
	if workspace.ID == "" {
		return
	}
	s.Model = workspace
	s.State.Routes = append([]readmodel.RouteReadModel(nil), workspace.Routes...)
	s.refreshTargetConfigs()
	if s.OnWorkspaceCommitted != nil {
		s.OnWorkspaceCommitted(workspace)
	}
}

func (s *SectionView) submitRouteName(routeID readmodel.RouteID, name string) {
	name = strings.TrimSpace(name)
	if name == "" {
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
	if s.Model.IsOnboarding() {
		saved := existing
		saved.ID = readmodel.RouteID(name)
		saved.ModelName = name
		s.saveRoute(routeID, saved)
		return
	}
	if s.SaveRoute == nil {
		return
	}
	expanded := s.State.ExpandedRoute.Get() == routeID
	addTarget := s.State.AddTargetRoute.Get() == routeID
	saved, err := s.SaveRoute(context.Background(), ports.SaveRouteRequest{
		WorkspaceID: s.Model.RoutingWorkspaceID(),
		RouteID:     routeID,
		ModelName:   name,
		Enabled:     existing.Enabled,
	})
	if err != nil {
		return
	}
	newRouteID := saved.Route.ID
	if newRouteID == "" {
		newRouteID = readmodel.RouteID(name)
	}
	if newRouteID != routeID {
		// Rekey the ephemeral child before authoritative refresh can garbage-collect
		// mounts whose old route identity is absent from the committed workspace.
		s.TargetConfigs.RekeyRoute(routeID, saved.Route)
	}
	s.applyCommittedWorkspace(saved.Workspace)
	if newRouteID != routeID {
		if expanded {
			s.State.ExpandedRoute.Set(newRouteID)
		}
		if addTarget {
			s.State.AddTargetRoute.Set(newRouteID)
		}
	}
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
		WorkspaceID: s.Model.RoutingWorkspaceID(),
		RouteID:     routeID,
		ModelName:   existing.ModelName,
		Enabled:     existing.Enabled,
		Default:     true,
	})
	if err != nil {
		return
	}
	s.applyCommittedWorkspace(saved.Workspace)
}

func (s *SectionView) confirmDeleteRoute(routeID readmodel.RouteID) error {
	if s.Model.IsOnboarding() {
		s.deleteRoute(routeID)
		return nil
	}
	if s.DeleteRoute == nil {
		return nil
	}
	deletedIdx := s.routeIndex(routeID)
	result, err := s.DeleteRoute(context.Background(), ports.DeleteRouteRequest{
		WorkspaceID: s.Model.RoutingWorkspaceID(),
		RouteID:     routeID,
	})
	if err != nil {
		return err
	}
	s.applyCommittedWorkspace(result.Workspace)
	s.finishRouteDeleteInteraction(routeID, deletedIdx)
	return nil
}

func (s *SectionView) routeIndex(routeID readmodel.RouteID) int {
	for i, route := range s.State.Routes {
		if route.ID == routeID {
			return i
		}
	}
	return -1
}

func (s *SectionView) finishRouteDeleteInteraction(routeID readmodel.RouteID, deletedIdx int) {
	if deletedIdx >= 0 {
		s.requestRouteDeleteFocus(deletedIdx)
	}
	s.State.ExpandedRoute.Set("")
	s.State.OpenTarget.Set("")
	s.State.AddTargetRoute.Set("")
	s.State.DeleteConfirmTarget.Set("")
	s.TargetConfigs.DeleteRoute(routeID)
}

func contractRowValue(route readmodel.RouteReadModel) string {
	return "model = " + route.ModelName
}

// RouteNameRowComponent mounts an editable row for the route model name.
func RouteNameRowComponent(s *SectionView, route readmodel.RouteReadModel) *ui.EditableRow {
	value := tui.NewState(route.ModelName)
	row := ui.NewEditableRow(s.routeNameRowKey(route), "name", value)
	row.ViewAction = "edit ↵"
	row.EditAction = "save ↵"
	row.OnSubmit = func(name string) {
		s.submitRouteName(route.ID, name)
	}
	row.OnClose = func() {
		for _, r := range s.State.Routes {
			if r.ID == route.ID {
				value.Set(r.ModelName)
				break
			}
		}
	}
	return row
}

func (s *SectionView) routeNameRowKey(route readmodel.RouteReadModel) string {
	return "route-name:" + string(s.Model.ID) + ":" + string(route.ID)
}

// DraftParentRowComponent mounts the selectable parent row for draft route creation.
func DraftParentRowComponent(s *SectionView) *ui.SelectableRow {
	draft := s.DraftRoute
	if draft == nil {
		draft = NewDraftRoute(s.Model.RoutingWorkspaceID())
	}
	label := draft.ModelName()
	if label == "" {
		label = "draft"
	}
	return ui.NewSelectableRow(
		"draft-parent",
		label,
		"incomplete",
		"collapse ↵",
		func() {
			s.closeDraftRoute()
		},
	)
}

// DraftNameRowComponent mounts the editable row for draft route name input.
func DraftNameRowComponent(s *SectionView) *ui.EditableRow {
	draft := s.DraftRoute
	if draft == nil {
		draft = NewDraftRoute(s.Model.RoutingWorkspaceID())
	}
	value := draft.Name
	row := ui.NewEditableRow("draft-name", "name", value)
	row.ViewAction = "create ↵"
	row.EditAction = "create ↵"
	row.OnSubmit = func(name string) {
		s.createDraftRoute(name)
	}
	return row
}

func (s *SectionView) routeDeleteRowKey(route readmodel.RouteReadModel) string {
	return "route-delete:" + string(s.Model.ID) + ":" + string(route.ID)
}

// RouteDeleteRowComponent mounts the route-level delete row inside the expanded
// route capsule using the shared ui.ConfirmActionRow. It deletes the route
// object and all its targets.
func RouteDeleteRowComponent(s *SectionView, route readmodel.RouteReadModel) *ui.ConfirmActionRow {
	modelName := route.ModelName
	copy := ui.ConfirmActionCopy{
		Label:           "delete",
		IdleValue:       "model route",
		IdleAction:      "delete ↵",
		ConfirmValue:    "delete " + modelName + "?",
		ConfirmAction:   "confirm ↵",
		SubmittingValue: "deleting " + modelName + "…",
		SubmittingHint:  "wait",
		FailedValue:     "delete failed",
		FailedAction:    "retry ↵",
	}
	return ui.NewConfirmActionRow(
		s.routeDeleteRowKey(route),
		copy,
		func() error { return s.confirmDeleteRoute(route.ID) },
	)
}

func (s *SectionView) routeDefaultRowKey(route readmodel.RouteReadModel) string {
	return "route-default:" + string(s.Model.ID) + ":" + string(route.ID)
}

func RouteDefaultRowComponent(s *SectionView, route readmodel.RouteReadModel) *ui.SelectableRow {
	action := "make default ↵"
	value := "no"
	if route.Default {
		action = "current"
		value = "yes"
	}
	return ui.NewSelectableRow(
		s.routeDefaultRowKey(route),
		"default",
		value,
		action,
		func() { s.setRouteDefault(route.ID) },
	)
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
	deletedIdx := -1
	next := make([]readmodel.RouteReadModel, 0, len(s.State.Routes))
	for i, route := range s.State.Routes {
		if route.ID == routeID {
			deletedIdx = i
			continue
		}
		next = append(next, route)
	}
	s.State.Routes = next
	if deletedIdx >= 0 {
		s.requestRouteDeleteFocus(deletedIdx)
	}
	if s.State.ExpandedRoute.Get() == routeID {
		s.State.ExpandedRoute.Set("")
	}
	s.State.OpenTarget.Set("")
	s.State.AddTargetRoute.Set("")
	s.State.DeleteConfirmTarget.Set("")
	s.TargetConfigs.DeleteRoute(routeID)
}

func (s *SectionView) ensureDefaultAfterDeletion() {
	for _, route := range s.State.Routes {
		if route.Default {
			return
		}
	}
	for _, route := range s.State.Routes {
		if route.TargetCount() == 0 {
			continue
		}
		s.markOnlyDefault(route.ID)
		return
	}
}

func (s *SectionView) requestRouteDeleteFocus(deletedIdx int) {
	if s.State == nil {
		return
	}
	if deletedIdx >= 0 && deletedIdx < len(s.State.Routes) {
		s.State.FocusRoute.Set(s.State.Routes[deletedIdx].ID)
		s.addRouteFocusPending = false
		return
	}
	if deletedIdx-1 >= 0 && deletedIdx-1 < len(s.State.Routes) {
		s.State.FocusRoute.Set(s.State.Routes[deletedIdx-1].ID)
		s.addRouteFocusPending = false
		return
	}
	s.State.FocusRoute.Set("")
	s.RequestAddRouteFocus()
}

func (s *SectionView) markOnlyDefault(routeID readmodel.RouteID) {
	for i := range s.State.Routes {
		s.State.Routes[i].Default = s.State.Routes[i].ID == routeID
	}
}

func targetRankLabel(target readmodel.TargetReadModel) string { return string(target.ID) }

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

// groupedTargets returns structural tiers without inference or sorting.
func groupedTargets(route readmodel.RouteReadModel) [][]readmodel.TargetReadModel {
	out := make([][]readmodel.TargetReadModel, 0, len(route.Tiers))
	for _, tier := range route.Tiers {
		out = append(out, append([]readmodel.TargetReadModel(nil), tier.Targets...))
	}
	return out
}

func tierHeaderText(tierIndex int, balanced bool) string {
	label := "primary"
	if tierIndex > 0 {
		label = fmt.Sprintf("fallback %d", tierIndex)
	}
	if balanced {
		return label + "        balance"
	}
	return label
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
	row := ui.NewSelectableRow(
		routeMountKey(route),
		route.ModelName,
		route.RowValue(),
		routeActionLabel(s.isExpanded(route)),
		func() { s.toggleRoute(route) },
	)
	if s.State != nil && s.State.FocusRoute != nil && s.State.FocusRoute.Get() == route.ID {
		row.AutoFocus = true
	}
	if row.AutoFocus && row.IsFocused() && s.State != nil && s.State.FocusRoute != nil {
		s.State.FocusRoute.Set("")
		row.AutoFocus = false
	}
	if s.isExpanded(route) {
		row.OnEscape = func() { s.State.ExpandedRoute.Set("") }
	}
	return row
}

// TargetRowComponent mounts a selectable target row indented as a route child.
// Target rows show provider/model and equal share inside balanced tiers.
func TargetRowComponent(s *SectionView, route readmodel.RouteReadModel, target readmodel.TargetReadModel) *ui.SelectableRow {
	value := targetValue(target)
	if tierIndex, ok := route.TargetTier(target.ID); ok && len(route.Tiers[tierIndex].Targets) > 1 {
		value = value + " · " + fmt.Sprint(100/len(route.Tiers[tierIndex].Targets)) + "%"
	}
	row := ui.NewSelectableRow(
		targetMountKey(route, target),
		"",
		value,
		"edit ↵",
		func() { s.openTarget(target) },
	)
	if s.State.OpenTarget.Get() == target.ID {
		row.OnEscape = func() { s.State.OpenTarget.Set("") }
	}
	return row
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

var (
	errTargetSaveNotWired   = errStr("target save is not wired")
	errTargetDeleteNotWired = errStr("target delete is not wired")
)

type errStr string

func (e errStr) Error() string { return string(e) }
