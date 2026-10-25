package routing

import (
	"fmt"

	"github.com/swobuforge/swobu/internal/domain/connectionidentity"
)

// WorkspaceSeed creates a workspace, its initial route, primary tier, and
// target as one invariant-preserving value transition.
type WorkspaceSeed struct {
	Slug   WorkspaceSlug
	Route  RouteName
	Target Target
}

// EmptyWorkspaceSeed creates a valid Workspace before any LLM route exists.
// The empty state is deliberate: readiness is operation-specific and does not
// require a fabricated default route or target.
type EmptyWorkspaceSeed struct{ Slug WorkspaceSlug }

func (c Config) CreateEmptyWorkspace(seed EmptyWorkspaceSeed) (Config, error) {
	if _, exists := c.workspaces[seed.Slug]; exists {
		return Config{}, fmt.Errorf("%w: workspace %q already exists", ErrConflict, seed.Slug.String())
	}
	workspace, err := RestoreWorkspace(seed.Slug, nil, nil)
	if err != nil {
		return Config{}, err
	}
	next := c.clone()
	next.workspaces[seed.Slug] = workspace
	return next, nil
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
	workspace, err := NewWorkspace(seed.Slug, []Route{route})
	if err != nil {
		return Config{}, err
	}
	next := c.clone()
	next.workspaces[seed.Slug] = workspace
	return next, nil
}

func (c Config) DeleteWorkspace(slug WorkspaceSlug) (Config, error) {
	if _, ok := c.workspaces[slug]; !ok {
		return Config{}, fmt.Errorf("%w: workspace %q", ErrNotFound, slug.String())
	}
	next := c.clone()
	delete(next.workspaces, slug)
	return next, nil
}

