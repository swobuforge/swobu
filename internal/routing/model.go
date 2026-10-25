package routing

import (
	"fmt"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/swobuforge/swobu/internal/domain/connectionidentity"
)

const (
	ReferenceSchemaVersion   = 5
	SchemaVersion            = 1
	CompositionSchemaVersion = 2
)

var (
	workspaceSlugPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
	commandIDPattern     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	mcpMountAliasPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
)

func ValidateMCPMountAlias(alias string) error {
	if alias == "swobu" || !mcpMountAliasPattern.MatchString(alias) {
		return pathError("workspace.mcp_mounts", "mount alias is invalid")
	}
	return nil
}

// WorkspaceSlug is the public path identity beneath /workspaces/{workspace}.
type WorkspaceSlug struct{ value string }

func ParseWorkspaceSlug(raw string) (WorkspaceSlug, error) {
	raw = strings.TrimSpace(raw) // swobu:io-string source=boundary
	if !workspaceSlugPattern.MatchString(raw) {
		return WorkspaceSlug{}, pathError("workspace.slug", "must be a lowercase path slug")
	}
	return WorkspaceSlug{value: raw}, nil
}

func (s WorkspaceSlug) String() string { return s.value }

// RouteName is the model-matching pattern. It is the route's sole public identity.
// The grammar is Go path.Match: '*' and '?' never cross '/', and character
// classes and backslash escapes have the standard Go semantics.
type RouteName struct{ value string }

func ParseRouteName(raw string) (RouteName, error) {
	if raw == "" || len(raw) > 128 || !utf8.ValidString(raw) || strings.TrimSpace(raw) != raw || raw == "." || raw == ".." {
		return RouteName{}, pathError("route.name", "must be a nonempty model pattern of at most 128 bytes")
	}
	for _, character := range raw {
		if unicode.IsControl(character) || unicode.IsSpace(character) {
			return RouteName{}, pathError("route.name", "model pattern cannot contain whitespace or controls")
		}
	}
	if _, err := path.Match(raw, ""); err != nil {
		return RouteName{}, pathError("route.name", "invalid Go path.Match pattern")
	}
	return RouteName{value: raw}, nil
}

func (n RouteName) String() string { return n.value }

// ExactModelID returns a client-selectable model ID only when a route's glob
// denotes a single literal. Wildcard patterns are routing policy, not models
// that can truthfully be advertised in a client model catalog.
func (n RouteName) ExactModelID() (string, bool) {
	var out strings.Builder
	for i := 0; i < len(n.value); i++ {
		switch n.value[i] {
		case '*', '?', '[':
			return "", false
		case '\\':
			i++
			if i >= len(n.value) {
				return "", false
			}
			out.WriteByte(n.value[i])
		default:
			out.WriteByte(n.value[i])
		}
	}
	return out.String(), n.value != ""
}

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
type Protocol struct{ value string }

type ProtocolSupport func(provider Provider, protocol string) bool

func ParseProtocol(raw string, provider Provider, supports ProtocolSupport) (Protocol, error) {
	raw = strings.TrimSpace(raw) // swobu:io-string source=boundary
	if raw == "" {
		return Protocol{}, pathError("target.protocol", "a concrete protocol is required")
	}
	if supports == nil || !supports(provider, raw) {
		return Protocol{}, pathError("target.protocol", fmt.Sprintf("protocol %q is unsupported for provider %q", raw, provider))
	}
	return Protocol{value: raw}, nil
}

func (p Protocol) String() string { return p.value }

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
	slices.SortFunc(out, func(a, b Workspace) int { return strings.Compare(a.slug.String(), b.slug.String()) })
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

// Workspace is one public namespace with independently enabled Services.
type Workspace struct {
	displayName           string
	slug                  WorkspaceSlug
	routes                []Route
	generations           map[TargetID]TargetVersion
	mcpMounts             map[string]MCPMount
	searchConnection      string
	searchFallback        []string
	serviceLimits         ServiceLimits
	enabledServices       map[ServiceKind]struct{}
	configurationRevision string
	managementRevision    uint64
	managementIncarnation string
}

// DisplayName is optional presentation metadata, never a path or authority key.
func (w Workspace) DisplayName() string { return w.displayName }

