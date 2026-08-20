package routes

import (
	"context"

	"github.com/swobuforge/swobu/internal/cockpit/features/target_config"
	"github.com/swobuforge/swobu/internal/cockpit/ports"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
)

// TargetConfigMode separates create and edit mounts without relying on
// pseudo-ID strings.
type TargetConfigMode string

const (
	TargetConfigAdd  TargetConfigMode = "add"
	TargetConfigEdit TargetConfigMode = "edit"
)

// TargetConfigKey is the structural identity for one mounted target config.
// Route cleanup matches typed fields so route IDs cannot collide through string
// formatting.
type TargetConfigKey struct {
	Mode        TargetConfigMode
	WorkspaceID readmodel.WorkspaceID
	RouteID     readmodel.RouteID
	TargetID    readmodel.TargetID
}

func (k TargetConfigKey) mountKey() string {
	return "target-config:" + string(k.Mode) + ":" + string(k.WorkspaceID) + ":" + string(k.RouteID) + ":" + string(k.TargetID)
}

// TargetConfigCommands are the domain effects a mounted target config may
// execute. Static provider picker options are page/readmodel data, not commands.
type TargetConfigCommands struct {
	SaveTarget  func(context.Context, ports.SaveTargetRequest) (ports.SaveTargetResult, error)
	Setup       ports.TargetSetupQueries
	Auth        ports.TargetAuthCommands
	Credentials ports.TargetCredentialCommands
}

// TargetConfigCallbacks report local target config lifecycle events back to the
// routes section that mounted the target config.
type TargetConfigCallbacks struct {
	OnCreated         func(ports.SaveTargetResult)
	OnSaved           func(ports.SaveTargetResult)
	OnDeleteConfirmed func(readmodel.RouteID, readmodel.TargetID) error
	OnAddClose        func(readmodel.RouteID)
	OnEditClose       func(readmodel.TargetID)
}

// TargetConfigMounts owns mounted target add/edit target config instances for one
// routes section. The section chooses where a target config is mounted; this
// keyed child cache owns only construction, instance refresh, and lifecycle
// callbacks.
//
// ProviderOptions are static catalog data for the provider picker. They arrive
// as plain readmodel props (never a port), already hydrated during workspace
// load. Mount code must never call out to the daemon or hold caches for data
// that belongs in the readmodel.
type TargetConfigMounts struct {
	WorkspaceID     readmodel.WorkspaceID
	ProviderOptions []readmodel.ProviderOptionReadModel
	components      map[TargetConfigKey]*target_config.TargetConfig
	Commands        TargetConfigCommands
	Callbacks       TargetConfigCallbacks
}

func NewTargetConfigMounts(workspaceID readmodel.WorkspaceID) *TargetConfigMounts {
	return &TargetConfigMounts{
		WorkspaceID: workspaceID,
		components:  make(map[TargetConfigKey]*target_config.TargetConfig),
	}
}

func (h *TargetConfigMounts) UpdateFrom(fresh *TargetConfigMounts) {
	if h == nil || fresh == nil {
		return
	}
	h.WorkspaceID = fresh.WorkspaceID
	h.ProviderOptions = fresh.ProviderOptions
	h.Commands = fresh.Commands
	h.Callbacks = fresh.Callbacks
	h.refreshCachedTargetConfigDependencies()
}

func (h *TargetConfigMounts) OpenAdd(route readmodel.RouteReadModel) *target_config.TargetConfig {
	wf := h.Add(route)
	wf.Open()
	return wf
}

func (h *TargetConfigMounts) Add(route readmodel.RouteReadModel) *target_config.TargetConfig {
	h.ensureMaps()
	key := h.addKey(route.ID)
	wf := h.components[key]
	if wf == nil {
		wf = h.newAdd(route)
		h.components[key] = wf
	}
	return wf
}

func (h *TargetConfigMounts) OpenEdit(route readmodel.RouteReadModel, target readmodel.TargetReadModel) *target_config.TargetConfig {
	wf := h.Edit(route, target)
	wf.Open()
	return wf
}