func (c Config) CreateRoute(slug WorkspaceSlug, name RouteName, first Target) (Config, error) {
	return c.editWorkspace(slug, func(workspace Workspace) (Workspace, error) {
		if !workspace.HasService(ServiceModels) {
			return Workspace{}, ErrServiceDisabled
		}
		if _, index := workspace.findRoute(name); index >= 0 {
			return Workspace{}, fmt.Errorf("%w: route %q already exists", ErrConflict, name.String())
		}
		if highest := workspace.generations[first.id]; highest > 0 {
			if highest == ^TargetVersion(0) {
				return Workspace{}, pathError("target.version", "target version exhausted")
			}
			var err error
			first, err = RestoreTarget(first.id, highest+1, first.model, first.protocol, first.connectionID)
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
		workspace.insertRoute(route)
		workspace.generations[first.id] = first.version
		return workspace, nil
	})
}

func (c Config) RenameRoute(slug WorkspaceSlug, from, to RouteName) (Config, error) {
	return c.editWorkspace(slug, func(workspace Workspace) (Workspace, error) {
		route, index := workspace.findRoute(from)
		if index < 0 {
			return Workspace{}, fmt.Errorf("%w: route %q", ErrNotFound, from.String())
		}
		if _, exists := workspace.findRoute(to); exists >= 0 {
			return Workspace{}, fmt.Errorf("%w: route %q already exists", ErrConflict, to.String())
		}
		route.name = to
		workspace.routes[index] = route
		return workspace, nil
	})
}

func (c Config) DeleteRoute(slug WorkspaceSlug, name RouteName) (Config, error) {
	return c.editWorkspace(slug, func(workspace Workspace) (Workspace, error) {
		_, index := workspace.findRoute(name)
		if index < 0 {
			return Workspace{}, fmt.Errorf("%w: route %q", ErrNotFound, name.String())
		}
		workspace.routes = append(workspace.routes[:index], workspace.routes[index+1:]...)
		return workspace, nil
	})
}

// SetRouteOrder atomically replaces precedence with exactly the existing set of
// patterns. No priority numbers, shadow copies, or sorting heuristics exist.
func (c Config) SetRouteOrder(slug WorkspaceSlug, order []RouteName) (Config, error) {
	return c.editWorkspace(slug, func(workspace Workspace) (Workspace, error) {
		if len(order) != len(workspace.routes) {
			return Workspace{}, pathError("workspace.routes", "must contain every configured route exactly once")
		}
		next := make([]Route, 0, len(order))
		seen := make(map[RouteName]struct{}, len(order))
		for _, name := range order {
			if _, duplicate := seen[name]; duplicate {
				return Workspace{}, pathError("workspace.routes", "duplicate route pattern")
			}
			seen[name] = struct{}{}
			route, index := workspace.findRoute(name)
			if index < 0 {
				return Workspace{}, pathError("workspace.routes", "unknown route pattern")
			}
			next = append(next, route)
		}
		workspace.routes = next
		return workspace, nil
	})
}

// TargetSettings replaces editable target settings while retaining stable ID.
type TargetSettings struct {
	Model        UpstreamModel
	Protocol     Protocol
	ConnectionID connectionidentity.ID
}

// Equal reports durable effective settings equality. Runtime caches or derived
// connection metadata must never change target generation behavior.
func (s TargetSettings) Equal(other TargetSettings) bool {
	return s.Model == other.Model && s.Protocol == other.Protocol && s.ConnectionID == other.ConnectionID
}

func (c Config) editRoute(slug WorkspaceSlug, routeName RouteName, edit func(Route) (Route, error)) (Config, error) {
	return c.editWorkspace(slug, func(workspace Workspace) (Workspace, error) {
		route, index := workspace.findRoute(routeName)
		if index < 0 {
			return Workspace{}, fmt.Errorf("%w: route %q", ErrNotFound, routeName.String())
		}
		nextRoute, err := edit(route.clone())
		if err != nil {
			return Workspace{}, err
		}
		workspace.routes[index] = nextRoute
		return workspace, nil
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
	// Semantic edits cannot alter independent presentation metadata.
	nextWorkspace.displayName = workspace.displayName
	if err := nextWorkspace.validate(); err != nil {
		return Config{}, err
	}
	next := c.clone()
	next.workspaces[slug] = nextWorkspace
	return next, nil
}

// SetWorkspaceDisplayName changes presentation only, preserving immutable slug.
func (c Config) SetWorkspaceDisplayName(slug WorkspaceSlug, name string) (Config, error) {
	workspace, exists := c.workspaces[slug]
	if !exists {
		return Config{}, fmt.Errorf("%w: workspace %q", ErrNotFound, slug.String())
	}
	updated, err := workspace.WithDisplayName(name)
	if err != nil {
		return Config{}, err
	}
	next := c.clone()
	next.workspaces[slug] = updated
	return next, nil
}

// SetMCPMount attaches a Connection and a closed tool allowlist.
// Credentials remain owned by the Connection.
func (c Config) SetMCPMount(slug WorkspaceSlug, alias string, mount MCPMount) (Config, error) {
	return c.editWorkspace(slug, func(workspace Workspace) (Workspace, error) {
		if !workspace.HasService(ServiceMCP) {
			return Workspace{}, ErrServiceDisabled
		}
		mounts := workspace.MCPMounts()
		mounts[alias] = mount
		workspace.mcpMounts = mounts
		return workspace, nil
	})
}

func (c Config) DeleteMCPMount(slug WorkspaceSlug, alias string) (Config, error) {
	return c.editWorkspace(slug, func(workspace Workspace) (Workspace, error) {
		mounts := workspace.MCPMounts()
		if _, ok := mounts[alias]; !ok {
			return Workspace{}, fmt.Errorf("%w: MCP mount %q", ErrNotFound, alias)
		}
		delete(mounts, alias)
		workspace.mcpMounts = mounts
		return workspace, nil
	})
}

// SetSearchRoute replaces the complete Search fallback chain atomically.
// Clearing the primary clears the chain without disabling Search.
func (c Config) SetSearchRoute(slug WorkspaceSlug, primary string, fallback []string) (Config, error) {
	return c.editWorkspace(slug, func(w Workspace) (Workspace, error) {
		if primary != "" && !w.HasService(ServiceSearch) {
			return Workspace{}, ErrServiceDisabled
		}
		w.searchConnection = primary
		w.searchFallback = append([]string(nil), fallback...)
		return w, nil
	})
}
