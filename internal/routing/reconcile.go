package routing

import "fmt"

// RouteSpec is complete operator-desired topology and backend configuration.
// It deliberately excludes RouteName and TargetVersion: the resource path owns
// the route identity and reconciliation owns backend identity revisions.
type RouteSpec struct {
	Tiers []TierSpec
}

// TierSpec is one desired equal-balance group. Slice position is fallback
// priority; target position within the slice carries no semantics.
type TierSpec struct {
	Targets []TargetSpec
}

// TargetSpec is the stable identity and effective backend configuration desired
// for one target.
type TargetSpec struct {
	ID       TargetID
	Settings TargetSettings
}

// Spec returns the editable desired representation of a materialized route.
func (r Route) Spec() RouteSpec {
	spec := RouteSpec{Tiers: make([]TierSpec, len(r.tiers))}
	for tierIndex, tier := range r.tiers {
		spec.Tiers[tierIndex].Targets = make([]TargetSpec, len(tier.targets))
		for targetIndex, target := range tier.targets {
			spec.Tiers[tierIndex].Targets[targetIndex] = TargetSpec{
				ID: target.id,
				Settings: TargetSettings{
					Model: target.model, Protocol: target.protocol, Connection: target.connection,
				},
			}
		}
	}
	return spec
}

// ApplyRouteSpec validates and materializes a complete desired route. Existing
// targets match only by stable ID. Topology changes preserve versions; a change
// to normalized effective backend identity increments exactly once.
func ApplyRouteSpec(current Route, desired RouteSpec) (Route, error) {
	history := make(map[TargetID]TargetVersion)
	for _, tier := range current.tiers {
		for _, target := range tier.targets {
			history[target.id] = target.version
		}
	}
	return applyRouteSpec(current, desired, history)
}

func applyRouteSpec(current Route, desired RouteSpec, history map[TargetID]TargetVersion) (Route, error) {
	if current.name.value == "" {
		return Route{}, pathError("route.name", "route name is required")
	}
	if len(desired.Tiers) == 0 {
		return Route{}, pathError("routes."+current.name.String()+".tiers", "route must contain a tier")
	}

	existing := make(map[TargetID]Target)
	for _, tier := range current.tiers {
		for _, target := range tier.targets {
			existing[target.id] = target
		}
	}

	seen := make(map[TargetID]struct{})
	tiers := make([]Tier, len(desired.Tiers))
	for tierIndex, tierSpec := range desired.Tiers {
		path := fmt.Sprintf("routes.%s.tiers[%d].targets", current.name.String(), tierIndex)
		if len(tierSpec.Targets) == 0 {
			return Route{}, pathError(path, "tier must contain a target")
		}
		targets := make([]Target, len(tierSpec.Targets))
		for targetIndex, targetSpec := range tierSpec.Targets {
			targetPath := fmt.Sprintf("%s[%d]", path, targetIndex)
			if targetSpec.ID.value == "" {
				return Route{}, pathError(targetPath+".id", "target ID is required")
			}
			if _, duplicate := seen[targetSpec.ID]; duplicate {
				return Route{}, pathError(targetPath+".id", "duplicate target ID "+targetSpec.ID.String())
			}
			seen[targetSpec.ID] = struct{}{}

			target, err := targetFromSpec(targetSpec, existing[targetSpec.ID], history[targetSpec.ID])
			if err != nil {
				return Route{}, err
			}
			targets[targetIndex] = target
		}
		tier, err := NewTier(targets)
		if err != nil {
			return Route{}, err
		}
		tiers[tierIndex] = tier
	}
	return NewRoute(current.name, tiers)
}

func targetFromSpec(spec TargetSpec, current Target, highest TargetVersion) (Target, error) {
	target, err := NewTarget(spec.ID, spec.Settings.Model, spec.Settings.Protocol, spec.Settings.Connection)
	if err != nil {
		return Target{}, err
	}
	if current.id.value == "" {
		if highest == ^TargetVersion(0) {
			return Target{}, pathError("target.version", "target version exhausted")
		}
		if highest > 0 {
			target.version = highest + 1
		}
		return target, nil
	}
	currentSettings := TargetSettings{Model: current.model, Protocol: current.protocol, Connection: current.connection}
	if currentSettings.Equal(spec.Settings) {
		target.version = current.version
		return target, nil
	}
	if current.version == ^TargetVersion(0) {
		return Target{}, pathError("target.version", "target version exhausted")
	}
	target.version = current.version + 1
	return target, nil
}

// ApplyRouteSpec replaces one route inside the immutable configuration.
func (c Config) ApplyRouteSpec(slug WorkspaceSlug, routeName RouteName, desired RouteSpec) (Config, error) {
	return c.editWorkspace(slug, func(workspace Workspace) (Workspace, error) {
		current, ok := workspace.routes[routeName]
		if !ok {
			return Workspace{}, fmt.Errorf("%w: route %q", ErrNotFound, routeName.String())
		}
		nextRoute, err := applyRouteSpec(current.clone(), desired, workspace.generations)
		if err != nil {
			return Workspace{}, err
		}
		workspace.routes[routeName] = nextRoute
		for _, tier := range nextRoute.tiers {
			for _, target := range tier.targets {
				workspace.generations[target.id] = target.version
			}
		}
		return RestoreWorkspace(workspace.slug, workspace.defaultRoute, workspace.Routes(), workspace.generations)
	})
}