func (h *TargetConfigMounts) Edit(route readmodel.RouteReadModel, target readmodel.TargetReadModel) *target_config.TargetConfig {
	h.ensureMaps()
	key := h.editKey(route.ID, target.ID)
	wf := h.components[key]
	if wf == nil {
		wf = h.newEdit(route, target)
		h.components[key] = wf
	}
	return wf
}

// RekeyRoute preserves every transient target editor owned by a route while
// the persisted route identity changes.
func (h *TargetConfigMounts) RekeyRoute(previousID readmodel.RouteID, route readmodel.RouteReadModel) {
	h.ensureMaps()
	if wf := h.components[h.addKey(previousID)]; wf != nil {
		delete(h.components, h.addKey(previousID))
		h.components[h.addKey(route.ID)] = wf
		h.refreshAddConfig(wf, route)
	}
	for key, wf := range h.components {
		if key.Mode != TargetConfigEdit || key.RouteID != previousID {
			continue
		}
		delete(h.components, key)
		key.RouteID = route.ID
		h.components[key] = wf
		// Rekey only; do not refresh the target snapshot, which would overwrite
		// operator-entered draft fields before the new aggregate is mounted.
		wf.WorkspaceID = h.WorkspaceID
		wf.Route = route
	}
}

func (h *TargetConfigMounts) RefreshAdd(route readmodel.RouteReadModel) {
	h.ensureMaps()
	if wf := h.components[h.addKey(route.ID)]; wf != nil {
		h.refreshAddConfig(wf, route)
	}
}

func (h *TargetConfigMounts) Refresh(routes []readmodel.RouteReadModel, closeAddRoute func(readmodel.RouteID)) {
	h.ensureMaps()
	routesByID := make(map[readmodel.RouteID]readmodel.RouteReadModel, len(routes))
	for _, route := range routes {
		routesByID[route.ID] = route
	}
	for key, wf := range h.components {
		if key.Mode != TargetConfigAdd {
			continue
		}
		route, found := routesByID[key.RouteID]
		if !found {
			delete(h.components, key)
			if closeAddRoute != nil {
				closeAddRoute(key.RouteID)
			}
			continue
		}
		h.refreshAddConfig(wf, route)
	}

	liveEditConfigs := make(map[TargetConfigKey]struct{})
	for _, route := range routes {
		for _, target := range route.AllTargets() {
			key := h.editKey(route.ID, target.ID)
			liveEditConfigs[key] = struct{}{}
			if wf := h.components[key]; wf != nil {
				h.refreshEditConfig(wf, route, target)
			}
		}
	}
	for key := range h.components {
		if key.Mode != TargetConfigEdit {
			continue
		}
		if _, ok := liveEditConfigs[key]; !ok {
			delete(h.components, key)
		}
	}
}

func (h *TargetConfigMounts) DeleteRoute(routeID readmodel.RouteID) {
	h.ensureMaps()
	for key := range h.components {
		if key.RouteID == routeID {
			delete(h.components, key)
		}
	}
}

func (h *TargetConfigMounts) DeleteEdit(route readmodel.RouteReadModel, targetID readmodel.TargetID) {
	h.ensureMaps()
	delete(h.components, h.editKey(route.ID, targetID))
}

func (h *TargetConfigMounts) HasAdd(routeID readmodel.RouteID) bool {
	h.ensureMaps()
	_, ok := h.components[h.addKey(routeID)]
	return ok
}

func (h *TargetConfigMounts) CachedAdd(routeID readmodel.RouteID) *target_config.TargetConfig {
	h.ensureMaps()
	return h.components[h.addKey(routeID)]
}

func (h *TargetConfigMounts) HasEdit(route readmodel.RouteReadModel, targetID readmodel.TargetID) bool {
	h.ensureMaps()
	_, ok := h.components[h.editKey(route.ID, targetID)]
	return ok
}

func (h *TargetConfigMounts) MountKey(route readmodel.RouteReadModel, targetID readmodel.TargetID) string {
	return h.editKey(route.ID, targetID).mountKey()
}

