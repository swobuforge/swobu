package routing

import (
	"fmt"
)

// WorkspaceSeed creates a workspace, its initial route, primary tier, and
// target as one invariant-preserving value transition.
type WorkspaceSeed struct {
	Slug   WorkspaceSlug
	Route  RouteName
	Target Target
}

func (c Config) CreateWorkspace(seed WorkspaceSeed) (Config, error) {
	if _, exists := c.workspaces[seed.Slug]; exists {
		return Config{}, fmt.Errorf("%w: workspace %q already exists", ErrConflict, seed.Slug.String())
	}
	tier, err := NewTier([]Target{seed.Target})
	if err != nil {
		return Config{}, err
	}
	route, err := NewRoute(seed.Route, []Tier{tier})
	if err != nil {
		return Config{}, err
	}
	workspace, err := NewWorkspace(seed.Slug, seed.Route, []Route{route})
	if err != nil {
		return Config{}, err
	}
	workspaces := c.Workspaces()
	workspaces = append(workspaces, workspace)
	return NewConfig(workspaces)
}

func (c Config) RenameWorkspace(from, to WorkspaceSlug) (Config, error) {
	workspace, ok := c.workspaces[from]
	if !ok {
		return Config{}, fmt.Errorf("%w: workspace %q", ErrNotFound, from.String())
	}
	if _, exists := c.workspaces[to]; exists {
		return Config{}, fmt.Errorf("%w: workspace %q already exists", ErrConflict, to.String())
	}
	next := c.clone()
	delete(next.workspaces, from)
	workspace.slug = to
	next.workspaces[to] = workspace
	return NewConfig(next.Workspaces())
}

func (c Config) DeleteWorkspace(slug WorkspaceSlug) (Config, error) {
	if _, ok := c.workspaces[slug]; !ok {
		return Config{}, fmt.Errorf("%w: workspace %q", ErrNotFound, slug.String())
	}
	next := c.clone()
	delete(next.workspaces, slug)
	return NewConfig(next.Workspaces())
}

func (c Config) CreateRoute(slug WorkspaceSlug, name RouteName, first Target) (Config, error) {
	return c.editWorkspace(slug, func(workspace Workspace) (Workspace, error) {
		if _, exists := workspace.routes[name]; exists {
			return Workspace{}, fmt.Errorf("%w: route %q already exists", ErrConflict, name.String())
		}
		tier, err := NewTier([]Target{first})
		if err != nil {
			return Workspace{}, err
		}
		route, err := NewRoute(name, []Tier{tier})
		if err != nil {
			return Workspace{}, err
		}
		workspace.routes[name] = route
		return NewWorkspace(workspace.slug, workspace.defaultRoute, workspace.Routes())
	})
}

func (c Config) RenameRoute(slug WorkspaceSlug, from, to RouteName) (Config, error) {
	return c.editWorkspace(slug, func(workspace Workspace) (Workspace, error) {
		route, ok := workspace.routes[from]
		if !ok {
			return Workspace{}, fmt.Errorf("%w: route %q", ErrNotFound, from.String())
		}
		if _, exists := workspace.routes[to]; exists {
			return Workspace{}, fmt.Errorf("%w: route %q already exists", ErrConflict, to.String())
		}
		delete(workspace.routes, from)
		route.name = to
		workspace.routes[to] = route
		if workspace.defaultRoute == from {
			workspace.defaultRoute = to
		}
		return NewWorkspace(workspace.slug, workspace.defaultRoute, workspace.Routes())
	})
}

func (c Config) SetDefaultRoute(slug WorkspaceSlug, name RouteName) (Config, error) {
	return c.editWorkspace(slug, func(workspace Workspace) (Workspace, error) {
		if _, ok := workspace.routes[name]; !ok {
			return Workspace{}, fmt.Errorf("%w: route %q", ErrNotFound, name.String())
		}
		workspace.defaultRoute = name
		return NewWorkspace(workspace.slug, workspace.defaultRoute, workspace.Routes())
	})
}

func (c Config) DeleteRoute(slug WorkspaceSlug, name RouteName, replacement *RouteName) (Config, error) {
	return c.editWorkspace(slug, func(workspace Workspace) (Workspace, error) {
		if _, ok := workspace.routes[name]; !ok {
			return Workspace{}, fmt.Errorf("%w: route %q", ErrNotFound, name.String())
		}
		if len(workspace.routes) == 1 {
			return Workspace{}, fmt.Errorf("%w: delete the workspace instead", ErrLastRoute)
		}
		if workspace.defaultRoute == name {
			if replacement == nil {
				return Workspace{}, ErrDefaultReplacementRequired
			}
			if *replacement == name {
				return Workspace{}, fmt.Errorf("%w: replacement must differ from deleted route", ErrConflict)
			}
			if _, ok := workspace.routes[*replacement]; !ok {
				return Workspace{}, fmt.Errorf("%w: replacement route %q", ErrNotFound, replacement.String())
			}
			workspace.defaultRoute = *replacement
		}
		delete(workspace.routes, name)
		return NewWorkspace(workspace.slug, workspace.defaultRoute, workspace.Routes())
	})
}

// CreateTarget balances with an existing target when balanceWith is non-nil.
// A nil balanceWith appends a new singleton final fallback tier.
func (c Config) CreateTarget(slug WorkspaceSlug, routeName RouteName, target Target, balanceWith *TargetID) (Config, error) {
	return c.editRoute(slug, routeName, func(route Route) (Route, error) {
		if _, _, ok := route.target(target.id); ok {
			return Route{}, fmt.Errorf("%w: target %q already exists", ErrConflict, target.id.String())
		}
		if balanceWith != nil {
			tierIndex, _, ok := route.target(*balanceWith)
			if !ok {
				return Route{}, fmt.Errorf("%w: balance target %q", ErrNotFound, balanceWith.String())
			}
			route.tiers[tierIndex].targets = append(route.tiers[tierIndex].targets, target)
		} else {
			tier, err := NewTier([]Target{target})
			if err != nil {
				return Route{}, err
			}
			route.tiers = append(route.tiers, tier)
		}
		return NewRoute(route.name, route.tiers)
	})
}

