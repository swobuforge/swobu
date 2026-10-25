// Package routing owns validated workspace, route, tier, and target configuration.
//
// Construction receives provider validation facts; this package does not perform
// provider I/O, persist configuration, or track runtime target availability.
// Models Targets hold only a reusable Connection ID and local model/protocol.
// Registry publication admits existence/kind/protocol; Exchange freezes the
// referenced revision at each attempt. Connection edits never churn Targets.
// MCPMounts and the ordered Search Connection chain retain typed reusable identities;
// Connection publication owns existence and kind admission. Aliases and tool
// allowlists remain Workspace-local policy, never credential owners.
// Collection accessors return copies. Target versions advance when durable
// settings change, and retained generations prevent delete/re-add from reviving
// stale target state.
// Workspace display names are optional presentation metadata, never slug,
// credential audience or permission identity. Validated edits preserve every
// independent Route, MCP mount and Search Connection binding; changing one
// capability cannot erase another. Route/mount/search edits retain display
// metadata and store-issued Workspace concurrency identity. A detached edit is
// validated once rather than reconstructed through a persistence constructor.
// Route patterns use Go path.Match semantics. Routes are tested in explicit
// priority order; the first match selects one route and all fallback stays
// within its target tiers. Unknown names never implicitly select a default.
// Literal default is an ordinary pattern. Deleting any route requires no
// successor; deleting the last route leaves Models enabled and unconfigured.
// Only concrete exact patterns appear in the model catalog.
// Slug is immutable for a Workspace lifetime. There is no rename transition;
// changing an address requires separately reviewed deletion and new creation.
// MCP mount aliases are exact lowercase ASCII identities starting with a letter,
// limited to 32 bytes, with digits and hyphens thereafter; swobu is reserved.
// ValidateMCPMountAlias owns this rule for construction and catalog consumers.
// Workspace ServiceLimits owns independent operational rate ceilings and Models
// concurrency. Zero is unlimited; neither permission nor spend policy is implied.
package routing