func (h *TargetConfigMounts) newAdd(route readmodel.RouteReadModel) *target_config.TargetConfig {
	var wf *target_config.TargetConfig
	wf = target_config.NewTargetConfig(
		h.WorkspaceID,
		route,
		h.saveTarget,
		func() {
			if h.Callbacks.OnAddClose != nil {
				h.Callbacks.OnAddClose(wf.Route.ID)
			}
		},
	)
	h.refreshAddConfig(wf, route)
	wf.OnCreated = func(result ports.SaveTargetResult) {
		if h.Callbacks.OnCreated != nil {
			h.Callbacks.OnCreated(result)
		}
	}
	return wf
}

func (h *TargetConfigMounts) newEdit(route readmodel.RouteReadModel, target readmodel.TargetReadModel) *target_config.TargetConfig {
	var wf *target_config.TargetConfig
	wf = target_config.NewEditTargetConfig(
		h.WorkspaceID,
		route,
		target,
		h.saveTarget,
		func() {
			if h.Callbacks.OnEditClose != nil {
				h.Callbacks.OnEditClose(wf.Target.ID)
			}
		},
	)
	h.refreshEditConfig(wf, route, target)
	wf.OnCreated = func(result ports.SaveTargetResult) {
		if h.Callbacks.OnCreated != nil {
			h.Callbacks.OnCreated(result)
		}
	}
	wf.OnSaved = func(result ports.SaveTargetResult) {
		if h.Callbacks.OnSaved != nil {
			h.Callbacks.OnSaved(result)
		}
	}
	wf.OnDeleteConfirmed = func() error {
		if h.Callbacks.OnDeleteConfirmed != nil {
			return h.Callbacks.OnDeleteConfirmed(wf.Route.ID, wf.Target.ID)
		}
		return nil
	}
	return wf
}

func (h *TargetConfigMounts) saveTarget(ctx context.Context, req ports.SaveTargetRequest) (ports.SaveTargetResult, error) {
	if h.Commands.SaveTarget == nil {
		return ports.SaveTargetResult{}, errTargetSaveNotWired
	}
	return h.Commands.SaveTarget(ctx, req)
}

func (h *TargetConfigMounts) ensureMaps() {
	if h.components == nil {
		h.components = make(map[TargetConfigKey]*target_config.TargetConfig)
	}
}

func (h *TargetConfigMounts) addKey(routeID readmodel.RouteID) TargetConfigKey {
	return TargetConfigKey{Mode: TargetConfigAdd, WorkspaceID: h.WorkspaceID, RouteID: routeID}
}

func (h *TargetConfigMounts) editKey(routeID readmodel.RouteID, targetID readmodel.TargetID) TargetConfigKey {
	return TargetConfigKey{Mode: TargetConfigEdit, WorkspaceID: h.WorkspaceID, RouteID: routeID, TargetID: targetID}
}

func (h *TargetConfigMounts) refreshAddConfig(wf *target_config.TargetConfig, route readmodel.RouteReadModel) {
	wf.UpdateRoute(h.WorkspaceID, route)
	wf.UpdateCommands(h.Commands.SaveTarget, h.Commands.Setup, h.Commands.Auth, h.Commands.Credentials)
	wf.UpdateProviderOptions(h.ProviderOptions)
}

func (h *TargetConfigMounts) refreshEditConfig(wf *target_config.TargetConfig, route readmodel.RouteReadModel, target readmodel.TargetReadModel) {
	wf.UpdateTarget(h.WorkspaceID, route, target)
	wf.UpdateCommands(h.Commands.SaveTarget, h.Commands.Setup, h.Commands.Auth, h.Commands.Credentials)
	wf.UpdateProviderOptions(h.ProviderOptions)
}

func (h *TargetConfigMounts) refreshCachedTargetConfigDependencies() {
	h.ensureMaps()
	for _, wf := range h.components {
		wf.UpdateCommands(h.Commands.SaveTarget, h.Commands.Setup, h.Commands.Auth, h.Commands.Credentials)
		wf.UpdateProviderOptions(h.ProviderOptions)
	}
}
