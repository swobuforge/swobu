// Package ports defines the cross-package boundaries that isolate the
// application layer from storage, evidence, and provider execution details.
//
// These interfaces carry semantic inputs and outputs. They must not become
// transport-shaped facades over provider or adapter internals, and they should
// expose one narrow contract where app orchestration needs compatibility-owned
// continuation storage or provider execution.
// Provider execution ports consume routable targets (execution-ready
// projections), not static provider-spec catalog rows.
package ports
