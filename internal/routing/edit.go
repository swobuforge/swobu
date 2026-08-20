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
		if highest := workspace.generations[first.id]; highest > 0 {
			if highest == ^TargetVersion(0) {
				return Workspace{}, pathError("target.version", "target version exhausted")
			}
			var err error
			first, err = RestoreTarget(first.id, highest+1, first.model, first.protocol, first.connection)
			if err != nil {
				return Workspace{}, err
			}
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
		workspace.generations[first.id] = first.version
		return RestoreWorkspace(workspace.slug, workspace.defaultRoute, workspace.Routes(), workspace.generations)
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
		return RestoreWorkspace(workspace.slug, workspace.defaultRoute, workspace.Routes(), workspace.generations)
	})
}

func (c Config) SetDefaultRoute(slug WorkspaceSlug, name RouteName) (Config, error) {
	return c.editWorkspace(slug, func(workspace Workspace) (Workspace, error) {
		if _, ok := workspace.routes[name]; !ok {
			return Workspace{}, fmt.Errorf("%w: route %q", ErrNotFound, name.String())
		}
		workspace.defaultRoute = name
		return RestoreWorkspace(workspace.slug, workspace.defaultRoute, workspace.Routes(), workspace.generations)
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
		return RestoreWorkspace(workspace.slug, workspace.defaultRoute, workspace.Routes(), workspace.generations)
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
		return RestoreWorkspace(workspace.slug, workspace.defaultRoute, workspace.Routes(), workspace.generations)
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
