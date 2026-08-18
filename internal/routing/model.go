package routing

import (
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
)

const (
	SchemaVersion        = 1
	PublicDefaultRouteID = "default"
)

var (
	workspaceSlugPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
	commandIDPattern     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
)

// WorkspaceSlug is the public path identity beneath /c/{workspace}.
type WorkspaceSlug struct{ value string }

func ParseWorkspaceSlug(raw string) (WorkspaceSlug, error) {
	raw = strings.TrimSpace(raw) // swobu:io-string source=boundary
	if !workspaceSlugPattern.MatchString(raw) {
		return WorkspaceSlug{}, pathError("workspace.slug", "must be a lowercase path slug")
	}
	return WorkspaceSlug{value: raw}, nil
}

func (s WorkspaceSlug) String() string { return s.value }

// RouteName is the client-visible model key inside one workspace.
type RouteName struct{ value string }

func ParseRouteName(raw string) (RouteName, error) {
	trimmed := strings.TrimSpace(raw) // swobu:io-string source=boundary
	if raw != trimmed || !commandIDPattern.MatchString(raw) || raw == "." || raw == ".." {
		return RouteName{}, pathError("route.name", "must be a URL-safe command identifier")
	}
	if raw == PublicDefaultRouteID {
		return RouteName{}, pathError("route.name", fmt.Sprintf("%q is reserved for default-route selection", PublicDefaultRouteID))
	}
	return RouteName{value: raw}, nil
}

func (n RouteName) String() string { return n.value }

// TargetID is one stable URL-segment identity, unique within a workspace.
type TargetID struct{ value string }

func ParseTargetID(raw string) (TargetID, error) {
	trimmed := strings.TrimSpace(raw) // swobu:io-string source=boundary
	if raw != trimmed || !commandIDPattern.MatchString(raw) || raw == "." || raw == ".." {
		return TargetID{}, pathError("target.id", "must be a URL-safe command identifier")
	}
	return TargetID{value: raw}, nil
}

func (id TargetID) String() string { return id.value }

// TargetVersion identifies the complete effective backend configuration. Any
// change affecting request encoding, transport, provider identity, native
// state ownership, model/deployment, credentials, codec options, or
// provider-specific request options creates a new version.
//
// Persistent session checkpoints are unsupported while TargetVersion is process-local.
type TargetVersion uint64

const initialTargetVersion TargetVersion = 1

// UpstreamModel is the provider-side model or deployment identifier.
type UpstreamModel struct{ value string }

func ParseUpstreamModel(raw string) (UpstreamModel, error) {
	raw = strings.TrimSpace(raw) // swobu:io-string source=boundary
	if raw == "" {
		return UpstreamModel{}, pathError("target.model", "upstream model is required")
	}
	return UpstreamModel{value: raw}, nil
}

func (m UpstreamModel) String() string { return m.value }

// Provider identifies one catalog-supported provider. Construction boundaries
// validate it through ProviderSupport so routing does not mirror catalog
// membership with its own provider constants.
type Provider string

// ProviderSupport is supplied by the profile catalog at a construction edge.
// It prevents routing from accepting unknown provider IDs without importing the
// catalog into this domain package.
type ProviderSupport func(string) bool

// ParseProvider validates one external provider identifier through the
// construction edge's catalog predicate.
func ParseProvider(raw string, supports ProviderSupport) (Provider, error) {
	raw = strings.TrimSpace(raw) // swobu:io-string source=boundary
	if raw == "" || supports == nil || !supports(raw) {
		return "", pathError("connection.provider", "provider is unsupported")
	}
	return Provider(raw), nil
}

// Protocol is a finalized, concrete provider protocol. ParseProtocol requires
// the provider catalog's support predicate at the construction edge, avoiding a
// second protocol table in this package.
type Protocol struct {
	value    string
	provider Provider
}

type ProtocolSupport func(provider Provider, protocol string) bool

func ParseProtocol(raw string, provider Provider, supports ProtocolSupport) (Protocol, error) {
	raw = strings.TrimSpace(raw) // swobu:io-string source=boundary
	if raw == "" {
		return Protocol{}, pathError("target.protocol", "a concrete protocol is required")
	}
	if supports == nil || !supports(provider, raw) {
		return Protocol{}, pathError("target.protocol", fmt.Sprintf("protocol %q is unsupported for provider %q", raw, provider))
	}
	return Protocol{value: raw, provider: provider}, nil
}

func (p Protocol) String() string { return p.value }

// Provider returns the provider whose catalog admitted this protocol.
func (p Protocol) Provider() Provider { return p.provider }

