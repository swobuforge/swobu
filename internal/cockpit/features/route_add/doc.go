// Package route_add owns the local-only draft route row.
//
// Adding a route does not persist an empty daemon route. The draft row stays in
// Cockpit UI state until the operator creates the first target, at which point
// the target save command carries the route model name to the adapter.
package route_add
