// Package cockpit is the active operator-facing TUI surface for Swobu.
//
// It is the canonical go-tui interactive entrypoint. It owns active tab
// product state, route and target inspection, inline editing workflows,
// activity review, and root-shell notices. It does not own daemon lifecycle
// commands, telemetry disclosure, transcript-mode output, or framework focus
// state.
//
// Topology: readmodel, ports, adapters, pages, sections, features, ui, and
// testkit.
//
// Laws:
//   - direct go-tui only; no wrappers, no internal/tui shim, no retained
//     interactive resurrection path
//   - UI packages import ports, not raw operator clients
//   - adapters import ports and concrete clients, not UI
//   - state belongs to the lowest component that owns the full lifecycle
//   - persisted workspace pages are rebuilt from the root registry projection;
//     only draft pages retain local authoring state
//   - active tab is Cockpit state; focus and text cursor are go-tui state
//
// Launch loads the daemon-backed Cockpit read model, enters the go-tui app loop
// for interactive terminals, and renders deterministic snapshots for
// non-interactive tests and transcript contexts. When the daemon owns zero
// workspaces, Cockpit locally projects the conventional `default` authoring
// surface. The projection remounts as a persisted page only after the first
// target atomically materializes its workspace, route, and target.
package cockpit