// Config is the complete immutable routing aggregate published to readers.
type Config struct {
	workspaces map[WorkspaceSlug]Workspace
}

func NewConfig(workspaces []Workspace) (Config, error) {
	bySlug := make(map[WorkspaceSlug]Workspace, len(workspaces))
	for _, workspace := range workspaces {
		if err := workspace.validate(); err != nil {
			return Config{}, err
		}
		if _, exists := bySlug[workspace.slug]; exists {
			return Config{}, pathError("workspaces."+workspace.slug.String(), "duplicate workspace")
		}
		bySlug[workspace.slug] = workspace.clone()
	}
	return Config{workspaces: bySlug}, nil
}

func (c Config) Workspace(slug WorkspaceSlug) (Workspace, bool) {
	w, ok := c.workspaces[slug]
	return w.clone(), ok
}

func (c Config) Workspaces() []Workspace {
	out := make([]Workspace, 0, len(c.workspaces))
	for _, workspace := range c.workspaces {
		out = append(out, workspace.clone())
	}
	return out
}

func (c Config) WorkspaceCount() int { return len(c.workspaces) }

func (c Config) clone() Config {
	out := Config{workspaces: make(map[WorkspaceSlug]Workspace, len(c.workspaces))}
	for slug, workspace := range c.workspaces {
		out.workspaces[slug] = workspace.clone()
	}
	return out
}

// Clone returns an immutable snapshot with independently owned collection
// storage. It is the publication boundary used by configstore readers.
func (c Config) Clone() Config { return c.clone() }

// Workspace is one non-empty public namespace and its model routes.
type Workspace struct {
	slug         WorkspaceSlug
	defaultRoute RouteName
	routes       map[RouteName]Route
}

func NewWorkspace(slug WorkspaceSlug, defaultRoute RouteName, routes []Route) (Workspace, error) {
	workspace := Workspace{slug: slug, defaultRoute: defaultRoute, routes: make(map[RouteName]Route, len(routes))}
	for _, route := range routes {
		if _, exists := workspace.routes[route.name]; exists {
			return Workspace{}, pathError("workspace.routes."+route.name.String(), "duplicate route")
		}
		workspace.routes[route.name] = route.clone()
	}
	if err := workspace.validate(); err != nil {
		return Workspace{}, err
	}
	return workspace, nil
}

func (w Workspace) validate() error {
	if w.slug.value == "" {
		return pathError("workspace.slug", "workspace slug is required")
	}
	if len(w.routes) == 0 {
		return pathError("workspaces."+w.slug.String()+".routes", "persisted workspace must contain a route")
	}
	if _, ok := w.routes[w.defaultRoute]; !ok {
		return pathError("workspaces."+w.slug.String()+".default_route", "must name an existing route")
	}
	targets := make(map[TargetID]RouteName)
	for name, route := range w.routes {
		if name != route.name {
			return pathError("workspaces."+w.slug.String()+".routes", "route key does not match route name")
		}
		if err := route.validate(); err != nil {
			return err
		}
		for _, tier := range route.tiers {
			for _, target := range tier.targets {
				if previous, exists := targets[target.id]; exists {
					return pathError("workspaces."+w.slug.String()+".routes."+name.String()+".targets", fmt.Sprintf("target ID %q duplicates route %q", target.id.String(), previous.String()))
				}
				targets[target.id] = name
			}
		}
	}
	return nil
}

func (w Workspace) Slug() WorkspaceSlug     { return w.slug }
func (w Workspace) DefaultRoute() RouteName { return w.defaultRoute }

func (w Workspace) Route(name RouteName) (Route, bool) {
	route, ok := w.routes[name]
	return route.clone(), ok
}

func (w Workspace) Routes() []Route {
	out := make([]Route, 0, len(w.routes))
	for _, route := range w.routes {
		out = append(out, route.clone())
	}
	return out
}

func (w Workspace) ResolveRoute(requested string) (Route, error) {
	if strings.TrimSpace(requested) == "" { // swobu:io-string source=boundary
		return Route{}, ErrEmptyRequestedRoute
	}
	if requested == PublicDefaultRouteID {
		return w.routes[w.defaultRoute].clone(), nil
	}
	name, err := ParseRouteName(requested)
	if err == nil {
		if route, ok := w.routes[name]; ok {
			return route.clone(), nil
		}
	}
	return w.routes[w.defaultRoute].clone(), nil
}

func (w Workspace) clone() Workspace {
	out := Workspace{slug: w.slug, defaultRoute: w.defaultRoute, routes: make(map[RouteName]Route, len(w.routes))}
	for name, route := range w.routes {
		out.routes[name] = route.clone()
	}
	return out
}

