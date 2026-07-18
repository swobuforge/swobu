// Package workspaces owns application-layer workspace queries and semantic
// commands over the routing aggregate.
//
// Command errors expose a deliberately small remediation taxonomy:
// INVALID_ARGUMENT, NOT_FOUND, CONFLICT, UNAVAILABLE, and INTERNAL. Routing may
// retain finer domain sentinels, but their messages—not new wire codes—explain
// individual business conflicts to operator clients.
package workspaces