// TargetSettings replaces editable target settings while retaining stable ID.
type TargetSettings struct {
	Model      UpstreamModel
	Protocol   Protocol
	Connection Connection
}

// Equal reports durable effective settings equality. Runtime caches or derived
// connection metadata must never change target generation behavior.
func (s TargetSettings) Equal(other TargetSettings) bool {
	return s.Model == other.Model && s.Protocol == other.Protocol && connectionsEqual(s.Connection, other.Connection)
}

// UpdateTargetSettings replaces editable settings in place and cannot alter
// the target's tier or any balanced peer.
func (c Config) UpdateTargetSettings(slug WorkspaceSlug, routeName RouteName, id TargetID, settings TargetSettings) (Config, error) {
	return c.editRoute(slug, routeName, func(route Route) (Route, error) {
		return replaceTargetSettings(route, id, settings)
	})
}

// replaceTargetSettings is the authoritative generation seam. TargetVersion
// identifies the complete effective backend configuration: provider,
// protocol, endpoint, credential reference, model/deployment, and
// provider-specific request options. Transport and codec options are pure
// derivatives of TargetSettings and have no independent mutation path.
func replaceTargetSettings(route Route, id TargetID, settings TargetSettings) (Route, error) {
	sourceTier, sourceIndex, ok := route.target(id)
	if !ok {
		return Route{}, fmt.Errorf("%w: target %q", ErrNotFound, id.String())
	}
	current := route.tiers[sourceTier].targets[sourceIndex]
	currentSettings := TargetSettings{Model: current.model, Protocol: current.protocol, Connection: current.connection}
	if currentSettings.Equal(settings) {
		return NewRoute(route.name, route.tiers)
	}
	replacement, err := NewTarget(id, settings.Model, settings.Protocol, settings.Connection)
	if err != nil {
		return Route{}, err
	}
	replacement.version = current.version + 1
	if replacement.version == 0 {
		return Route{}, pathError("target.version", "target version exhausted")
	}
	route.tiers[sourceTier].targets[sourceIndex] = replacement
	return NewRoute(route.name, route.tiers)
}

func (c Config) DeleteTarget(slug WorkspaceSlug, routeName RouteName, id TargetID) (Config, error) {
	return c.editRoute(slug, routeName, func(route Route) (Route, error) {
		total := 0
		for _, tier := range route.tiers {
			total += len(tier.targets)
		}
		if total == 1 {
			return Route{}, fmt.Errorf("%w: delete the route instead", ErrLastTarget)
		}
		tierIndex, targetIndex, ok := route.target(id)
		if !ok {
			return Route{}, fmt.Errorf("%w: target %q", ErrNotFound, id.String())
		}
		route.tiers[tierIndex].targets = append(route.tiers[tierIndex].targets[:targetIndex], route.tiers[tierIndex].targets[targetIndex+1:]...)
		if len(route.tiers[tierIndex].targets) == 0 {
			route.tiers = append(route.tiers[:tierIndex], route.tiers[tierIndex+1:]...)
		}
		return NewRoute(route.name, route.tiers)
	})
}

func (c Config) SetCredential(slug WorkspaceSlug, routeName RouteName, id TargetID, raw string) (Config, error) {
	return c.editRoute(slug, routeName, func(route Route) (Route, error) {
		tierIndex, targetIndex, ok := route.target(id)
		if !ok {
			return Route{}, fmt.Errorf("%w: target %q", ErrNotFound, id.String())
		}
		target := route.tiers[tierIndex].targets[targetIndex]
		connection, err := setConnectionCredential(target.connection, raw)
		if err != nil {
			return Route{}, err
		}
		return replaceTargetSettings(route, id, TargetSettings{Model: target.model, Protocol: target.protocol, Connection: connection})
	})
}

func (r Route) target(id TargetID) (int, int, bool) {
	for tierIndex := range r.tiers {
		for targetIndex := range r.tiers[tierIndex].targets {
			if r.tiers[tierIndex].targets[targetIndex].id == id {
				return tierIndex, targetIndex, true
			}
		}
	}
	return 0, 0, false
}

func (c Config) editRoute(slug WorkspaceSlug, routeName RouteName, edit func(Route) (Route, error)) (Config, error) {
	return c.editWorkspace(slug, func(workspace Workspace) (Workspace, error) {
		route, ok := workspace.routes[routeName]
		if !ok {
			return Workspace{}, fmt.Errorf("%w: route %q", ErrNotFound, routeName.String())
		}
		nextRoute, err := edit(route.clone())
		if err != nil {
			return Workspace{}, err
		}
		workspace.routes[routeName] = nextRoute
		return NewWorkspace(workspace.slug, workspace.defaultRoute, workspace.Routes())
	})
}

func (c Config) editWorkspace(slug WorkspaceSlug, edit func(Workspace) (Workspace, error)) (Config, error) {
	workspace, ok := c.workspaces[slug]
	if !ok {
		return Config{}, fmt.Errorf("%w: workspace %q", ErrNotFound, slug.String())
	}
	nextWorkspace, err := edit(workspace.clone())
	if err != nil {
		return Config{}, err
	}
	next := c.clone()
	next.workspaces[slug] = nextWorkspace
	return NewConfig(next.Workspaces())
}
