// Package clientconnect configures supported local AI clients for Swobu.
//
// Discovery and planning do not mutate client configuration. Plans describe
// Swobu-owned configuration changes, not effective settings after client flags,
// environment variables, or managed policy take precedence.
//
// Apply re-plans before writing and rejects stale reviewed changes. It preserves
// unrelated configuration and re-inspects after mutation; success requires no
// remaining planned changes. Multi-file writes require every committed prefix
// to be harmless; otherwise adapters replace one file atomically.
//
// Adapters inspect only the client state necessary to review and mutate
// Swobu-owned bindings.
//
// Route facades do not advertise model capability limits when the selected
// route's real limits are unavailable.
package clientconnect