// Route is one client-visible model name and its ordered fallback tiers.
type Route struct {
	name  RouteName
	tiers []Tier
}

func NewRoute(name RouteName, tiers []Tier) (Route, error) {
	route := Route{name: name, tiers: cloneTiers(tiers)}
	if err := route.validate(); err != nil {
		return Route{}, err
	}
	return route, nil
}

func (r Route) validate() error {
	if r.name.value == "" {
		return pathError("route.name", "route name is required")
	}
	if len(r.tiers) == 0 {
		return pathError("routes."+r.name.String()+".tiers", "route must contain a tier")
	}
	seen := map[TargetID]struct{}{}
	for i, tier := range r.tiers {
		if len(tier.targets) == 0 {
			return pathError(fmt.Sprintf("routes.%s.tiers[%d].targets", r.name.String(), i), "tier must contain a target")
		}
		for _, target := range tier.targets {
			if err := target.validate(); err != nil {
				return err
			}
			if _, exists := seen[target.id]; exists {
				return pathError("routes."+r.name.String()+".targets", "duplicate target ID "+target.id.String())
			}
			seen[target.id] = struct{}{}
		}
	}
	return nil
}

func (r Route) Name() RouteName { return r.name }
func (r Route) Tiers() []Tier   { return cloneTiers(r.tiers) }

// clone owns every mutable slice transitively. Semantic edits mutate tier
// target arrays in place, so a shallow tier copy would corrupt the source value
// before a configstore transaction reaches its persistence commit point.
func (r Route) clone() Route { return Route{name: r.name, tiers: cloneTiers(r.tiers)} }

func cloneTiers(tiers []Tier) []Tier {
	out := make([]Tier, len(tiers))
	for index := range tiers {
		out[index] = tiers[index].clone()
	}
	return out
}

// Tier is one non-empty equal-balance target group. Slice position is fallback
// order; target position carries no semantics.
type Tier struct{ targets []Target }

func NewTier(targets []Target) (Tier, error) {
	if len(targets) == 0 {
		return Tier{}, pathError("tier.targets", "tier must contain a target")
	}
	tier := Tier{targets: slices.Clone(targets)}
	seen := map[TargetID]struct{}{}
	for _, target := range tier.targets {
		if err := target.validate(); err != nil {
			return Tier{}, err
		}
		if _, exists := seen[target.id]; exists {
			return Tier{}, pathError("tier.targets", "duplicate target ID "+target.id.String())
		}
		seen[target.id] = struct{}{}
	}
	return tier, nil
}

func (t Tier) Targets() []Target { return slices.Clone(t.targets) }

func (t Tier) clone() Tier { return Tier{targets: slices.Clone(t.targets)} }

// Target is one immutable provider/model/protocol configuration.
type Target struct {
	id         TargetID
	version    TargetVersion
	model      UpstreamModel
	protocol   Protocol
	connection Connection
}

func NewTarget(id TargetID, model UpstreamModel, protocol Protocol, connection Connection) (Target, error) {
	target := Target{id: id, version: initialTargetVersion, model: model, protocol: protocol, connection: connection}
	if err := target.validate(); err != nil {
		return Target{}, err
	}
	return target, nil
}

func (t Target) validate() error {
	if t.id.value == "" {
		return pathError("target.id", "target ID is required")
	}
	if t.version == 0 {
		return pathError("target.version", "target version is required")
	}
	if t.model.value == "" {
		return pathError("target.model", "upstream model is required")
	}
	if t.protocol.value == "" {
		return pathError("target.protocol", "concrete protocol is required")
	}
	if t.connection == nil || t.connection.Provider() == "" {
		return pathError("target.connection", "exactly one connection variant is required")
	}
	if t.protocol.provider != t.connection.Provider() {
		return pathError("target.protocol", fmt.Sprintf("provider %q contradicts connection provider %q", t.protocol.provider, t.connection.Provider()))
	}
	return nil
}

func (t Target) ID() TargetID           { return t.id }
func (t Target) Version() TargetVersion { return t.version }
func (t Target) Model() UpstreamModel   { return t.model }
func (t Target) Protocol() Protocol     { return t.protocol }
func (t Target) Connection() Connection { return t.connection }
func (t Target) Provider() Provider     { return t.connection.Provider() }

// URL is a validated HTTP(S) service URL.
type URL struct{ value string }

func ParseURL(path, raw string) (URL, error) {
	raw = strings.TrimSpace(raw) // swobu:io-string source=boundary
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil {
		return URL{}, pathError(path, "must be an absolute HTTP(S) URL without user info")
	}
	return URL{value: strings.TrimRight(raw, "/")}, nil
}

func (u URL) String() string { return u.value }