// WithDisplayName validates presentation metadata without changing Workspace
// identity, routes, mounts or authority. Empty clears the optional display name.
func (w Workspace) WithDisplayName(name string) (Workspace, error) {
	if !utf8.ValidString(name) || utf8.RuneCountInString(name) > 128 {
		return Workspace{}, pathError("workspace.display_name", "must be valid UTF-8 and at most 128 characters")
	}
	for _, value := range name {
		if unicode.IsControl(value) {
			return Workspace{}, pathError("workspace.display_name", "must not contain control characters")
		}
	}
	w = w.clone()
	w.displayName = strings.TrimSpace(name)
	return w, nil
}

// ManagementIncarnation distinguishes a durable Workspace lifetime from a
// later recreation at the same slug. It is not a credential or runtime stamp.
func (w Workspace) ManagementIncarnation() string { return w.managementIncarnation }

// WithManagementIncarnation restores store-owned concurrency metadata.
func (w Workspace) WithManagementIncarnation(incarnation string) Workspace {
	w = w.clone()
	w.managementIncarnation = incarnation
	return w
}

// ManagementRevision is durable routing concurrency metadata. It is not the
// process-local publication identity used by pending Exchanges.
func (w Workspace) ManagementRevision() uint64 { return w.managementRevision }

// WithManagementRevision restores store-owned metadata on a detached value.
// Semantic command inputs never choose this revision; configstore advances it.
func (w Workspace) WithManagementRevision(revision uint64) Workspace {
	w = w.clone()
	w.managementRevision = revision
	return w
}

// ConfigurationRevision identifies the store publication captured by this
// snapshot. It is runtime authority, not operator-authored durable settings.
func (w Workspace) ConfigurationRevision() string { return w.configurationRevision }

// WithConfigurationRevision attaches a store-issued publication identity to a
// detached snapshot. Configstore alone issues revisions; semantic edits do not.
func (w Workspace) WithConfigurationRevision(revision string) Workspace {
	w = w.clone()
	w.configurationRevision = revision
	return w
}

// AdminMCPSource is the sole server-owned builtin. Its identity cannot collide
// with Connection IDs; the builtin variant is explicit in durable mount intent.
const AdminMCPSource = "swobu-admin"
const AdminMCPRevision = "1"

// MCPMount selects exactly one external Connection or the closed builtin source.
// Credentials are owned by the Connection; builtin authority comes from ingress.
type MCPMount struct {
	Builtin      string
	ConnectionID string
	AllowedTools []string
}

// SourceID is the runtime binding identity, distinct from display/alias names.
func (m MCPMount) SourceID() string {
	if m.Builtin != "" {
		return "builtin:" + m.Builtin
	}
	return m.ConnectionID
}

func NewWorkspace(slug WorkspaceSlug, routes []Route) (Workspace, error) {
	return newWorkspace(slug, routes, nil)
}

// RestoreWorkspace constructs a materialized workspace with its durable target
// generation history. Operator-authored state cannot supply this ledger.
func RestoreWorkspace(slug WorkspaceSlug, routes []Route, generations map[TargetID]TargetVersion) (Workspace, error) {
	return newWorkspaceWithMounts(slug, routes, generations, nil)
}

func RestoreWorkspaceWithMCPMounts(slug WorkspaceSlug, routes []Route, generations map[TargetID]TargetVersion, mounts map[string]MCPMount) (Workspace, error) {
	return newWorkspaceWithMounts(slug, routes, generations, mounts)
}

func RestoreWorkspaceWithSearchConnection(slug WorkspaceSlug, routes []Route, generations map[TargetID]TargetVersion, mounts map[string]MCPMount, searchConnection string) (Workspace, error) {
	workspace, err := newWorkspaceWithMounts(slug, routes, generations, mounts)
	if err != nil {
		return Workspace{}, err
	}
	workspace.searchConnection = searchConnection
	if workspace.searchConnection != "" {
		workspace.enabledServices[ServiceSearch] = struct{}{}
	}
	return workspace, workspace.validate()
}

// RestoreWorkspaceWithSearchRoute reconstructs one ordered Search fallback chain.
func RestoreWorkspaceWithSearchRoute(slug WorkspaceSlug, routes []Route, generations map[TargetID]TargetVersion, mounts map[string]MCPMount, primary string, fallback []string) (Workspace, error) {
	workspace, err := RestoreWorkspaceWithSearchConnection(slug, routes, generations, mounts, primary)
	if err != nil {
		return Workspace{}, err
	}
	workspace.searchFallback = append([]string(nil), fallback...)
	return workspace, workspace.validate()
}

