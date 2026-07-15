// Package cockpit is the active operator-facing TUI surface for Swobu.
//
// It is the canonical go-tui interactive entrypoint. It owns active tab
// product state, route and target inspection, inline editing workflows, and
// activity review. It does not own daemon lifecycle commands, telemetry
// disclosure, transcript-mode output, or framework focus state.
//
// Topology: readmodel, ports, adapters, surfaces, sections, features, ui,
// design, and testkit.
//
// Laws:
//   - direct go-tui only; no wrappers, no internal/tui shim, no retained
//     terminalui interactive resurrection
//   - UI packages import ports, not raw operator clients
//   - adapters import ports and concrete clients, not UI
//   - state belongs to the lowest component that owns the full lifecycle
//   - active tab is Cockpit state; focus and text cursor are go-tui state
//
// Temporary launch behavior is scoped to epic-05-tui-cockpit-v2 and will be
// replaced by a real go-tui app loop in subsequent tasks.
package cockpit
