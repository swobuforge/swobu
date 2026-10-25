// Package clientconnect configures supported local AI clients for Swobu.
//
// Planning requires an explicit concrete model ID. The application validates it
// against saved Workspace routes and binds it to review and recovery identity.
// Adapters write that ID where the client supports a model setting; Claude Code
// and Antigravity configure endpoint/discovery only, so users must select the
// route-matching model explicitly in the client.
//
// Discovery and planning do not mutate client configuration. Plans describe
// Swobu-owned configuration changes, not effective settings after client flags,
// environment variables, or managed policy take precedence. Credential evidence
// is transient and redacted in reviews; Access alone validates Principal,
// Workspace audience and current Models permission. Preview never issues a key.
//
// Apply re-plans before writing and rejects stale reviewed changes. It preserves
// unrelated configuration and re-inspects after mutation; success requires no
// remaining planned changes. This is the file phase: the Connect application
// must also authenticate the exact credential read back before acknowledging
// configured. New files request owner-only permissions on POSIX; existing
// client files and backups retain the local operator's OS-selected permissions.
// Connect never adds permission-only changes to a semantic configuration plan.
// Multi-file writes require every committed prefix
// to be harmless; otherwise adapters replace one file atomically.
//
// Adapters inspect only the client state necessary to review and mutate
// Swobu-owned bindings.
//
// Route facades do not advertise model capability limits when the selected
// route's real limits are unavailable.
package clientconnect