// RestoreWorkspaceWithServices reconstructs an explicit Service lifecycle from YAML.
// Unlike legacy fixture constructors it does not infer enabled Services.
func RestoreWorkspaceWithServices(slug WorkspaceSlug, routes []Route, generations map[TargetID]TargetVersion, mounts map[string]MCPMount, searchConnection string, enabled []ServiceKind) (Workspace, error) {
	w, err := RestoreWorkspaceWithSearchConnection(slug, routes, generations, mounts, searchConnection)
	if err != nil {
		return Workspace{}, err
	}
	return w.WithServices(enabled)
}

func newWorkspace(slug WorkspaceSlug, routes []Route, generations map[TargetID]TargetVersion) (Workspace, error) {
	return newWorkspaceWithMounts(slug, routes, generations, nil)
}

func newWorkspaceWithMounts(slug WorkspaceSlug, routes []Route, generations map[TargetID]TargetVersion, mounts map[string]MCPMount) (Workspace, error) {
	workspace := Workspace{slug: slug, routes: make([]Route, 0, len(routes)), generations: make(map[TargetID]TargetVersion), mcpMounts: make(map[string]MCPMount, len(mounts)), enabledServices: make(map[ServiceKind]struct{})}
	if len(mounts) > 0 {
		workspace.enabledServices[ServiceMCP] = struct{}{}
	}
	for alias, mount := range mounts {
		workspace.mcpMounts[alias] = MCPMount{Builtin: mount.Builtin, ConnectionID: mount.ConnectionID, AllowedTools: append([]string(nil), mount.AllowedTools...)}
	}
	for id, version := range generations {
		if id.value == "" || version == 0 {
			return Workspace{}, pathError("workspace.target_generations", "target ID and generation must be valid")
		}
		workspace.generations[id] = version
	}
	if len(routes) > 0 {
		workspace.enabledServices[ServiceModels] = struct{}{}
	}
	seenRoutes := make(map[RouteName]struct{}, len(routes))
	for _, route := range routes {
		if _, exists := seenRoutes[route.name]; exists {
			return Workspace{}, pathError("workspace.routes."+route.name.String(), "duplicate route")
		}
		seenRoutes[route.name] = struct{}{}
		workspace.routes = append(workspace.routes, route.clone())
		for _, tier := range route.tiers {
			for _, target := range tier.targets {
				if previous, exists := workspace.generations[target.id]; exists && previous != target.version {
					return Workspace{}, pathError("workspace.target_generations."+target.id.String(), "must equal the active target version")
				}
				workspace.generations[target.id] = target.version
			}
		}
	}
	if err := workspace.validate(); err != nil {
		return Workspace{}, err
	}
	return workspace, nil
}

