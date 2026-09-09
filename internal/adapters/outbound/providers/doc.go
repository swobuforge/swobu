// Package providers composes outbound provider discovery and execution adapters.
//
// Provider facets are composed explicitly rather than registered through
// package initialization or mutable globals. Provider-owned rules select
// translation; model names, catalog labels, and backend error prose do not.
package providers
