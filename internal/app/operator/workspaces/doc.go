// Package workspaces owns checked Workspace lifecycle and Service configuration.
// It delegates durable publication and revisions to configstore, reusable
// providers to Connections, grants to Access, and remote reachability to
// publications. None of those authorities are duplicated in Workspace state.
//
// Workspace identity is the canonical slug. A display-name edit never renames
// it. Creation, deletion and route updates validate the exact original ETag
// and trusted local admission at publication time; retries do not refresh
// review or restore revoked credentials. Deletion requires an unpublished
// Workspace and no remaining scoped accounting or Access dependencies.
//
// Model routes are first-match ordered patterns. Their target tiers express
// fallback within a winning route. MCP mounts carry an explicit Connection
// and allowed tool names; checked reviews bind Workspace, Connection, Access
// and publication revisions so a stale review cannot expand tool authority.
//
// Service activation reviews bind the Workspace revision, effective Principal
// grants and relay publication before enabling an authority-expanding Service.
// Checked writes rederive that impact under the authority owners' locks;
// delegated Admin MCP cannot supply independent human confirmation.
//
// Search changes replace the complete ordered Connection chain at the original
// Workspace ETag. Reference admission covers every fallback at publication;
// clearing removes the chain without disabling the independent Search Service.
//
// Command failures use INVALID_ARGUMENT, NOT_FOUND, CONFLICT, UNAVAILABLE and
// INTERNAL with concrete causes rather than independent error taxonomies.
package workspaces