func (w Workspace) validate() error {
	if !workspaceSlugPattern.MatchString(w.slug.value) {
		return pathError("workspace.slug", "workspace slug is invalid")
	}
	for alias, mount := range w.mcpMounts {
		if err := ValidateMCPMountAlias(alias); err != nil {
			return err
		}
		if mount.Builtin != "" {
			if mount.Builtin != AdminMCPSource || mount.ConnectionID != "" {
				return pathError("workspace.mcp_mounts."+alias, "select exactly one supported builtin or Connection")
			}
		} else if _, err := connectionidentity.Parse(mount.ConnectionID); err != nil {
			return pathError("workspace.mcp_mounts."+alias+".connection_id", "invalid Connection ID")
		}
		for _, tool := range mount.AllowedTools {
			if tool == "" || tool == "*" || strings.ContainsAny(tool, "\r\n") {
				return pathError("workspace.mcp_mounts."+alias, "allowed_tools must be explicit")
			}
		}
	}
	if w.searchConnection != "" {
		if _, err := connectionidentity.Parse(w.searchConnection); err != nil {
			return pathError("workspace.search.connection_id", "invalid Connection ID")
		}
	}
	if len(w.searchFallback) > 7 || (w.searchConnection == "" && len(w.searchFallback) > 0) {
		return pathError("workspace.search.fallback_connection_ids", "at most seven fallback Connections require one primary")
	}
	seenSearch := map[string]bool{w.searchConnection: w.searchConnection != ""}
	for _, source := range w.searchFallback {
		if _, err := connectionidentity.Parse(source); err != nil || seenSearch[source] {
			return pathError("workspace.search.fallback_connection_ids", "fallback IDs must be valid and unique across the route")
		}
		seenSearch[source] = true
	}
	for id, version := range w.generations {
		if id.value == "" || version == 0 {
			return pathError("workspace.target_generations", "target ID and generation must be valid")
		}
	}
	for kind := range w.enabledServices {
		if _, err := ParseServiceKind(string(kind)); err != nil {
			return err
		}
	}
	if err := w.serviceLimits.Validate(); err != nil {
		return err
	}
	if len(w.routes) > 0 && !w.HasService(ServiceModels) {
		return fmt.Errorf("workspaces.%s.enabled_services: %w: models routes", w.slug.String(), ErrServiceNotEmpty)
	}
	if len(w.mcpMounts) > 0 && !w.HasService(ServiceMCP) {
		return fmt.Errorf("workspaces.%s.enabled_services: %w: mcp mounts", w.slug.String(), ErrServiceNotEmpty)
	}
	if w.searchConnection != "" && !w.HasService(ServiceSearch) {
		return fmt.Errorf("workspaces.%s.enabled_services: %w: search route", w.slug.String(), ErrServiceNotEmpty)
	}
	if w.slug.value == "" {
		return pathError("workspace.slug", "workspace slug is required")
	}
	if len(w.routes) == 0 {
		return nil
	}
	targets := make(map[TargetID]RouteName)
	seenRoutes := make(map[RouteName]struct{}, len(w.routes))
	for _, route := range w.routes {
		name := route.name
		if _, exists := seenRoutes[name]; exists {
			return pathError("workspaces."+w.slug.String()+".routes", "duplicate route pattern")
		}
		seenRoutes[name] = struct{}{}
		if _, err := ParseRouteName(name.String()); err != nil {
			return err
		}
		if err := route.validate(); err != nil {
			return err
		}
		for _, tier := range route.tiers {
			for _, target := range tier.targets {
				if w.generations[target.id] != target.version {
					return pathError("workspace.target_generations."+target.id.String(), "must equal the active target version")
				}
				if previous, exists := targets[target.id]; exists {
					return pathError("workspaces."+w.slug.String()+".routes."+name.String()+".targets", fmt.Sprintf("target ID %q duplicates route %q", target.id.String(), previous.String()))
				}
				targets[target.id] = name
			}
		}
	}
	return nil
}

func (w Workspace) Slug() WorkspaceSlug { return w.slug }

// findRoute keeps route ordering as the only representation of precedence.
// Model route lists are small; a second index would duplicate authority.
func (w Workspace) findRoute(name RouteName) (Route, int) {
	for i, route := range w.routes {
		if route.name == name {
			return route, i
		}
	}
	return Route{}, -1
}

func (w Workspace) Route(name RouteName) (Route, bool) {
	route, index := w.findRoute(name)
	return route.clone(), index >= 0
}

func (w Workspace) Routes() []Route {
	out := make([]Route, len(w.routes))
	for i, route := range w.routes {
		out[i] = route.clone()
	}
	return out
}

// insertRoute puts new rules before the catch-all. It does not infer glob
// specificity; all other precedence is explicitly operator-authored.
func (w *Workspace) insertRoute(route Route) {
	for i, existing := range w.routes {
		if existing.name.String() == "*" {
			w.routes = append(w.routes, Route{})
			copy(w.routes[i+1:], w.routes[i:])
			w.routes[i] = route
			return
		}
	}
	w.routes = append(w.routes, route)
}

// TargetGenerations returns the highest committed generation for every target
// ID ever used in this workspace, including currently absent targets.
func (w Workspace) TargetGenerations() map[TargetID]TargetVersion {
	out := make(map[TargetID]TargetVersion, len(w.generations))
	for id, version := range w.generations {
		out[id] = version
	}
	return out
}

func (w Workspace) MCPMounts() map[string]MCPMount {
	out := make(map[string]MCPMount, len(w.mcpMounts))
	for alias, mount := range w.mcpMounts {
		out[alias] = MCPMount{Builtin: mount.Builtin, ConnectionID: mount.ConnectionID, AllowedTools: append([]string(nil), mount.AllowedTools...)}
	}
	return out
}

// SearchConnectionID is the primary Search Connection. The fallback chain is
// separate from Models tiers: Search V1 attempts each upstream in explicit order.
func (w Workspace) SearchConnectionID() string { return w.searchConnection }

// SearchFallbackIDs returns a detached ordered list excluding the primary.
func (w Workspace) SearchFallbackIDs() []string { return append([]string(nil), w.searchFallback...) }

