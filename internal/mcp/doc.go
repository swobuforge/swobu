// Package mcp owns client-declared Exchange MCP execution and Workspace MCP
// connections, catalog composition, tool allowlists and invocation evidence.
//
// Client declarations are request-scoped. Workspace mounts refer directly to
// typed Node Connections or the closed daemon-owned swobu-admin builtin.
// External credentials are resolved at invocation from
// Connection-owned locators. No second MCP source identity registry exists.
// A catalog is derived from observed upstream tools and the Workspace allowlist;
// discovery cannot grant tools. The builtin dispatches typed Node commands only
// with live Workspace admission supplied by ingress; labels cannot forge it.
// Daemon-owned bindings close on shutdown.
//
// HTTP Connection checks reuse the guarded SDK discovery path with bounded
// responses and redirect denial. Stdio Connection checks launch a configured
// executable without a shell, bound its IO and kill/join the child at exit.
// Neither check invokes tools or persists a binding or a health flag.
package mcp