// SearchConnections returns a detached invocation order: primary then fallbacks.
func (w Workspace) SearchConnections() []string {
	if w.searchConnection == "" {
		return nil
	}
	return append([]string{w.searchConnection}, w.searchFallback...)
}

func (w Workspace) ResolveRoute(requested string) (Route, error) {
	if !w.HasService(ServiceModels) {
		return Route{}, ErrServiceDisabled
	}
	if len(w.routes) == 0 {
		return Route{}, ErrNoRoute
	}
	if strings.TrimSpace(requested) == "" {
		return Route{}, ErrEmptyRequestedRoute
	}
	for _, route := range w.routes {
		matched, err := path.Match(route.name.String(), requested)
		if err != nil {
			return Route{}, err
		} // Construction rejects invalid patterns.
		if matched {
			return route.clone(), nil
		}
	}
	return Route{}, fmt.Errorf("%w: route %q", ErrNotFound, requested)
}

func (w Workspace) clone() Workspace {
	out := Workspace{displayName: w.displayName, slug: w.slug, routes: w.Routes(), generations: w.TargetGenerations(), mcpMounts: w.MCPMounts(), searchConnection: w.searchConnection, searchFallback: w.SearchFallbackIDs(), serviceLimits: w.serviceLimits, enabledServices: make(map[ServiceKind]struct{}, len(w.enabledServices)), configurationRevision: w.configurationRevision, managementRevision: w.managementRevision, managementIncarnation: w.managementIncarnation}
	for kind := range w.enabledServices {
		out.enabledServices[kind] = struct{}{}
	}
	return out
}

// Route is one model-matching pattern and its ordered fallback tiers.
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

// Target selects a reusable Connection and target-local model/protocol.
// Connection edits never change this target generation; execution freezes the
// current Connection revision separately at each attempt.
type Target struct {
	id           TargetID
	version      TargetVersion
	model        UpstreamModel
	protocol     Protocol
	connectionID connectionidentity.ID
	// connection is retained only for in-memory fixture construction. Durable
	// targets carry connectionID and reopen through the Connection registry.
	connection Connection
}

// ConnectionReference is an in-memory construction seam for verification
// fixtures that need a stable identity and the corresponding materialized
// connection at once. Durable callers use a parsed connectionidentity.ID.
type ConnectionReference struct {
	ID    connectionidentity.ID
	Value Connection
}

func NewTarget(id TargetID, model UpstreamModel, protocol Protocol, reference any) (Target, error) {
	var connectionID connectionidentity.ID
	var connection Connection
	switch value := reference.(type) {
	case connectionidentity.ID:
		connectionID = value
	case ConnectionReference:
		connectionID = value.ID
		connection = value.Value
	case Connection:
		connection = value
		var err error
		connectionID, err = connectionidentity.New()
		if err != nil {
			return Target{}, err
		}
	default:
		return Target{}, pathError("target.connection_id", "Connection ID is required")
	}
	target := Target{id: id, version: initialTargetVersion, model: model, protocol: protocol, connectionID: connectionID, connection: connection}
	if err := target.validate(); err != nil {
		return Target{}, err
	}
	return target, nil
}

// RestoreTarget constructs a materialized target from trusted daemon
// persistence. Operator-authored specifications use NewTarget and can never
// choose a version.
func RestoreTarget(id TargetID, version TargetVersion, model UpstreamModel, protocol Protocol, connectionID connectionidentity.ID) (Target, error) {
	target := Target{id: id, version: version, model: model, protocol: protocol, connectionID: connectionID}
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
	if t.connectionID.String() == "" {
		return pathError("target.connection_id", "Connection ID is required")
	}
	return nil
}

func (t Target) ID() TargetID                        { return t.id }
func (t Target) Version() TargetVersion              { return t.version }
func (t Target) Model() UpstreamModel                { return t.model }
func (t Target) Protocol() Protocol                  { return t.protocol }
func (t Target) ConnectionID() connectionidentity.ID { return t.connectionID }

// Connection returns the optional in-memory fixture upstream. Durable targets
// intentionally return nil and resolve their ID through the registry.
func (t Target) Connection() Connection { return t.connection }

// Provider returns the fixture upstream provider when one is attached.
func (t Target) Provider() Provider {
	if t.connection == nil {
		return ""
	}
	return t.connection.Provider()
}

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
